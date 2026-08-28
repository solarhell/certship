package database

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/alidns"
	"github.com/solarhell/certship/pkg/ent"
	entappsettings "github.com/solarhell/certship/pkg/ent/appsettings"
	entcloudaccount "github.com/solarhell/certship/pkg/ent/cloudaccount"
)

const AppSettingsID = "default"

// ensureAppSettings 保证全局配置行存在。
//
// 所有字段都有默认值,首次启动时自动落一行,不需要人工往库里插数据——
// 否则 GetAppSettings 会一直 NotFound,daemon 起不来。
func ensureAppSettings(ctx context.Context) error {
	db := GetClient()
	exists, err := db.AppSettings.Query().
		Where(entappsettings.IDEQ(AppSettingsID)).
		Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := db.AppSettings.Create().SetID(AppSettingsID).Exec(ctx); err != nil {
		return err
	}
	zap.L().Info("已创建默认全局配置", zap.String("id", AppSettingsID))
	return nil
}

// GetAppSettings 从 DB 读取全局配置
func GetAppSettings(ctx context.Context) (*ent.AppSettings, error) {
	return GetClient().AppSettings.
		Query().
		Where(entappsettings.IDEQ(AppSettingsID)).
		Only(ctx)
}

// SaveACMEAccount 将 ACME 账号私钥和注册信息持久化到 DB
func SaveACMEAccount(ctx context.Context, keyPEM, regJSON string) error {
	s, err := GetAppSettings(ctx)
	if err != nil {
		return err
	}
	return s.Update().
		SetAcmeAccountKey(keyPEM).
		SetAcmeRegistration(regJSON).
		Exec(ctx)
}

// GetEnabledCloudAccounts 从 DB 读取所有启用的阿里云账号
func GetEnabledCloudAccounts(ctx context.Context) []*ent.CloudAccount {
	accounts, _ := GetClient().CloudAccount.
		Query().
		Where(entcloudaccount.EnabledEQ(true)).
		All(ctx)
	return accounts
}

// ParseScanInterval 解析 scan_interval 字段为 time.Duration
func ParseScanInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("scan_interval %q 格式无效: %w", s, err)
	}
	return d, nil
}

// 域名下线判定窗口的兜底值,配置非法时使用
const (
	DefaultMissingGrace = 72 * time.Hour
	DefaultArchiveAfter = 168 * time.Hour
)

// PresenceWindows 读取域名下线判定的两个时间窗口。
// 配置值非法时退回默认值,而不是让整轮对账跑不下去。
func PresenceWindows(ctx context.Context) (missingGrace, archiveAfter time.Duration) {
	missingGrace, archiveAfter = DefaultMissingGrace, DefaultArchiveAfter
	s, err := GetAppSettings(ctx)
	if err != nil {
		return
	}
	if d, err := time.ParseDuration(s.MissingGrace); err == nil {
		missingGrace = d
	}
	if d, err := time.ParseDuration(s.ArchiveAfter); err == nil {
		archiveAfter = d
	}
	return
}

// ParseRetention 解析归档记录保留期,非法值按"永久保留"处理。
// 宁可留着不该留的数据,也不要因为配置写错就把历史删了。
func ParseRetention(value string) time.Duration {
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// DNSResolvers 读取配置的递归 DNS 列表,读不到时返回 nil(调用方退回系统解析器)
func DNSResolvers(ctx context.Context) []string {
	s, err := GetAppSettings(ctx)
	if err != nil {
		return nil
	}
	return alidns.ParseResolvers(s.DNSResolvers)
}
