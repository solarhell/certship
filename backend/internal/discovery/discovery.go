// Package discovery 把 OSS 自定义域名与 CDN 加速域名统一成一份"云上现状"快照,
// 并如实记录本轮扫描实际覆盖到的范围,供上层做安全的下线对账。
package discovery

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	cdnpkg "github.com/solarhell/certship/internal/cdn"
	"github.com/solarhell/certship/internal/oss"
	"github.com/solarhell/certship/pkg/ent"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
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
	Domain  string
	Bucket  string // 源站/绑定的 bucket,源站非 OSS 的纯 CDN 域名为空
	Region  string
	Account *ent.CloudAccount
	Origin  entdomain.Origin

	// OSSCert / CDNCert 为 nil 表示对应侧未绑证书
	OSSCert *CertInfo
	CDNCert *CertInfo
}

// DeployTarget 返回该域名应当部署证书的目标。
// 只要域名出现在 CDN 加速域名列表里就走 CDN——此时 OSS 侧的 bucket 只是回源源站。
func (d DomainInfo) DeployTarget() entdomain.DeployTarget {
	if d.Origin == entdomain.OriginCdn || d.Origin == entdomain.OriginBoth {
		return entdomain.DeployTargetCdn
	}
	return entdomain.DeployTargetOss
}

// Coverage 描述本轮扫描实际成功覆盖到的范围。
//
// 存在的意义:只有"本轮确实看得见、却没看到"的域名才可能是下线了。
// 扫描失败(限流、网络抖动、AK 权限变更)必须表现为"看不见",而不是"不存在"。
type Coverage struct {
	ossBuckets  map[string]map[string]struct{} // account name -> bucket set
	cdnAccounts map[string]struct{}            // 成功拉全 CDN 域名列表的账号
}

func newCoverage() Coverage {
	return Coverage{
		ossBuckets:  make(map[string]map[string]struct{}),
		cdnAccounts: make(map[string]struct{}),
	}
}

func (c Coverage) addOSSBuckets(account string, buckets []string) {
	set, ok := c.ossBuckets[account]
	if !ok {
		set = make(map[string]struct{}, len(buckets))
		c.ossBuckets[account] = set
	}
	for _, b := range buckets {
		set[b] = struct{}{}
	}
}

func (c Coverage) addCDNAccount(account string) {
	c.cdnAccounts[account] = struct{}{}
}

func (c Coverage) coversOSS(account, bucket string) bool {
	if bucket == "" {
		return false
	}
	_, ok := c.ossBuckets[account][bucket]
	return ok
}

func (c Coverage) coversCDN(account string) bool {
	_, ok := c.cdnAccounts[account]
	return ok
}

// CanObserve 判断本轮扫描是否有能力观察到这条记录。
// 返回 false 时调用方必须放过这条记录,不得据此判定下线。
func (c Coverage) CanObserve(origin entdomain.Origin, account, bucket string) bool {
	switch origin {
	case entdomain.OriginOss:
		return c.coversOSS(account, bucket)
	case entdomain.OriginCdn:
		return c.coversCDN(account)
	case entdomain.OriginBoth:
		// 两侧都要看得见才敢下结论:只有一侧可见时,域名可能只是从这一侧摘掉了
		return c.coversOSS(account, bucket) && c.coversCDN(account)
	default:
		return false
	}
}

// IsEmpty 报告本轮是否什么都没扫到。整轮全瞎时不应做任何下线对账。
func (c Coverage) IsEmpty() bool {
	return len(c.ossBuckets) == 0 && len(c.cdnAccounts) == 0
}

// Result 一轮发现的完整结果
type Result struct {
	Domains  []DomainInfo
	Coverage Coverage
}

// Run 扫描所有账号的 OSS 与 CDN,合并成统一快照。
// 单个账号、单侧失败只是缩小 Coverage,不会中断整轮。
func Run(ctx context.Context, logger *zap.Logger, accounts []*ent.CloudAccount) Result {
	result := Result{Coverage: newCoverage()}
	byDomain := make(map[string]*DomainInfo)
	var order []string

	ossScanner := oss.NewScanner(logger)
	cdnMgr := cdnpkg.NewManager(logger)

	for _, account := range accounts {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		// ---- OSS 侧 ----
		scan, err := ossScanner.ScanAccount(ctx, account)
		if err != nil {
			logger.Error("扫描账号 OSS 失败,本轮不对该账号的 OSS 域名做下线判定",
				zap.String("account", account.Name),
				zap.Error(err),
			)
		} else {
			result.Coverage.addOSSBuckets(account.Name, scan.CoveredBuckets)
			for _, d := range scan.Domains {
				info := &DomainInfo{
					Domain:  d.Domain,
					Bucket:  d.Bucket,
					Region:  d.Region,
					Account: account,
					Origin:  entdomain.OriginOss,
				}
				if d.OSSCert != nil {
					info.OSSCert = &CertInfo{ValidStartDate: d.OSSCert.ValidStartDate, ValidEndDate: d.OSSCert.ValidEndDate}
				}
				if _, dup := byDomain[d.Domain]; dup {
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
		cdnDomains, err := cdnMgr.ListDomains(account.AccessKeyID, account.AccessKeySecret)
		if err != nil {
			logger.Error("扫描账号 CDN 失败,本轮不对该账号的 CDN 域名做下线判定",
				zap.String("account", account.Name),
				zap.Error(err),
			)
			continue
		}
		result.Coverage.addCDNAccount(account.Name)

		certs := fetchCDNCerts(ctx, cdnMgr, account, cdnDomains)
		for _, d := range cdnDomains {
			cert := certs[d.Domain]
			if existing, ok := byDomain[d.Domain]; ok {
				// 两侧都有:身份不变,但部署目标是 CDN,OSS 侧的 bucket 降格为源站
				existing.Origin = entdomain.OriginBoth
				existing.CDNCert = cert
				continue
			}
			info := &DomainInfo{
				Domain:  d.Domain,
				Bucket:  d.SourceBucket,
				Region:  d.SourceRegion,
				Account: account,
				Origin:  entdomain.OriginCdn,
				CDNCert: cert,
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
	return result
}

// fetchCDNCerts 并发查询 CDN 域名上已配置证书的有效期
func fetchCDNCerts(ctx context.Context, mgr *cdnpkg.Manager, account *ent.CloudAccount, domains []cdnpkg.DomainInfo) map[string]*CertInfo {
	certs := make(map[string]*CertInfo, len(domains))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, cdnCertConcurrency)

	for _, d := range domains {
		if !d.SSLOn {
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

			cert := mgr.DescribeCert(account.AccessKeyID, account.AccessKeySecret, domain)
			if cert == nil {
				return
			}
			mu.Lock()
			certs[domain] = &CertInfo{ValidStartDate: cert.ValidStartDate, ValidEndDate: cert.ValidEndDate}
			mu.Unlock()
		}(d.Domain)
	}
	wg.Wait()
	return certs
}
