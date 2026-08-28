package cdn

import (
	"fmt"
	"strings"
	"time"

	cdn "github.com/alibabacloud-go/cdn-20180510/v9/client"
	carbon "github.com/dromara/carbon/v2"
	"go.uber.org/zap"
)

// DomainInfo 描述一个阿里云 CDN 加速域名
type DomainInfo struct {
	Domain string
	// SourceBucket/SourceRegion 从 OSS 类型源站解析而来,源站非 OSS 时为空
	SourceBucket string
	SourceRegion string
	SSLOn        bool
	// Cert 为 nil 表示 CDN 侧未配置证书或读取失败
	Cert *CertInfo
}

// CertInfo CDN 域名上已配置证书的有效期
type CertInfo struct {
	ValidStartDate time.Time
	ValidEndDate   time.Time
}

// ListDomains 拉取账号下全部 CDN 加速域名。
//
// 只收 online 状态的域名——offline/configuring 的域名不承载流量,
// 语义上等价于 OSS 侧非 Enabled 的 cname。
// 任何一页失败都返回 error:残缺的域名列表不能用来判定下线。
func (m *Manager) ListDomains(accessKeyID, accessKeySecret string) ([]DomainInfo, error) {
	client, err := newClient(accessKeyID, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("创建 CDN 客户端失败: %w", err)
	}

	const pageSize = 50
	var domains []DomainInfo

	for pageNumber := int32(1); ; pageNumber++ {
		req := &cdn.DescribeUserDomainsRequest{}
		req.SetPageSize(pageSize)
		req.SetPageNumber(pageNumber)

		resp, err := client.DescribeUserDomains(req)
		if err != nil {
			return nil, fmt.Errorf("查询 CDN 域名列表第 %d 页失败: %w", pageNumber, err)
		}
		if resp.Body == nil || resp.Body.Domains == nil {
			break
		}

		page := resp.Body.Domains.PageData
		for _, d := range page {
			name := deref(d.DomainName)
			if name == "" || deref(d.DomainStatus) != "online" {
				continue
			}
			info := DomainInfo{
				Domain: name,
				SSLOn:  deref(d.SslProtocol) == "on",
			}
			if d.Sources != nil {
				info.SourceBucket, info.SourceRegion = parseOSSSource(d.Sources.Source)
			}
			domains = append(domains, info)
		}

		if len(page) < pageSize {
			break
		}
	}
	return domains, nil
}

// DescribeCert 查询 CDN 域名上已配置证书的有效期,查不到返回 nil
func (m *Manager) DescribeCert(accessKeyID, accessKeySecret, domain string) *CertInfo {
	client, err := newClient(accessKeyID, accessKeySecret)
	if err != nil {
		return nil
	}

	req := &cdn.DescribeDomainCertificateInfoRequest{}
	req.SetDomainName(domain)

	resp, err := client.DescribeDomainCertificateInfo(req)
	if err != nil {
		m.logger.Debug("查询 CDN 证书信息失败", zap.String("domain", domain), zap.Error(err))
		return nil
	}
	if resp.Body == nil || resp.Body.CertInfos == nil || len(resp.Body.CertInfos.CertInfo) == 0 {
		return nil
	}

	info := resp.Body.CertInfos.CertInfo[0]
	if info.ServerCertificateStatus != nil && *info.ServerCertificateStatus != "on" {
		return nil
	}
	end := carbon.Parse(deref(info.CertExpireTime))
	if end.IsInvalid() || end.IsZero() {
		return nil
	}
	start := carbon.Parse(deref(info.CertStartTime))
	return &CertInfo{
		ValidStartDate: start.StdTime(),
		ValidEndDate:   end.StdTime(),
	}
}

// parseOSSSource 从源站列表里找出 OSS 类型的源站,解析出 bucket 与 region。
// 形如 my-bucket.oss-cn-hangzhou.aliyuncs.com / my-bucket.oss-cn-hangzhou-internal.aliyuncs.com
func parseOSSSource(sources []*cdn.DescribeUserDomainsResponseBodyDomainsPageDataSourcesSource) (bucket, region string) {
	for _, src := range sources {
		content := deref(src.Content)
		if content == "" {
			continue
		}
		host, _, _ := strings.Cut(content, ":")
		name, rest, ok := strings.Cut(host, ".oss-")
		if !ok || name == "" {
			continue
		}
		region, _, ok = strings.Cut(rest, ".")
		if !ok || region == "" {
			continue
		}
		region = strings.TrimSuffix(region, "-internal")
		return name, region
	}
	return "", ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
