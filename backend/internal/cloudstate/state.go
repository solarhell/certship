// Package cloudstate keeps short-lived cloud observations in memory. The database
// stores management intent and certificate material, never the current inventory.
package cloudstate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	"github.com/solarhell/certship/pkg/ent/cloudaccount"
	"go.uber.org/zap"
	"time"
)

const freshness = 30 * time.Second

type cache struct {
	gate       chan struct{}
	key        [32]byte
	value      discovery.Result
	validUntil time.Time
}

func newCache() *cache { return &cache{gate: make(chan struct{}, 1)} }

var observations = newCache()

func (c *cache) refresh(ctx context.Context, key [32]byte, force bool, load func() (discovery.Result, error)) (discovery.Result, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return discovery.Result{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return discovery.Result{}, err
	}
	if !force && key == c.key && time.Now().Before(c.validUntil) {
		return c.value, nil
	}
	value, err := load()
	if err != nil {
		return discovery.Result{}, err
	}
	c.key, c.value, c.validUntil = key, value, time.Now().Add(freshness)
	return value, nil
}

// Refresh forces a new observation for user refreshes and scheduled scans.
// Concurrent task execution can reuse that observation for at most 30 seconds;
// writes additionally verify the target immediately before deployment.
func Refresh(ctx context.Context, logger *zap.Logger, force bool) (discovery.Result, error) {
	accounts, err := database.GetClient().CloudAccount.Query().Where(cloudaccount.EnabledEQ(true)).Order(ent.Asc(cloudaccount.FieldID)).All(ctx)
	if err != nil {
		return discovery.Result{}, err
	}
	return RefreshAccounts(ctx, logger, accounts, force)
}
func RefreshAccounts(ctx context.Context, logger *zap.Logger, accounts []*ent.CloudAccount, force bool) (discovery.Result, error) {
	encoded, err := json.Marshal(accounts)
	if err != nil {
		return discovery.Result{}, fmt.Errorf("identify cloud accounts: %w", err)
	}
	key := sha256.Sum256(encoded)
	return observations.refresh(ctx, key, force, func() (discovery.Result, error) {
		result := discovery.Run(ctx, logger, accounts)
		if err := ctx.Err(); err != nil {
			return discovery.Result{}, err
		}
		return result, nil
	})
}
func Resolve(ctx context.Context, logger *zap.Logger, domain string, force bool) (discovery.DomainInfo, error) {
	result, err := Refresh(ctx, logger, force)
	if err != nil {
		return discovery.DomainInfo{}, err
	}
	info, ok := result.Lookup(domain)
	if !ok {
		return discovery.DomainInfo{}, fmt.Errorf("云上未确认域名 %s 的有效绑定，请检查云资源或扫描错误", domain)
	}
	if !result.Deployable(info) {
		return discovery.DomainInfo{}, fmt.Errorf("域名 %s 的部署目标尚未确认，暂不签发或部署", domain)
	}
	if !info.CertKnown {
		return discovery.DomainInfo{}, fmt.Errorf("域名 %s 的线上证书查询失败，暂不签发或部署", domain)
	}
	return info, nil
}
