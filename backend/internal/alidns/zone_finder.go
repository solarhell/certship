// Package alidns 提供阿里云 DNS 相关辅助能力。
package alidns

import (
	"context"
	"fmt"
	"net"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	alidnssdk "github.com/go-acme/alidns-20150109/v4/client"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/solarhell/certship/pkg/ent"
)

// 阿里云 DNS 的权威 NS 域名后缀。
// 免费版:dns*.hichina.com;付费/企业版:ns*.alidns.com。
var aliyunNSSuffixes = []string{".hichina.com.", ".alidns.com."}

// FindAccountForDomain 在 accounts 里找到一个 AliDNS 真正托管 domain 所在 zone 的账号。
//
// 判定:
//  1. 查 zone 的权威 NS,必须指向阿里云(*.hichina.com / *.alidns.com)——
//     排除"域名注册在阿里云但 NS 改到 Cloudflare/DNSPod"。
//  2. 该 zone 在某个 enabled 账号的 AliDNS 下(DescribeDomainInfo 能查到)。
//
// 返回匹配的账号和 zone(不含末尾点);没有匹配返回 error。
//
// 用途:bucket 账号 ≠ DNS zone 账号时,用 DNS 账号的 AK 做 DNS-01 挑战,OSS/CDN 部署仍走 bucket 账号。
func FindAccountForDomain(ctx context.Context, logger *zap.Logger, domain string, accounts []*ent.CloudAccount) (*ent.CloudAccount, string, error) {
	authZone, err := dns01.FindZoneByFqdn(dns.Fqdn(domain))
	if err != nil {
		return nil, "", fmt.Errorf("查询 %s 的 zone 失败: %w", domain, err)
	}
	zone := dns01.UnFqdn(authZone)

	if ok, err := isAliyunNS(zone); err != nil {
		return nil, zone, fmt.Errorf("查询 zone %s 的 NS 失败: %w", zone, err)
	} else if !ok {
		return nil, zone, fmt.Errorf("zone %s 的 NS 未指向阿里云 DNS", zone)
	}

	for _, acc := range accounts {
		if !acc.Enabled {
			continue
		}
		client, err := newClient(acc.AccessKeyID, acc.AccessKeySecret)
		if err != nil {
			logger.Warn("创建 AliDNS client 失败", zap.String("account", acc.Name), zap.Error(err))
			continue
		}
		req := new(alidnssdk.DescribeDomainInfoRequest).SetDomainName(zone)
		if _, err := alidnssdk.DescribeDomainInfoWithContext(ctx, client, req, &dara.RuntimeOptions{}); err != nil {
			logger.Debug("账号不持有 zone",
				zap.String("account", acc.Name),
				zap.String("zone", zone),
				zap.Error(err),
			)
			continue
		}
		return acc, zone, nil
	}
	return nil, zone, fmt.Errorf("zone %s 的 NS 指向阿里云,但不在已添加的任何云账号下(请添加持有此 zone 的阿里云账号)", zone)
}

// isAliyunNS 查 zone 的 NS 记录,判断是否指向阿里云 DNS。
func isAliyunNS(zone string) (bool, error) {
	nss, err := net.LookupNS(zone)
	if err != nil {
		return false, err
	}
	for _, ns := range nss {
		host := strings.ToLower(ns.Host)
		if !strings.HasSuffix(host, ".") {
			host += "."
		}
		for _, suffix := range aliyunNSSuffixes {
			if strings.HasSuffix(host, suffix) {
				return true, nil
			}
		}
	}
	return false, nil
}

func newClient(accessKeyID, accessKeySecret string) (*alidnssdk.Client, error) {
	cfg := new(openapi.Config).
		SetRegionId("cn-hangzhou").
		SetAccessKeyId(accessKeyID).
		SetAccessKeySecret(accessKeySecret)
	return alidnssdk.NewClient(cfg)
}
