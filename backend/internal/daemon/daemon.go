package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/acme"
	cdnpkg "github.com/solarhell/certship/internal/cdn"
	"github.com/solarhell/certship/internal/domainsync"
	"github.com/solarhell/certship/internal/oss"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entcloudaccount "github.com/solarhell/certship/pkg/ent/cloudaccount"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
	"github.com/solarhell/certship/pkg/ent/renewtask"
	"github.com/solarhell/certship/pkg/model"
)

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
	acmeMgr := acme.NewManager(d.logger)
	d.executeTask(ctx, task, acmeMgr)
	return nil
}

// cycle 一次完整周期：同步 OSS → 检查并重新部署 → 续期
func (d *Daemon) cycle(ctx context.Context) {
	d.logger.Info("开始扫描周期")

	accounts := database.GetEnabledCloudAccounts(ctx)
	if len(accounts) == 0 {
		d.logger.Warn("数据库中无可用的阿里云账号，跳过本次扫描")
		return
	}

	scanner := oss.NewScanner(d.logger)
	domains := scanner.ScanAll(ctx, accounts)
	d.logger.Info("发现自定义域名", zap.Int("count", len(domains)))

	// Phase 1: 将 OSS 现状同步到 DB
	domainsync.Sync(ctx, d.logger, domains)

	// Phase 2: 检查已有证书是否需要重新部署（CDN↔OSS 切换场景）
	d.checkAndRedeploy(ctx)

	// Phase 3: 续期需要处理的证书
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		d.logger.Error("读取配置失败", zap.Error(err))
		return
	}
	d.renewCerts(ctx, settings.RenewBeforeDays)

	d.logger.Info("扫描周期完成")
}

// checkAndRedeploy 检查 DB 中有有效证书的域名，目标侧（OSS/CDN）是否真正生效
// 如果 DB 证书未过期但目标侧证书过期或缺失，用 DB 中的证书重新部署
func (d *Daemon) checkAndRedeploy(ctx context.Context) {
	db := database.GetClient()
	cdnMgr := cdnpkg.NewManager(d.logger)
	ossBinder := oss.NewCertBinder(d.logger)

	// 查询 DB 中有有效证书的域名
	activeDomains, err := db.Domain.Query().
		Where(
			entdomain.StatusEQ(entdomain.StatusActive),
			entdomain.CertPemNEQ(""),
			entdomain.ExpiresAtGT(time.Now()),
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

		isCDN := cdnMgr.IsCDNDomain(account.AccessKeyID, account.AccessKeySecret, dom.Domain)

		// 更新 deploy_target
		if isCDN {
			_ = dom.Update().SetDeployTarget(entdomain.DeployTargetCdn).Exec(ctx)
		} else {
			_ = dom.Update().SetDeployTarget(entdomain.DeployTargetOss).Exec(ctx)
		}

		domainInfo := oss.DomainInfo{Domain: dom.Domain, Bucket: dom.Bucket, Region: dom.Region, Account: account}

		if isCDN {
			// CDN 域名：如果 OSS 侧有证书则清理
			ossScanner := oss.NewScanner(d.logger)
			if ossCert := ossScanner.GetDomainCert(ctx, account, dom.Bucket, dom.Region, dom.Domain); ossCert != nil {
				if err := ossBinder.DeleteCert(ctx, domainInfo); err != nil {
					d.logger.Warn("删除 OSS 侧证书失败", zap.String("domain", dom.Domain), zap.Error(err))
				}
			}
			// 检查 CDN 侧证书是否需要部署
			if cdnMgr.NeedDeploy(account.AccessKeyID, account.AccessKeySecret, dom.Domain) {
				d.logger.Info("检测到 CDN 域名证书需要重新部署", zap.String("domain", dom.Domain))
				if err := cdnMgr.DeployCert(account.AccessKeyID, account.AccessKeySecret, dom.Domain, dom.CertPem, dom.KeyPem); err != nil {
					d.logger.Warn("重新部署 CDN 证书失败", zap.String("domain", dom.Domain), zap.Error(err))
				} else {
					d.logger.Info("CDN 证书重新部署成功", zap.String("domain", dom.Domain))
				}
			}
		} else {
			// 检查 OSS 侧：如果 OSS 侧证书过期但 DB 有有效证书，重新绑定
			scanner := oss.NewScanner(d.logger)
			ossCert := scanner.GetDomainCert(ctx, account, dom.Bucket, dom.Region, dom.Domain)
			if ossCert == nil || ossCert.ValidEndDate.Before(time.Now()) {
				d.logger.Info("检测到 OSS 域名证书需要重新绑定", zap.String("domain", dom.Domain))
				if err := ossBinder.BindCert(ctx, domainInfo, dom.CertPem, dom.KeyPem); err != nil {
					d.logger.Warn("重新绑定 OSS 证书失败", zap.String("domain", dom.Domain), zap.Error(err))
				} else {
					d.logger.Info("OSS 证书重新绑定成功", zap.String("domain", dom.Domain))
				}
			}
		}
	}
}

// renewCerts 为需要续期的域名创建任务，然后执行所有 pending 任务
func (d *Daemon) renewCerts(ctx context.Context, renewBeforeDays int) {
	d.createRenewTasks(ctx, renewBeforeDays)
	d.executePendingTasks(ctx)
}

// createRenewTasks 查询需要续期的域名，为每个创建 pending 任务（跳过已有 pending/running 任务的域名）
func (d *Daemon) createRenewTasks(ctx context.Context, renewBeforeDays int) {
	db := database.GetClient()
	threshold := time.Now().Add(time.Duration(renewBeforeDays) * 24 * time.Hour)

	domains, err := db.Domain.Query().
		Where(
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
		for _, dn := range t.Domains {
			busyDomains[dn] = true
		}
	}

	// 为每个需要续期且不在处理中的域名创建独立任务
	for _, dom := range domains {
		if busyDomains[dom.Domain] {
			continue
		}
		err := db.RenewTask.Create().
			SetDomains([]string{dom.Domain}).
			SetTrigger(renewtask.TriggerAuto).
			Exec(ctx)
		if err != nil {
			d.logger.Warn("创建续期任务失败", zap.String("domain", dom.Domain), zap.Error(err))
		} else {
			d.logger.Info("已创建续期任务", zap.String("domain", dom.Domain))
		}
	}
}

// executePendingTasks 查询所有 pending 任务并逐个执行
func (d *Daemon) executePendingTasks(ctx context.Context) {
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

	d.logger.Info("待执行续期任务数量", zap.Int("count", len(tasks)))

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return
		default:
		}
		d.executeTask(ctx, task, acmeMgr)
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

// executeTask 执行单个续期任务：申请 SAN 证书并绑定到各域名对应的 OSS/CDN
func (d *Daemon) executeTask(ctx context.Context, task *ent.RenewTask, acmeMgr *acme.Manager) {
	db := database.GetClient()
	log := d.logger.With(
		zap.String("task_id", task.ID),
		zap.Strings("domains", task.Domains),
	)
	tl := &taskLog{ctx: ctx, task: task}

	// 标记为 running
	now := time.Now()
	_ = task.Update().SetStatus(renewtask.StatusRunning).SetStartedAt(now).Exec(ctx)

	tl.appendf("开始执行续期任务，域名: %s", strings.Join(task.Domains, ", "))
	log.Info("开始执行续期任务")

	// 查找第一个域名对应的云账号
	firstDomain, err := db.Domain.Query().
		Where(entdomain.DomainEQ(task.Domains[0])).
		Only(ctx)
	if err != nil {
		d.failTask(ctx, task, log, tl, fmt.Errorf("查询域名 %s 失败: %w", task.Domains[0], err))
		return
	}

	account, err := db.CloudAccount.Query().
		Where(entcloudaccount.NameEQ(firstDomain.AccountName)).
		Only(ctx)
	if err != nil {
		d.failTask(ctx, task, log, tl, fmt.Errorf("找不到云账号 %s: %w", firstDomain.AccountName, err))
		return
	}
	tl.appendf("使用云账号: %s", account.Name)

	accountKeyPEM, regJSON, err := database.GetACMEAccount(ctx)
	if err != nil {
		d.failTask(ctx, task, log, tl, fmt.Errorf("读取 ACME 账号信息失败: %w", err))
		return
	}

	// 申请 SAN 多域名证书
	tl.appendf("正在申请 Let's Encrypt 证书 (DNS-01)...")
	result, err := acmeMgr.ObtainCert(task.Domains, account.AccessKeyID, account.AccessKeySecret, accountKeyPEM, regJSON)
	if err != nil {
		d.failTask(ctx, task, log, tl, fmt.Errorf("申请证书失败: %w", err))
		return
	}
	expiry, _ := acme.ParseCertExpiry(result.CertPEM)
	tl.appendf("证书申请成功，到期时间: %s", expiry.Format("2006-01-02"))

	if err := database.SaveACMEAccount(ctx, result.AccountKeyPEM, result.RegistrationJSON); err != nil {
		log.Warn("保存 ACME 账号信息失败", zap.Error(err))
	}

	// 为每个域名部署证书
	ossBinder := oss.NewCertBinder(d.logger)
	cdnMgr := cdnpkg.NewManager(d.logger)
	for _, domainName := range task.Domains {
		dom, err := db.Domain.Query().
			Where(entdomain.DomainEQ(domainName)).
			Only(ctx)
		if err != nil {
			tl.appendf("⚠ 查询域名 %s 失败，跳过: %v", domainName, err)
			continue
		}

		domainInfo := oss.DomainInfo{
			Domain:  dom.Domain,
			Bucket:  dom.Bucket,
			Region:  dom.Region,
			Account: account,
		}

		// 动态检测 CDN/OSS
		if cdnMgr.IsCDNDomain(account.AccessKeyID, account.AccessKeySecret, dom.Domain) {
			tl.appendf("检测到 %s 为 CDN 域名，部署到 CDN...", domainName)
			if err := cdnMgr.DeployCert(account.AccessKeyID, account.AccessKeySecret, dom.Domain, result.CertPEM, result.KeyPEM); err != nil {
				d.failTask(ctx, task, log, tl, fmt.Errorf("部署证书到 CDN %s 失败: %w", domainName, err))
				return
			}
			tl.appendf("✓ CDN 证书部署成功: %s", domainName)
			// 清理 OSS 侧证书
			ossScanner := oss.NewScanner(d.logger)
			if ossCert := ossScanner.GetDomainCert(ctx, account, dom.Bucket, dom.Region, dom.Domain); ossCert != nil {
				if err := ossBinder.DeleteCert(ctx, domainInfo); err != nil {
					tl.appendf("⚠ 删除 OSS 侧证书失败: %v", err)
				} else {
					tl.appendf("✓ 已清理 OSS 侧证书: %s", domainName)
				}
			}
		} else {
			tl.appendf("检测到 %s 为 OSS 直连域名，绑定到 OSS...", domainName)
			if err := ossBinder.BindCert(ctx, domainInfo, result.CertPEM, result.KeyPEM); err != nil {
				d.failTask(ctx, task, log, tl, fmt.Errorf("绑定证书到 OSS %s 失败: %w", domainName, err))
				return
			}
			tl.appendf("✓ OSS 证书绑定成功: %s (bucket: %s)", domainName, dom.Bucket)
		}

		// 更新域名记录
		_ = dom.Update().
			SetIssuedAt(time.Now()).
			SetExpiresAt(expiry).
			SetCertPem(result.CertPEM).
			SetKeyPem(result.KeyPEM).
			SetStatus(entdomain.StatusActive).
			SetErrorMessage("").
			Exec(ctx)
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

	domainList := strings.Join(task.Domains, ", ")
	database.SendToAllFeishuChannels(ctx,
		fmt.Sprintf("✅ 证书申请成功\n域名: %s\n到期时间: %s", domainList, expiry.Format("2006-01-02")),
		d.logger,
	)

	log.Info("续期任务执行成功", zap.Time("expires_at", expiry))
}

// failTask 标记任务失败，更新关联域名状态，发送通知
func (d *Daemon) failTask(ctx context.Context, task *ent.RenewTask, log *zap.Logger, tl *taskLog, err error) {
	log.Error("续期任务失败", zap.Error(err))
	tl.appendf("❌ 任务失败: %v", err)

	finished := time.Now()
	_ = task.Update().
		SetStatus(renewtask.StatusFailed).
		SetFinishedAt(finished).
		SetErrorMessage(err.Error()).
		SetLog(tl.entries).
		Exec(ctx)

	// 更新关联域名状态为 error
	db := database.GetClient()
	for _, domainName := range task.Domains {
		_ = db.Domain.Update().
			Where(entdomain.DomainEQ(domainName)).
			SetStatus(entdomain.StatusError).
			SetErrorMessage(err.Error()).
			Exec(ctx)
	}

	domainList := strings.Join(task.Domains, ", ")
	database.SendToAllFeishuChannels(ctx,
		fmt.Sprintf("❌ 证书续期失败\n域名: %s\n错误: %s", domainList, err.Error()),
		d.logger,
	)
}
