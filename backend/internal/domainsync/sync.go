// Package domainsync 把扫描到的 OSS 自定义域名同步到数据库。
package domainsync

import (
	"context"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/oss"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

// Stats 描述一次同步的结果统计。
type Stats struct {
	Added   int
	Updated int
}

// Sync 把扫描结果写入 DB；已存在的 domain 只更新 bucket/region/account（以及在未自颁发证书时的 OSS 侧日期）。
// 对已自颁发证书（cert_pem 非空）的记录不会覆盖证书日期。
func Sync(ctx context.Context, logger *zap.Logger, domains []oss.DomainInfo) Stats {
	db := database.GetClient()
	var stats Stats

	for _, info := range domains {
		select {
		case <-ctx.Done():
			return stats
		default:
		}

		existing, err := db.Domain.Query().
			Where(entdomain.DomainEQ(info.Domain)).
			Only(ctx)

		if ent.IsNotFound(err) {
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
				logger.Warn("写入新域名记录失败",
					zap.String("domain", info.Domain),
					zap.Error(err),
				)
			} else {
				stats.Added++
				logger.Info("发现新域名，已写入数据库",
					zap.String("domain", info.Domain),
					zap.Bool("has_cert", info.OSSCert != nil),
				)
			}
			continue
		}

		if err != nil {
			logger.Warn("查询域名记录失败", zap.String("domain", info.Domain), zap.Error(err))
			continue
		}

		updater := existing.Update().
			SetBucket(info.Bucket).
			SetRegion(info.Region).
			SetAccountName(info.Account.Name)

		if existing.CertPem == "" && info.OSSCert != nil {
			updater = updater.
				SetIssuedAt(info.OSSCert.ValidStartDate).
				SetExpiresAt(info.OSSCert.ValidEndDate).
				SetStatus(entdomain.StatusActive)
		} else if existing.CertPem == "" && info.OSSCert == nil {
			updater = updater.SetStatus(entdomain.StatusPending)
		}

		if err := updater.Exec(ctx); err != nil {
			logger.Warn("更新域名记录失败", zap.String("domain", info.Domain), zap.Error(err))
			continue
		}
		stats.Updated++
	}
	return stats
}
