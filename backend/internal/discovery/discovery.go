// Package discovery 把 OSS 自定义域名与 CDN 加速域名统一成一份"云上现状"快照,
// 资源仅来自本轮观测，查询不完整时保留明确的完整性标记。
package discovery

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	cdnpkg "github.com/solarhell/certship/internal/cdn"
	"github.com/solarhell/certship/internal/oss"
	"github.com/solarhell/certship/pkg/ent"
)

type Origin string

const (
	OriginOSS  Origin = "oss"
	OriginCDN  Origin = "cdn"
	OriginBoth Origin = "both"
)

type DeploymentTarget string

const (
	DeployTargetOSS DeploymentTarget = "oss"
	DeployTargetCDN DeploymentTarget = "cdn"
)

// cdnCertConcurrency 查询 CDN 证书信息的并发度
const cdnCertConcurrency = 8

// CertInfo 云上已绑定证书的有效期
type CertInfo struct {
	ValidStartDate time.Time
	ValidEndDate   time.Time
}

// DomainInfo 一个被发现的自定义域名及其在云上的现状
type DomainInfo struct {
	Domain               string
	Bucket               string // 源站/绑定的 bucket,源站非 OSS 的纯 CDN 域名为空
	Region               string
	Account              *ent.CloudAccount
	Origin               Origin
	OSSAccount           *ent.CloudAccount // 实际 OSS 绑定账号，可能与 CDN 部署账号不同
	OSSBucket, OSSRegion string
	CertKnown            bool // 区分无证书与证书查询失败
	Ambiguous            bool

	// OSSCert / CDNCert 为 nil 表示对应侧未绑证书
	OSSCert *CertInfo
	CDNCert *CertInfo
}

// DeployTarget 返回该域名应当部署证书的目标。
// 只要域名出现在 CDN 加速域名列表里就走 CDN——此时 OSS 侧的 bucket 只是回源源站。
func (d DomainInfo) DeployTarget() DeploymentTarget {
	if d.Origin == OriginCDN || d.Origin == OriginBoth {
		return DeployTargetCDN
	}
	return DeployTargetOSS
}

func (d DomainInfo) CloudCert() *CertInfo {
	if d.DeployTarget() == DeployTargetCDN {
		return d.CDNCert
	}
	return d.OSSCert
}

// Coverage tracks completeness, including successful empty inventories. Query
// failure is unknown, never evidence that a resource or certificate is absent.
type Coverage struct {
	ossAccounts map[string]struct{}
	cdnAccounts map[string]struct{}
	accounts    map[string]struct{}
}

func newCoverage() Coverage {
	return Coverage{ossAccounts: make(map[string]struct{}), cdnAccounts: make(map[string]struct{}), accounts: make(map[string]struct{})}
}
func (c Coverage) addCDNAccount(account string) { c.cdnAccounts[account] = struct{}{} }
func (c Coverage) CDNComplete() bool {
	for account := range c.accounts {
		if _, ok := c.cdnAccounts[account]; !ok {
			return false
		}
	}
	return true
}
func (c Coverage) IsEmpty() bool { return len(c.ossAccounts) == 0 && len(c.cdnAccounts) == 0 }

// Result 一轮发现的完整结果
type Result struct {
	Domains    []DomainInfo
	Coverage   Coverage
	ObservedAt time.Time
}

func (r Result) Lookup(domain string) (DomainInfo, bool) {
	for _, info := range r.Domains {
		if info.Domain == domain {
			return info, true
		}
	}
	return DomainInfo{}, false
}

func (r Result) Deployable(info DomainInfo) bool {
	return !info.Ambiguous && (info.DeployTarget() == DeployTargetCDN || r.Coverage.CDNComplete())
}

// Complete includes all pages, all enabled accounts, all bucket CNAMEs and
// certificate observations. An empty account or empty bucket list is complete.
func (r Result) Complete() bool {
	if !r.Coverage.CDNComplete() {
		return false
	}
	for account := range r.Coverage.accounts {
		if _, ok := r.Coverage.ossAccounts[account]; !ok {
			return false
		}
	}
	for _, info := range r.Domains {
		if !r.Deployable(info) || !info.CertKnown {
			return false
		}
	}
	return true
}

// Run 扫描所有账号的 OSS 与 CDN,合并成统一快照。
// 单个账号、单侧失败只是缩小 Coverage,不会中断整轮。
type sources struct {
	scanOSS      func(context.Context, *ent.CloudAccount) (oss.ScanResult, error)
	listCDN      func(string, string) ([]cdnpkg.DomainInfo, error)
	describeCert func(string, string, string) (*cdnpkg.CertInfo, error)
}

func Run(ctx context.Context, logger *zap.Logger, accounts []*ent.CloudAccount) Result {
	scanner := oss.NewScanner(logger)
	manager := cdnpkg.NewManager(logger)
	return run(ctx, logger, accounts, sources{scanner.ScanAccount, manager.ListDomains, manager.DescribeCert})
}

func run(ctx context.Context, logger *zap.Logger, accounts []*ent.CloudAccount, cloud sources) Result {
	result := Result{Coverage: newCoverage()}
	byDomain := make(map[string]*DomainInfo)
	var order []string
	for _, account := range accounts {
		result.Coverage.accounts[account.Name] = struct{}{}
	}
	for _, account := range accounts {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		// ---- OSS 侧 ----
		scan, err := cloud.scanOSS(ctx, account)
		if err != nil {
			logger.Error("扫描账号 OSS 失败,本轮 OSS 观测不完整",
				zap.String("account", account.Name),
				zap.Error(err),
			)
		} else {
			if len(scan.Buckets) == len(scan.CoveredBuckets) {
				result.Coverage.ossAccounts[account.Name] = struct{}{}
			}
			for _, d := range scan.Domains {
				info := &DomainInfo{
					Domain:     d.Domain,
					Bucket:     d.Bucket,
					Region:     d.Region,
					Account:    account,
					Origin:     OriginOSS,
					OSSAccount: account,
					OSSBucket:  d.Bucket, OSSRegion: d.Region,
					CertKnown: d.CertKnown,
				}
				if d.OSSCert != nil {
					info.OSSCert = &CertInfo{ValidStartDate: d.OSSCert.ValidStartDate, ValidEndDate: d.OSSCert.ValidEndDate}
				}
				if _, dup := byDomain[d.Domain]; dup {
					existing := byDomain[d.Domain]
					if existing.DeployTarget() == DeployTargetCDN {
						existing.Origin = OriginBoth
						existing.OSSAccount = account
						existing.OSSBucket, existing.OSSRegion = d.Bucket, d.Region
						existing.OSSCert = info.OSSCert

						continue
					}
					existing.Ambiguous = true
					logger.Warn("同一域名在多处绑定,保留先发现的一条", zap.String("domain", d.Domain))
					continue
				}
				byDomain[d.Domain] = info
				order = append(order, d.Domain)
			}
			logger.Info("账号 OSS 扫描完成",
				zap.String("account", account.Name),
				zap.Int("domain_count", len(scan.Domains)),
				zap.Int("covered_buckets", len(scan.CoveredBuckets)),
			)
		}

		// ---- CDN 侧 ----
		cdnDomains, err := cloud.listCDN(account.AccessKeyID, account.AccessKeySecret)
		if err != nil {
			logger.Error("扫描账号 CDN 失败,本轮 CDN 观测不完整",
				zap.String("account", account.Name),
				zap.Error(err),
			)
			continue
		}
		result.Coverage.addCDNAccount(account.Name)

		certs := fetchCDNCerts(ctx, cloud.describeCert, account, cdnDomains)
		for _, d := range cdnDomains {
			cert := certs[d.Domain]
			if existing, ok := byDomain[d.Domain]; ok {
				if existing.DeployTarget() == DeployTargetCDN {
					existing.Ambiguous = true
					continue
				}
				// 两侧都有:身份不变,但部署目标是 CDN,OSS 侧的 bucket 降格为源站
				existing.Origin = OriginBoth
				existing.Account = account
				existing.Ambiguous = false
				existing.CDNCert = cert.cert
				existing.CertKnown = cert.known
				existing.Bucket, existing.Region = d.SourceBucket, d.SourceRegion
				continue
			}
			info := &DomainInfo{
				Domain:    d.Domain,
				Bucket:    d.SourceBucket,
				Region:    d.SourceRegion,
				Account:   account,
				Origin:    OriginCDN,
				CDNCert:   cert.cert,
				CertKnown: cert.known,
			}
			byDomain[d.Domain] = info
			order = append(order, d.Domain)
		}
		logger.Info("账号 CDN 扫描完成",
			zap.String("account", account.Name),
			zap.Int("domain_count", len(cdnDomains)),
		)
	}

	result.Domains = make([]DomainInfo, 0, len(order))
	for _, name := range order {
		result.Domains = append(result.Domains, *byDomain[name])
	}
	result.ObservedAt = time.Now()
	return result
}

type certObservation struct {
	cert  *CertInfo
	known bool
}

// fetchCDNCerts 并发查询 CDN 域名上已配置证书的有效期
func fetchCDNCerts(ctx context.Context, describe func(string, string, string) (*cdnpkg.CertInfo, error), account *ent.CloudAccount, domains []cdnpkg.DomainInfo) map[string]certObservation {
	certs := make(map[string]certObservation, len(domains))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, cdnCertConcurrency)

	for _, d := range domains {
		if !d.SSLOn {
			mu.Lock()
			certs[d.Domain] = certObservation{known: true}
			mu.Unlock()
			continue
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return certs
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(domain string) {
			defer wg.Done()
			defer func() { <-sem }()

			cert, err := describe(account.AccessKeyID, account.AccessKeySecret, domain)
			mu.Lock()
			defer mu.Unlock()
			observation := certObservation{known: err == nil}
			if cert != nil {
				observation.cert = &CertInfo{ValidStartDate: cert.ValidStartDate, ValidEndDate: cert.ValidEndDate}
			}
			certs[domain] = observation
		}(d.Domain)
	}
	wg.Wait()
	return certs
}
