// Package domainsync 把云上域名现状对账到数据库:新增、更新,以及识别已经下线的域名。
package domainsync

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

// Stats 描述一次对账的结果
type Stats struct {
	Added   int
	Updated int
	// Revived/Missing/Archived 记录发生状态跃迁的域名,供上层告警
	Revived  []string
	Missing  []string
	Archived []string
	// Unobservable 本轮扫描看不见、因此被放过的记录数
	Unobservable int
}

// Reconcile 把 result 描述的云上现状对账到 DB。
//
// 两个方向:
//   - 云上有 → DB 新增/更新,并刷新 last_seen_at;已归档的域名重新出现会自动复活
//   - 云上没有 → 仅在本轮确实观察得到该记录时才推进 present → missing → archived
//
// missingGrace 是判定 missing 前的宽限期,archiveAfter 是 missing 后再等多久归档。
// 宽限期的意义:运维迁移 bucket 时常见"先解绑再绑",中间那一下不该被判死。
func Reconcile(
	ctx context.Context,
	logger *zap.Logger,
	result discovery.Result,
	missingGrace, archiveAfter time.Duration,
) Stats {
	var stats Stats
	now := time.Now()

	seen := make(map[string]struct{}, len(result.Domains))
	for _, info := range result.Domains {
		select {
		case <-ctx.Done():
			return stats
		default:
		}
		seen[info.Domain] = struct{}{}
		upsert(ctx, logger, info, now, &stats)
	}

	// 整轮一个账号都没扫通时不做任何下线判定,避免把全量域名判死
	if result.Coverage.IsEmpty() {
		logger.Warn("本轮没有任何账号扫描成功,跳过下线对账")
		return stats
	}

	reconcileMissing(ctx, logger, result, seen, now, missingGrace, archiveAfter, &stats)
	return stats
}

// upsert 写入或更新一个云上确实存在的域名
func upsert(ctx context.Context, logger *zap.Logger, info discovery.DomainInfo, now time.Time, stats *Stats) {
	db := database.GetClient()

	existing, err := db.Domain.Query().
		Where(entdomain.DomainEQ(info.Domain)).
		Only(ctx)

	if ent.IsNotFound(err) {
		creator := db.Domain.Create().
			SetDomain(info.Domain).
			SetBucket(info.Bucket).
			SetRegion(info.Region).
			SetAccountName(info.Account.Name).
			SetOrigin(info.Origin).
			SetDeployTarget(info.DeployTarget()).
			SetPresence(entdomain.PresencePresent).
			SetLastSeenAt(now)

		if cert := cloudCert(info); cert != nil {
			creator = creator.
				SetIssuedAt(cert.ValidStartDate).
				SetExpiresAt(cert.ValidEndDate).
				SetStatus(entdomain.StatusActive)
		} else {
			creator = creator.SetStatus(entdomain.StatusPending)
		}

		if err := creator.Exec(ctx); err != nil {
			logger.Warn("写入新域名记录失败", zap.String("domain", info.Domain), zap.Error(err))
			return
		}
		stats.Added++
		logger.Info("发现新域名,已写入数据库",
			zap.String("domain", info.Domain),
			zap.String("origin", string(info.Origin)),
			zap.String("deploy_target", string(info.DeployTarget())),
			zap.Bool("has_cert", cloudCert(info) != nil),
		)
		return
	}

	if err != nil {
		logger.Warn("查询域名记录失败", zap.String("domain", info.Domain), zap.Error(err))
		return
	}

	updater := existing.Update().
		SetBucket(info.Bucket).
		SetRegion(info.Region).
		SetAccountName(info.Account.Name).
		SetOrigin(info.Origin).
		SetDeployTarget(info.DeployTarget()).
		SetPresence(entdomain.PresencePresent).
		SetLastSeenAt(now)

	// 域名重新出现:清掉下线期间累积的失败状态,给它一个干净的重来机会
	if existing.Presence != entdomain.PresencePresent {
		updater = updater.
			SetRetryCount(0).
			ClearNextRetryAt().
			SetErrorKind(entdomain.ErrorKindNone).
			SetBlockedReason("")
		stats.Revived = append(stats.Revived, info.Domain)
		logger.Info("下线的域名重新出现,已恢复托管",
			zap.String("domain", info.Domain),
			zap.String("from", string(existing.Presence)),
		)
	}

	// 只有在没有自颁发证书时才拿云上日期覆盖,否则会把自己签的证书信息冲掉
	if existing.CertPem == "" {
		if cert := cloudCert(info); cert != nil {
			updater = updater.
				SetIssuedAt(cert.ValidStartDate).
				SetExpiresAt(cert.ValidEndDate).
				SetStatus(entdomain.StatusActive)
		} else {
			updater = updater.SetStatus(entdomain.StatusPending)
		}
	}

	if err := updater.Exec(ctx); err != nil {
		logger.Warn("更新域名记录失败", zap.String("domain", info.Domain), zap.Error(err))
		return
	}
	stats.Updated++
}

// reconcileMissing 处理本轮没扫到的记录:推进 present → missing → archived
func reconcileMissing(
	ctx context.Context,
	logger *zap.Logger,
	result discovery.Result,
	seen map[string]struct{},
	now time.Time,
	missingGrace, archiveAfter time.Duration,
	stats *Stats,
) {
	db := database.GetClient()

	records, err := db.Domain.Query().
		Where(entdomain.PresenceNEQ(entdomain.PresenceArchived)).
		All(ctx)
	if err != nil {
		logger.Error("查询域名记录失败,跳过下线对账", zap.Error(err))
		return
	}

	for _, rec := range records {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if _, ok := seen[rec.Domain]; ok {
			continue
		}
		if !result.Coverage.CanObserve(rec.Origin, rec.AccountName, rec.Bucket) {
			stats.Unobservable++
			logger.Debug("本轮扫描覆盖不到该记录,跳过下线判定",
				zap.String("domain", rec.Domain),
				zap.String("origin", string(rec.Origin)),
			)
			continue
		}

		// 存量记录没有 last_seen_at:从本轮开始计时,而不是立刻判死
		if rec.LastSeenAt == nil {
			if err := rec.Update().SetLastSeenAt(now).Exec(ctx); err != nil {
				logger.Warn("初始化 last_seen_at 失败", zap.String("domain", rec.Domain), zap.Error(err))
			}
			continue
		}

		absent := now.Sub(*rec.LastSeenAt)
		next, changed := nextPresence(rec.Presence, absent, missingGrace, archiveAfter)
		if !changed {
			continue
		}

		if err := rec.Update().SetPresence(next).Exec(ctx); err != nil {
			logger.Warn("更新域名存在性失败",
				zap.String("domain", rec.Domain),
				zap.String("to", string(next)),
				zap.Error(err),
			)
			continue
		}

		switch next {
		case entdomain.PresenceArchived:
			stats.Archived = append(stats.Archived, rec.Domain)
			logger.Info("域名已在云上消失足够久,归档并停止续期",
				zap.String("domain", rec.Domain),
				zap.Duration("absent", absent),
			)
		case entdomain.PresenceMissing:
			stats.Missing = append(stats.Missing, rec.Domain)
			logger.Info("域名连续多轮未在云上扫到,暂停续期",
				zap.String("domain", rec.Domain),
				zap.Duration("absent", absent),
			)
		}
	}
}

// nextPresence 根据已缺席时长决定该迁移到哪个存在性状态。
//
// present --missingGrace--> missing --archiveAfter--> archived,只进不退;
// 回到 present 只能靠域名重新被扫到(见 upsert)。
func nextPresence(
	current entdomain.Presence,
	absent, missingGrace, archiveAfter time.Duration,
) (entdomain.Presence, bool) {
	var want entdomain.Presence
	switch {
	case absent >= missingGrace+archiveAfter:
		want = entdomain.PresenceArchived
	case absent >= missingGrace:
		want = entdomain.PresenceMissing
	default:
		return current, false
	}
	if want == current {
		return current, false
	}
	return want, true
}

// cloudCert 返回该域名在其部署目标侧已绑定的证书信息
func cloudCert(info discovery.DomainInfo) *discovery.CertInfo {
	if info.DeployTarget() == entdomain.DeployTargetCdn {
		return info.CDNCert
	}
	return info.OSSCert
}
