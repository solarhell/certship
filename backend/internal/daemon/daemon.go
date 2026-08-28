package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/acme"
	"github.com/solarhell/certship/internal/alert"
	alidnsutil "github.com/solarhell/certship/internal/alidns"
	cdnpkg "github.com/solarhell/certship/internal/cdn"
	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/internal/domainsync"
	"github.com/solarhell/certship/internal/oss"
	"github.com/solarhell/certship/internal/renewerr"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entcloudaccount "github.com/solarhell/certship/pkg/ent/cloudaccount"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
	"github.com/solarhell/certship/pkg/ent/renewtask"
	"github.com/solarhell/certship/pkg/model"
)

// renewConcurrency 同时执行的续期任务数。
// 压得低是因为每个任务都要写阿里云 DNS 记录并向 CA 发起订单,两边都有速率限制。
const renewConcurrency = 4

type Daemon struct {
	logger *zap.Logger
}

func New(logger *zap.Logger) *Daemon {
	return &Daemon{logger: logger}
}

func (d *Daemon) Run(ctx context.Context) error {
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		return fmt.Errorf("从数据库读取配置失败: %w", err)
	}

	interval, err := database.ParseScanInterval(settings.ScanInterval)
	if err != nil {
		return err
	}

	d.logger.Info("certship 启动",
		zap.Duration("scan_interval", interval),
		zap.Int("renew_before_days", settings.RenewBeforeDays),
	)

	d.cycle(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.cycle(ctx)
		case <-ctx.Done():
			d.logger.Info("certship 正在停止")
			return nil
		}
	}
}

// ExecuteTaskByID 按任务 ID 立即执行（供 API 层调用）
func (d *Daemon) ExecuteTaskByID(ctx context.Context, taskID string) error {
	db := database.GetClient()
	task, err := db.RenewTask.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}
	if task.Status != renewtask.StatusPending {
		return fmt.Errorf("任务状态为 %s，只能执行 pending 状态的任务", task.Status)
	}
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	acmeMgr := acme.NewManager(d.logger)
	account, err := d.ensureACMEAccount(ctx, acmeMgr, settings)
	if err != nil {
		return err
	}
	d.executeTask(ctx, task, acmeMgr, account, settings)
	return nil
}

// ensureACMEAccount 取出(或首次注册)ACME 账号并持久化。
//
// 单独做一次的原因:并发续期时若每个任务各自注册,会重复向 CA 建账号,
// 还会各自把不同的账号密钥写回同一行配置。
func (d *Daemon) ensureACMEAccount(ctx context.Context, mgr *acme.Manager, settings *ent.AppSettings) (*acme.Account, error) {
	account, err := mgr.EnsureAccount(settings.AcmeAccountKey, settings.AcmeRegistration)
	if err != nil {
		return nil, fmt.Errorf("准备 ACME 账号失败: %w", err)
	}
	if account.Registered {
		if err := database.SaveACMEAccount(ctx, account.KeyPEM, account.RegistrationJSON); err != nil {
			d.logger.Warn("保存 ACME 账号信息失败", zap.Error(err))
		}
	}
	return account, nil
}

// cycle 一次完整周期：扫描云上现状 → 对账 → 检查并重新部署 → 续期
func (d *Daemon) cycle(ctx context.Context) {
	d.logger.Info("开始扫描周期")

	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		d.logger.Error("读取配置失败", zap.Error(err))
		return
	}

	accounts := database.GetEnabledCloudAccounts(ctx)
	if len(accounts) == 0 {
		d.logger.Warn("数据库中无可用的阿里云账号，跳过本次扫描")
		return
	}

	// Phase 1: 扫描 OSS + CDN,得到云上现状与本轮实际覆盖范围
	result := discovery.Run(ctx, d.logger, accounts)
	d.logger.Info("发现自定义域名", zap.Int("count", len(result.Domains)))

	// Phase 2: 双向对账——新增/更新,以及识别已经下线的域名
	missingGrace, archiveAfter := database.PresenceWindows(ctx)
	stats := domainsync.Reconcile(ctx, d.logger, result, missingGrace, archiveAfter)
	d.logger.Info("域名对账完成",
		zap.Int("added", stats.Added),
		zap.Int("updated", stats.Updated),
		zap.Int("revived", len(stats.Revived)),
		zap.Int("missing", len(stats.Missing)),
		zap.Int("archived", len(stats.Archived)),
		zap.Int("unobservable", stats.Unobservable),
	)
	d.notifyPresenceChanges(ctx, stats)

	// Phase 3: 检查已有证书是否真正生效在目标侧
	d.checkAndRedeploy(ctx)

	// Phase 4: 续期需要处理的证书
	d.renewCerts(ctx, settings)

	// Phase 5: 清掉早已归档、证书也过期的历史记录
	d.purgeArchived(ctx, settings.ArchivedRetention)

	d.logger.Info("扫描周期完成")
}

// notifyPresenceChanges 为存在性跃迁发通知（经去噪）
func (d *Daemon) notifyPresenceChanges(ctx context.Context, stats domainsync.Stats) {
	for _, name := range stats.Missing {
		d.notifyDomainState(ctx, name, alert.StateMissing,
			fmt.Sprintf("⚠️ 域名已从云上消失，暂停续期\n域名: %s\n如果是有意下线可忽略；若是误删请在阿里云侧恢复绑定", name))
	}
	for _, name := range stats.Archived {
		d.notifyDomainState(ctx, name, alert.StateArchived,
			fmt.Sprintf("📦 域名长期不在云上，已归档\n域名: %s\ncertship 不再为它签发证书", name))
	}
	for _, name := range stats.Revived {
		d.notifyDomainState(ctx, name, alert.StateOK,
			fmt.Sprintf("♻️ 下线的域名重新出现，已恢复托管\n域名: %s", name))
	}
}

// notifyDomainState 在状态确实变化（或异常持续足够久）时才发通知
func (d *Daemon) notifyDomainState(ctx context.Context, domainName string, state alert.State, text string) {
	db := database.GetClient()
	dom, err := db.Domain.Query().Where(entdomain.DomainEQ(domainName)).Only(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	if !alert.ShouldNotify(alert.State(dom.NotifiedState), dom.LastNotifiedAt, state, now) {
		return
	}
	database.SendToAllFeishuChannels(ctx, text, d.logger)
	if err := dom.Update().
		SetNotifiedState(string(state)).
		SetLastNotifiedAt(now).
		Exec(ctx); err != nil {
		d.logger.Warn("更新通知状态失败", zap.String("domain", domainName), zap.Error(err))
	}
}

// checkAndRedeploy 检查 DB 中有有效证书的域名，目标侧（OSS/CDN）是否真正生效。
// 只处理云上还在、且仍由 certship 托管的域名。
func (d *Daemon) checkAndRedeploy(ctx context.Context) {
	db := database.GetClient()
	cdnMgr := cdnpkg.NewManager(d.logger)
	ossBinder := oss.NewCertBinder(d.logger)
	ossScanner := oss.NewScanner(d.logger)

	activeDomains, err := db.Domain.Query().
		Where(
			entdomain.StatusEQ(entdomain.StatusActive),
			entdomain.CertPemNEQ(""),
			entdomain.ExpiresAtGT(time.Now()),
			entdomain.PresenceEQ(entdomain.PresencePresent),
			entdomain.ManagedEQ(true),
		).
		All(ctx)
	if err != nil {
		d.logger.Error("查询活跃域名失败", zap.Error(err))
		return
	}

	for _, dom := range activeDomains {
		select {
		case <-ctx.Done():
			return
		default:
		}

		account, err := db.CloudAccount.Query().
			Where(entcloudaccount.NameEQ(dom.AccountName)).
			Only(ctx)
		if err != nil {
			continue
		}

		if dom.DeployTarget == entdomain.DeployTargetCdn {
			d.redeployCDN(ctx, dom, account, cdnMgr, ossBinder, ossScanner)
			continue
		}
		d.redeployOSS(ctx, dom, account, ossBinder, ossScanner)
	}
}

func (d *Daemon) redeployCDN(
	ctx context.Context,
	dom *ent.Domain,
	account *ent.CloudAccount,
	cdnMgr *cdnpkg.Manager,
	ossBinder *oss.CertBinder,
	ossScanner *oss.Scanner,
) {
	// 域名同时绑在 bucket 上时，OSS 侧的证书是多余的，清掉避免两处过期时间打架
	if dom.Origin == entdomain.OriginBoth && dom.Bucket != "" {
		if cert := ossScanner.GetDomainCert(ctx, account, dom.Bucket, dom.Region, dom.Domain); cert != nil {
			info := oss.DomainInfo{Domain: dom.Domain, Bucket: dom.Bucket, Region: dom.Region, Account: account}
			if err := ossBinder.DeleteCert(ctx, info); err != nil {
				d.logger.Warn("删除 OSS 侧证书失败", zap.String("domain", dom.Domain), zap.Error(err))
			}
		}
	}

	if !cdnMgr.NeedDeploy(account.AccessKeyID, account.AccessKeySecret, dom.Domain) {
		return
	}
	d.logger.Info("检测到 CDN 域名证书需要重新部署", zap.String("domain", dom.Domain))
	if err := cdnMgr.DeployCert(account.AccessKeyID, account.AccessKeySecret, dom.Domain, dom.CertPem, dom.KeyPem); err != nil {
		d.logger.Warn("重新部署 CDN 证书失败", zap.String("domain", dom.Domain), zap.Error(err))
		return
	}
	d.logger.Info("CDN 证书重新部署成功", zap.String("domain", dom.Domain))
}

func (d *Daemon) redeployOSS(
	ctx context.Context,
	dom *ent.Domain,
	account *ent.CloudAccount,
	ossBinder *oss.CertBinder,
	ossScanner *oss.Scanner,
) {
	cname, err := ossScanner.FindCname(ctx, account, dom.Bucket, dom.Region, dom.Domain)
	if err != nil {
		d.logger.Warn("查询 OSS 域名绑定失败", zap.String("domain", dom.Domain), zap.Error(err))
		return
	}
	if cname == nil {
		// 域名已经不在这个 bucket 上了，交给下线对账处理，不在这里反复重绑
		return
	}
	if cname.Cert != nil && cname.Cert.ValidEndDate.After(time.Now()) {
		return
	}

	d.logger.Info("检测到 OSS 域名证书需要重新绑定", zap.String("domain", dom.Domain))
	info := oss.DomainInfo{Domain: dom.Domain, Bucket: dom.Bucket, Region: dom.Region, Account: account}
	if err := ossBinder.BindCert(ctx, info, dom.CertPem, dom.KeyPem); err != nil {
		d.logger.Warn("重新绑定 OSS 证书失败", zap.String("domain", dom.Domain), zap.Error(err))
		return
	}
	d.logger.Info("OSS 证书重新绑定成功", zap.String("domain", dom.Domain))
}

// renewCerts 为需要续期的域名创建任务，然后执行所有 pending 任务
func (d *Daemon) renewCerts(ctx context.Context, settings *ent.AppSettings) {
	d.createRenewTasks(ctx, settings.RenewBeforeDays)
	d.executePendingTasks(ctx, settings)
}

// createRenewTasks 查询需要续期的域名，为每个创建 pending 任务。
//
// 过滤掉四类域名：云上已消失的、被人工暂停托管的、已阻塞待人工处理的、还在退避窗口里的。
func (d *Daemon) createRenewTasks(ctx context.Context, renewBeforeDays int) {
	db := database.GetClient()
	now := time.Now()
	threshold := now.Add(time.Duration(renewBeforeDays) * 24 * time.Hour)

	domains, err := db.Domain.Query().
		Where(
			entdomain.PresenceEQ(entdomain.PresencePresent),
			entdomain.ManagedEQ(true),
			entdomain.BlockedReasonEQ(""),
			entdomain.Or(
				entdomain.NextRetryAtIsNil(),
				entdomain.NextRetryAtLTE(now),
			),
			entdomain.Or(
				entdomain.StatusEQ(entdomain.StatusPending),
				entdomain.StatusEQ(entdomain.StatusError),
				entdomain.And(
					entdomain.StatusEQ(entdomain.StatusActive),
					entdomain.ExpiresAtLT(threshold),
				),
			),
		).
		All(ctx)
	if err != nil {
		d.logger.Error("查询待续期域名失败", zap.Error(err))
		return
	}

	if len(domains) == 0 {
		return
	}

	// 查询已有 pending/running 任务中涉及的域名，避免重复创建
	existingTasks, err := db.RenewTask.Query().
		Where(
			renewtask.StatusIn(renewtask.StatusPending, renewtask.StatusRunning),
		).
		All(ctx)
	if err != nil {
		d.logger.Error("查询现有任务失败", zap.Error(err))
		return
	}

	busyDomains := make(map[string]bool)
	for _, t := range existingTasks {
		busyDomains[t.Domain] = true
	}

	allAccounts, err := db.CloudAccount.Query().Where(entcloudaccount.EnabledEQ(true)).All(ctx)
	if err != nil {
		d.logger.Error("查询启用的云账号失败", zap.Error(err))
		return
	}

	resolvers := database.DNSResolvers(ctx)

	// 为每个需要续期且不在处理中的域名创建独立任务
	for _, dom := range domains {
		if busyDomains[dom.Domain] {
			continue
		}

		// 提前探测 DNS 账号,zone 不可解析就走退避,避免每轮都白跑一次完整签发
		if _, _, findErr := alidnsutil.FindAccountForDomain(ctx, d.logger, dom.Domain, allAccounts, resolvers); findErr != nil {
			d.recordFailure(ctx, dom, fmt.Errorf("定位 DNS 账号失败: %w", findErr))
			continue
		}

		err := db.RenewTask.Create().
			SetDomain(dom.Domain).
			SetTrigger(renewtask.TriggerAuto).
			Exec(ctx)
		if err != nil {
			d.logger.Warn("创建续期任务失败", zap.String("domain", dom.Domain), zap.Error(err))
		} else {
			d.logger.Info("已创建续期任务", zap.String("domain", dom.Domain))
		}
	}
}

// executePendingTasks 查询所有 pending 任务并发执行。
//
// 一次 DNS-01 挑战要等 TXT 记录传播,通常几十秒到几分钟,串行跑几十个域名
// 会拖成几个小时。不同域名的挑战记录名互不相同,可以安全并发;
// 并发度压得比较低是为了不打爆阿里云 DNS 的写接口,也避免一次性向 CA 发起过多订单。
func (d *Daemon) executePendingTasks(ctx context.Context, settings *ent.AppSettings) {
	db := database.GetClient()
	tasks, err := db.RenewTask.Query().
		Where(renewtask.StatusEQ(renewtask.StatusPending)).
		All(ctx)
	if err != nil {
		d.logger.Error("查询 pending 任务失败", zap.Error(err))
		return
	}

	if len(tasks) == 0 {
		return
	}

	acmeMgr := acme.NewManager(d.logger)
	// 先把账号准备好,后面的并发就只剩签发,不会重复注册
	account, err := d.ensureACMEAccount(ctx, acmeMgr, settings)
	if err != nil {
		d.logger.Error("准备 ACME 账号失败,本轮跳过续期", zap.Error(err))
		return
	}

	d.logger.Info("待执行续期任务数量",
		zap.Int("count", len(tasks)),
		zap.Int("concurrency", renewConcurrency),
	)

	var wg sync.WaitGroup
	sem := make(chan struct{}, renewConcurrency)

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(t *ent.RenewTask) {
			defer wg.Done()
			defer func() { <-sem }()
			d.executeTask(ctx, t, acmeMgr, account, settings)
		}(task)
	}
	wg.Wait()
}

// purgeArchived 物理删除早已归档、且证书也已过期的记录。
//
// 归档记录本身是有价值的历史(这个域名什么时候上线、签过什么证书、什么时候下线),
// 所以留得比较久;retention 配成 0 表示永久保留。
func (d *Daemon) purgeArchived(ctx context.Context, retention string) {
	window := database.ParseRetention(retention)
	if window <= 0 {
		return
	}

	now := time.Now()
	deleted, err := database.GetClient().Domain.Delete().
		Where(
			entdomain.PresenceEQ(entdomain.PresenceArchived),
			entdomain.LastSeenAtLT(now.Add(-window)),
			// 证书还没过期就先留着:万一域名是被误摘的,记录里的证书还能直接复用
			entdomain.Or(
				entdomain.ExpiresAtIsNil(),
				entdomain.ExpiresAtLT(now),
			),
		).
		Exec(ctx)
	if err != nil {
		d.logger.Warn("清理归档域名记录失败", zap.Error(err))
		return
	}
	if deleted > 0 {
		d.logger.Info("已清理归档且证书过期的域名记录",
			zap.Int("count", deleted),
			zap.Duration("retention", window),
		)
	}
}

// taskLog 用于追加结构化日志到 DB
type taskLog struct {
	ctx     context.Context
	task    *ent.RenewTask
	entries []model.TaskLogEntry
}

func (l *taskLog) appendf(format string, args ...any) {
	l.entries = append(l.entries, model.TaskLogEntry{
		Time:    time.Now(),
		Content: fmt.Sprintf(format, args...),
	})
	_ = l.task.Update().SetLog(l.entries).Exec(l.ctx)
}

// executeTask 执行单个续期任务:预检 → 拿到可用证书 → 部署到目标侧。
//
// 三段是解耦的:预检不过就绝不去 ACME 签证书,证书够新就直接复用现有的去重试部署。
// 这样"签发成功、部署失败"的域名重试起来不再消耗 Let's Encrypt 配额。
func (d *Daemon) executeTask(ctx context.Context, task *ent.RenewTask, acmeMgr *acme.Manager, acmeAccount *acme.Account, settings *ent.AppSettings) {
	db := database.GetClient()
	log := d.logger.With(
		zap.String("task_id", task.ID),
		zap.String("domain", task.Domain),
	)
	tl := &taskLog{ctx: ctx, task: task}

	if task.Domain == "" {
		d.failTask(ctx, task, nil, log, tl, renewerr.Permanent("任务缺少 domain 字段"))
		return
	}

	// 标记为 running
	now := time.Now()
	_ = task.Update().SetStatus(renewtask.StatusRunning).SetStartedAt(now).Exec(ctx)

	tl.appendf("开始执行续期任务,域名: %s", task.Domain)
	log.Info("开始执行续期任务")

	dom, err := db.Domain.Query().
		Where(entdomain.DomainEQ(task.Domain)).
		Only(ctx)
	if err != nil {
		d.failTask(ctx, task, nil, log, tl, fmt.Errorf("查询域名 %s 失败: %w", task.Domain, err))
		return
	}

	// 任务可能是在状态变化之前排的队,执行前再确认一次
	if dom.Presence == entdomain.PresenceArchived {
		d.failTask(ctx, task, dom, log, tl, renewerr.Permanent("域名 %s 已归档(云上不存在),不再签发", task.Domain))
		return
	}
	if !dom.Managed {
		d.failTask(ctx, task, dom, log, tl, renewerr.Permanent("域名 %s 已暂停托管", task.Domain))
		return
	}

	account, err := db.CloudAccount.Query().
		Where(entcloudaccount.NameEQ(dom.AccountName)).
		Only(ctx)
	if err != nil {
		d.failTask(ctx, task, dom, log, tl, fmt.Errorf("找不到云账号 %s: %w", dom.AccountName, err))
		return
	}
	tl.appendf("所属云账号: %s,部署目标: %s", account.Name, dom.DeployTarget)

	// 第一步:目标侧还认不认这个域名
	if err := d.preflight(ctx, dom, account, tl); err != nil {
		d.failTask(ctx, task, dom, log, tl, err)
		return
	}

	// 第二步:拿到一张可用证书(够新就复用,不够才去签)
	certPEM, keyPEM, expiry, issued, err := d.ensureCert(ctx, dom, acmeMgr, acmeAccount, settings, tl)
	if err != nil {
		d.failTask(ctx, task, dom, log, tl, err)
		return
	}

	// 第三步:部署到目标侧
	if err := d.deploy(ctx, dom, account, certPEM, keyPEM, tl); err != nil {
		d.failTask(ctx, task, dom, log, tl, err)
		return
	}

	// 更新域名记录,清掉全部失败痕迹
	updater := dom.Update().
		SetExpiresAt(expiry).
		SetCertPem(certPEM).
		SetKeyPem(keyPEM).
		SetStatus(entdomain.StatusActive).
		SetErrorMessage("").
		SetErrorKind(entdomain.ErrorKindNone).
		SetRetryCount(0).
		ClearNextRetryAt().
		SetBlockedReason("")
	if issued {
		updater = updater.SetIssuedAt(time.Now())
	}
	if err := updater.Exec(ctx); err != nil {
		log.Warn("更新域名记录失败", zap.Error(err))
	}

	// 标记任务成功
	tl.appendf("✅ 任务完成")
	finished := time.Now()
	_ = task.Update().
		SetStatus(renewtask.StatusSuccess).
		SetFinishedAt(finished).
		SetErrorMessage("").
		SetLog(tl.entries).
		Exec(ctx)

	d.notifyDomainState(ctx, dom.Domain, alert.StateOK,
		fmt.Sprintf("✅ 证书部署成功\n域名: %s\n到期时间: %s", task.Domain, expiry.Format("2006-01-02")))

	log.Info("续期任务执行成功", zap.Time("expires_at", expiry))
}

// preflight 部署前确认目标侧还接受这个域名。
//
// 这是最省钱的一道闸:域名早就从 bucket 解绑、或从 CDN 下线时,
// 阿里云一定会在绑定阶段拒绝——没必要先去 Let's Encrypt 签一张注定用不上的证书。
func (d *Daemon) preflight(ctx context.Context, dom *ent.Domain, account *ent.CloudAccount, tl *taskLog) error {
	if dom.DeployTarget == entdomain.DeployTargetCdn {
		online, err := cdnpkg.NewManager(d.logger).CheckOnline(account.AccessKeyID, account.AccessKeySecret, dom.Domain)
		if err != nil {
			return fmt.Errorf("预检 CDN 域名 %s 失败: %w", dom.Domain, err)
		}
		if !online {
			return renewerr.Permanent("域名 %s 已不是账号 %s 下 online 的 CDN 加速域名", dom.Domain, account.Name)
		}
		tl.appendf("✓ 预检通过: %s 在 CDN 上处于 online", dom.Domain)
		return nil
	}

	cname, err := oss.NewScanner(d.logger).FindCname(ctx, account, dom.Bucket, dom.Region, dom.Domain)
	if err != nil {
		return fmt.Errorf("预检 OSS 域名 %s 失败: %w", dom.Domain, err)
	}
	if cname == nil {
		return renewerr.Permanent("域名 %s 已不在 bucket %s 的自定义域名列表中", dom.Domain, dom.Bucket)
	}
	if cname.Status != "Enabled" {
		return renewerr.Permanent("域名 %s 在 bucket %s 上的状态为 %s,非 Enabled", dom.Domain, dom.Bucket, cname.Status)
	}
	tl.appendf("✓ 预检通过: %s 仍绑定在 bucket %s 上", dom.Domain, dom.Bucket)
	return nil
}

// ensureCert 返回一张可用于部署的证书。
// issued 表示是否是本次新签发的——复用旧证书时不该刷新 issued_at。
func (d *Daemon) ensureCert(
	ctx context.Context,
	dom *ent.Domain,
	acmeMgr *acme.Manager,
	acmeAccount *acme.Account,
	settings *ent.AppSettings,
	tl *taskLog,
) (certPEM, keyPEM string, expiry time.Time, issued bool, err error) {
	threshold := time.Now().Add(time.Duration(settings.RenewBeforeDays) * 24 * time.Hour)
	if dom.CertPem != "" && dom.KeyPem != "" && dom.ExpiresAt != nil && dom.ExpiresAt.After(threshold) {
		tl.appendf("复用已有证书(到期 %s),跳过签发", dom.ExpiresAt.Format("2006-01-02"))
		return dom.CertPem, dom.KeyPem, *dom.ExpiresAt, false, nil
	}

	db := database.GetClient()
	allAccounts, err := db.CloudAccount.Query().Where(entcloudaccount.EnabledEQ(true)).All(ctx)
	if err != nil {
		return "", "", time.Time{}, false, fmt.Errorf("查询启用的云账号失败: %w", err)
	}

	// DNS-01 挑战所用账号可能与部署账号不同,探测 zone 实际所在账号
	resolvers := alidnsutil.ParseResolvers(settings.DNSResolvers)
	dnsAccount, zone, err := alidnsutil.FindAccountForDomain(ctx, d.logger, dom.Domain, allAccounts, resolvers)
	if err != nil {
		return "", "", time.Time{}, false, fmt.Errorf("定位 DNS 账号失败: %w", err)
	}
	if dnsAccount.Name != dom.AccountName {
		tl.appendf("DNS zone %s 在账号 %s 下,使用其 AK 做 DNS-01 挑战", zone, dnsAccount.Name)
	} else {
		tl.appendf("DNS zone %s 就在部署账号下", zone)
	}

	tl.appendf("正在申请证书 (DNS-01)...")
	result, err := acmeMgr.ObtainCert(acmeAccount, acme.ObtainOptions{
		Domains:         []string{dom.Domain},
		AccessKeyID:     dnsAccount.AccessKeyID,
		AccessKeySecret: dnsAccount.AccessKeySecret,
		Resolvers:       resolvers,
	})
	if err != nil {
		return "", "", time.Time{}, false, fmt.Errorf("申请证书失败: %w", err)
	}

	expiry, err = acme.ParseCertExpiry(result.CertPEM)
	if err != nil {
		return "", "", time.Time{}, false, fmt.Errorf("解析证书到期时间失败: %w", err)
	}
	tl.appendf("证书申请成功,到期时间: %s", expiry.Format("2006-01-02"))
	return result.CertPEM, result.KeyPEM, expiry, true, nil
}

// deploy 把证书部署到域名的目标侧
func (d *Daemon) deploy(ctx context.Context, dom *ent.Domain, account *ent.CloudAccount, certPEM, keyPEM string, tl *taskLog) error {
	if dom.DeployTarget == entdomain.DeployTargetCdn {
		cdnMgr := cdnpkg.NewManager(d.logger)
		tl.appendf("部署证书到 CDN 域名 %s...", dom.Domain)
		if err := cdnMgr.DeployCert(account.AccessKeyID, account.AccessKeySecret, dom.Domain, certPEM, keyPEM); err != nil {
			return fmt.Errorf("部署证书到 CDN %s 失败: %w", dom.Domain, err)
		}
		tl.appendf("✓ CDN 证书部署成功: %s", dom.Domain)

		// 域名同时绑在 bucket 上时清掉 OSS 侧的旧证书,避免两处过期时间不一致
		if dom.Origin == entdomain.OriginBoth && dom.Bucket != "" {
			ossScanner := oss.NewScanner(d.logger)
			ossBinder := oss.NewCertBinder(d.logger)
			info := oss.DomainInfo{Domain: dom.Domain, Bucket: dom.Bucket, Region: dom.Region, Account: account}
			if cert := ossScanner.GetDomainCert(ctx, account, dom.Bucket, dom.Region, dom.Domain); cert != nil {
				if err := ossBinder.DeleteCert(ctx, info); err != nil {
					tl.appendf("⚠ 删除 OSS 侧证书失败: %v", err)
				} else {
					tl.appendf("✓ 已清理 OSS 侧证书: %s", dom.Domain)
				}
			}
		}
		return nil
	}

	tl.appendf("绑定证书到 OSS 自定义域名 %s...", dom.Domain)
	info := oss.DomainInfo{Domain: dom.Domain, Bucket: dom.Bucket, Region: dom.Region, Account: account}
	if err := oss.NewCertBinder(d.logger).BindCert(ctx, info, certPEM, keyPEM); err != nil {
		return fmt.Errorf("绑定证书到 OSS %s 失败: %w", dom.Domain, err)
	}
	tl.appendf("✓ OSS 证书绑定成功: %s (bucket: %s)", dom.Domain, dom.Bucket)
	return nil
}

// failTask 标记任务失败，并按错误分类更新域名的重试状态
func (d *Daemon) failTask(ctx context.Context, task *ent.RenewTask, dom *ent.Domain, log *zap.Logger, tl *taskLog, err error) {
	log.Error("续期任务失败", zap.Error(err))
	tl.appendf("❌ 任务失败: %v", err)

	finished := time.Now()
	_ = task.Update().
		SetStatus(renewtask.StatusFailed).
		SetFinishedAt(finished).
		SetErrorMessage(err.Error()).
		SetLog(tl.entries).
		Exec(ctx)

	if dom == nil {
		// 域名记录都查不到，只能直报
		database.SendToAllFeishuChannels(ctx,
			fmt.Sprintf("❌ 证书续期失败\n域名: %s\n错误: %s", task.Domain, err.Error()), d.logger)
		return
	}
	d.recordFailure(ctx, dom, err)
}

// recordFailure 按错误分类推进域名的重试状态，并在必要时告警。
//
// 可重试的错误按指数退避排下一次；重试到头或本就无解的错误写入 blocked_reason，
// 从此不再自动排任务——等人工修好后手动触发续期来解除。
func (d *Daemon) recordFailure(ctx context.Context, dom *ent.Domain, err error) {
	kind := renewerr.Classify(err)
	attempts := dom.RetryCount + 1
	now := time.Now()
	next := renewerr.Backoff(kind, attempts, now)

	updater := dom.Update().
		SetStatus(entdomain.StatusError).
		SetErrorMessage(err.Error()).
		SetErrorKind(kind).
		SetRetryCount(attempts)

	state := alert.StateFailed
	text := fmt.Sprintf("❌ 证书续期失败\n域名: %s\n错误: %s", dom.Domain, err.Error())

	if next != nil {
		updater = updater.SetNextRetryAt(*next)
		text += fmt.Sprintf("\n第 %d 次失败，将在 %s 后重试", attempts, next.Format("2006-01-02 15:04"))
	} else {
		state = alert.StateBlocked
		updater = updater.
			ClearNextRetryAt().
			SetBlockedReason(err.Error())
		text += fmt.Sprintf("\n第 %d 次失败，已停止自动重试（%s），需人工处理后手动触发续期", attempts, kindLabel(kind))
	}

	if err := updater.Exec(ctx); err != nil {
		d.logger.Warn("更新域名失败状态出错", zap.String("domain", dom.Domain), zap.Error(err))
	}

	d.notifyDomainState(ctx, dom.Domain, state, text)
}

func kindLabel(kind entdomain.ErrorKind) string {
	switch kind {
	case entdomain.ErrorKindPermanent:
		return "需人工介入的错误"
	case entdomain.ErrorKindRateLimited:
		return "被限速"
	case entdomain.ErrorKindTransient:
		return "重试次数已用尽"
	default:
		return string(kind)
	}
}
