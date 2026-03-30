package database

import (
	"context"
	"fmt"
	"time"

	entappsettings "github.com/solarhell/certship/pkg/ent/appsettings"
	entcloudaccount "github.com/solarhell/certship/pkg/ent/cloudaccount"

	"github.com/solarhell/certship/pkg/ent"
)

const AppSettingsID = "default"

// GetAppSettings 从 DB 读取全局配置
func GetAppSettings(ctx context.Context) (*ent.AppSettings, error) {
	return GetClient().AppSettings.
		Query().
		Where(entappsettings.IDEQ(AppSettingsID)).
		Only(ctx)
}

// GetACMEAccount 从 DB 读取 ACME 账号私钥和注册信息（首次为空字符串）
func GetACMEAccount(ctx context.Context) (keyPEM, regJSON string, err error) {
	s, err := GetAppSettings(ctx)
	if err != nil {
		return "", "", err
	}
	return s.AcmeAccountKey, s.AcmeRegistration, nil
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
