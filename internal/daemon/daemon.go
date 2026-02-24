package daemon

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/acme"
	"github.com/solarhell/certship/internal/oss"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
	entcloudaccount "github.com/solarhell/certship/pkg/ent/cloudaccount"
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

// cycle 一次完整周期：先同步 OSS 现状到 DB，再续期需要处理的证书
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
	d.syncDomains(ctx, domains)

	// Phase 2: 续期需要处理的证书（测试 Phase 1 时暂时跳过）
	// settings, err := database.GetAppSettings(ctx)
	// if err != nil {
	// 	d.logger.Error("读取配置失败", zap.Error(err))
	// 	return
	// }
	// d.renewCerts(ctx, settings.RenewBeforeDays)

	d.logger.Info("扫描周期完成")
}

// syncDomains 将扫描到的域名及其 OSS 侧证书信息同步到 DB
// 已有自颁发证书（cert_pem 非空）的记录，不覆盖证书日期，只更新 bucket/region/account
func (d *Daemon) syncDomains(ctx context.Context, domains []oss.DomainInfo) {
	db := database.GetClient()
	for _, info := range domains {
		select {
		case <-ctx.Done():
			return
		default:
		}

		existing, err := db.Domain.Query().
			Where(entdomain.DomainEQ(info.Domain)).
			Only(ctx)

		if ent.IsNotFound(err) {
			// 新发现的域名
			creator := db.Domain.Create().
				SetDomain(info.Domain).
				SetBucket(info.Bucket).
				SetRegion(info.Region).
				SetAccountName(info.Account.Name)

			if info.OSSCert != nil {
				creator = creator.
					SetIssuedAt(info.OSSCert.ValidStartDate).
					SetExpiresAt(info.OSSCert.ValidEndDate).
					SetStatus(entdomain.StatusActive)
			} else {
				creator = creator.SetStatus(entdomain.StatusPending)
			}

			if err := creator.Exec(ctx); err != nil {
				d.logger.Warn("写入新域名记录失败",
					zap.String("domain", info.Domain),
					zap.Error(err),
				)
			} else {
				d.logger.Info("发现新域名，已写入数据库",
					zap.String("domain", info.Domain),
					zap.Bool("has_cert", info.OSSCert != nil),
				)
			}
			continue
		}

		if err != nil {
			d.logger.Warn("查询域名记录失败", zap.String("domain", info.Domain), zap.Error(err))
			continue
		}

		// 已存在：始终更新 bucket/region/account
		updater := existing.Update().
			SetBucket(info.Bucket).
			SetRegion(info.Region).
			SetAccountName(info.Account.Name)

		// 若 DB 中无自颁发证书，则用 OSS 侧信息更新日期
		if existing.CertPem == "" && info.OSSCert != nil {
			updater = updater.
				SetIssuedAt(info.OSSCert.ValidStartDate).
				SetExpiresAt(info.OSSCert.ValidEndDate).
				SetStatus(entdomain.StatusActive)
		} else if existing.CertPem == "" && info.OSSCert == nil {
			updater = updater.SetStatus(entdomain.StatusPending)
		}

		if err := updater.Exec(ctx); err != nil {
			d.logger.Warn("更新域名记录失败", zap.String("domain", info.Domain), zap.Error(err))
		}
	}
}

// renewCerts 查询 DB 中需要续期的域名并进行证书申请和绑定
func (d *Daemon) renewCerts(ctx context.Context, renewBeforeDays int) {
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		d.logger.Error("读取 ACME 配置失败", zap.Error(err))
		return
	}
	acmeMgr := acme.NewManager(settings, d.logger)

	// 查询需要续期的域名：pending、error、或 active 但即将到期
	threshold := time.Now().Add(time.Duration(renewBeforeDays) * 24 * time.Hour)

	certs, err := database.GetClient().Domain.Query().
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
		d.logger.Error("查询待续期证书失败", zap.Error(err))
		return
	}

	d.logger.Info("待续期域名数量", zap.Int("count", len(certs)))

	for _, cert := range certs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		d.processCert(ctx, cert, acmeMgr)
	}
}

func (d *Daemon) processCert(ctx context.Context, cert *ent.Domain, acmeMgr *acme.Manager) {
	log := d.logger.With(
		zap.String("domain", cert.Domain),
		zap.String("bucket", cert.Bucket),
		zap.String("account", cert.AccountName),
	)

	log.Info("开始处理证书续期")

	// 查找对应的云账号
	account, err := database.GetClient().CloudAccount.Query().
		Where(entcloudaccount.NameEQ(cert.AccountName)).
		Only(ctx)
	if err != nil {
		log.Error("找不到对应云账号", zap.Error(err))
		return
	}

	accountKeyPEM, regJSON, err := database.GetACMEAccount(ctx)
	if err != nil {
		log.Error("读取 ACME 账号信息失败", zap.Error(err))
		return
	}

	result, err := acmeMgr.ObtainCert(cert.Domain, account.AccessKeyID, account.AccessKeySecret, accountKeyPEM, regJSON)
	if err != nil {
		log.Error("申请证书失败", zap.Error(err))
		d.saveErrorToDB(ctx, cert, err)
		database.SendToAllFeishuChannels(ctx,
			fmt.Sprintf("❌ 证书申请失败\n域名: %s\n错误: %s", cert.Domain, err.Error()),
			d.logger,
		)
		return
	}

	if err := database.SaveACMEAccount(ctx, result.AccountKeyPEM, result.RegistrationJSON); err != nil {
		log.Warn("保存 ACME 账号信息失败", zap.Error(err))
	}

	domainInfo := oss.DomainInfo{
		Domain:  cert.Domain,
		Bucket:  cert.Bucket,
		Region:  cert.Region,
		Account: account,
	}
	binder := oss.NewCertBinder(d.logger)
	if err := binder.BindCert(ctx, domainInfo, result.CertPEM, result.KeyPEM); err != nil {
		log.Error("绑定证书到 OSS 失败", zap.Error(err))
		d.saveErrorToDB(ctx, cert, err)
		database.SendToAllFeishuChannels(ctx,
			fmt.Sprintf("❌ 证书绑定失败\n域名: %s\n错误: %s", cert.Domain, err.Error()),
			d.logger,
		)
		return
	}

	expiry, _ := acme.ParseCertExpiry(result.CertPEM)
	d.saveCertToDB(ctx, cert, result, expiry)

	database.SendToAllFeishuChannels(ctx,
		fmt.Sprintf("✅ 证书申请成功\n域名: %s\n到期时间: %s", cert.Domain, expiry.Format("2006-01-02")),
		d.logger,
	)

	log.Info("证书申请并绑定成功", zap.Time("expires_at", expiry))
}

func (d *Daemon) saveCertToDB(ctx context.Context, cert *ent.Domain, result *acme.CertResult, expiresAt time.Time) {
	err := cert.Update().
		SetIssuedAt(time.Now()).
		SetExpiresAt(expiresAt).
		SetCertPem(result.CertPEM).
		SetKeyPem(result.KeyPEM).
		SetStatus(entdomain.StatusActive).
		SetErrorMessage("").
		Exec(ctx)
	if err != nil {
		d.logger.Warn("写入证书记录到数据库失败",
			zap.String("domain", cert.Domain),
			zap.Error(err),
		)
	}
}

func (d *Daemon) saveErrorToDB(ctx context.Context, cert *ent.Domain, certErr error) {
	_ = cert.Update().
		SetStatus(entdomain.StatusError).
		SetErrorMessage(certErr.Error()).
		Exec(ctx)
}
