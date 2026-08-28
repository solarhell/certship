// Package alidns 提供阿里云 DNS 相关辅助能力。
package alidns

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// dnsQueryTimeout 单次 DNS 查询的超时
const dnsQueryTimeout = 10 * time.Second

// FindAccountForDomain 在 accounts 里找到一个 AliDNS 真正托管 domain 所在 zone 的账号。
//
// 判定:
//  1. 查 zone 的权威 NS,必须指向阿里云(*.hichina.com / *.alidns.com)——
//     排除"域名注册在阿里云但 NS 改到 Cloudflare/DNSPod"。
//  2. 该 zone 在某个 enabled 账号的 AliDNS 下(DescribeDomainInfo 能查到)。
//
// resolvers 是显式指定的递归 DNS(host:port)。不走系统解析器是有意的:
// 服务器上的 /etc/resolv.conf 可能指向内网 DNS 或被劫持,拿到的 SOA/NS 与公网不一致,
// 会让 zone 判定张冠李戴——而这个判定的结果直接决定用哪个账号做 DNS-01 挑战。
//
// 返回匹配的账号和 zone(不含末尾点);没有匹配返回 error。
func FindAccountForDomain(
	ctx context.Context,
	logger *zap.Logger,
	domain string,
	accounts []*ent.CloudAccount,
	resolvers []string,
) (*ent.CloudAccount, string, error) {
	zone, aliyunNS, err := InspectZone(domain, resolvers)
	if err != nil {
		if zone == "" {
			return nil, "", fmt.Errorf("查询 %s 的 zone 失败: %w", domain, err)
		}
		return nil, zone, fmt.Errorf("查询 zone %s 的 NS 失败: %w", zone, err)
	}
	if !aliyunNS {
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

// InspectZone 找出 domain 所属的权威 zone,并判断该 zone 的 NS 是否指向阿里云。
//
// 单独导出是因为"这个域名被判到了哪个 zone"是排查签发失败时最先要问的问题,
// 而它只依赖 DNS,不需要任何云账号。
func InspectZone(domain string, resolvers []string) (zone string, aliyunNS bool, err error) {
	fqdn := dns.Fqdn(domain)

	var authZone string
	if len(resolvers) == 0 {
		authZone, err = dns01.FindZoneByFqdn(fqdn)
	} else {
		authZone, err = dns01.FindZoneByFqdnCustom(fqdn, resolvers)
	}
	if err != nil {
		return "", false, err
	}

	zone = dns01.UnFqdn(authZone)
	aliyunNS, err = isAliyunNS(zone, resolvers)
	return zone, aliyunNS, err
}

// isAliyunNS 查 zone 的 NS 记录,判断是否指向阿里云 DNS。
//
// 用指定的 resolver 直接发查询,而不是 net.LookupNS——后者走系统解析器,
// 会和 zone 探测用的解析器不一致,两处结论对不上时排查起来非常费劲。
func isAliyunNS(zone string, resolvers []string) (bool, error) {
	nss, err := lookupNS(zone, resolvers)
	if err != nil {
		return false, err
	}
	if len(nss) == 0 {
		return false, fmt.Errorf("zone %s 没有查到 NS 记录", zone)
	}
	for _, host := range nss {
		host = strings.ToLower(host)
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

// lookupNS 依次向 resolvers 查询 zone 的 NS 记录,返回第一个成功的结果
func lookupNS(zone string, resolvers []string) ([]string, error) {
	if len(resolvers) == 0 {
		return systemLookupNS(zone)
	}

	msg := new(dns.Msg).SetQuestion(dns.Fqdn(zone), dns.TypeNS)
	client := &dns.Client{Timeout: dnsQueryTimeout}

	var lastErr error
	for _, server := range resolvers {
		resp, _, err := client.Exchange(msg, server)
		if err != nil {
			lastErr = fmt.Errorf("向 %s 查询 %s 的 NS 失败: %w", server, zone, err)
			continue
		}
		if resp.Rcode != dns.RcodeSuccess {
			lastErr = fmt.Errorf("向 %s 查询 %s 的 NS 返回 %s", server, zone, dns.RcodeToString[resp.Rcode])
			continue
		}
		var hosts []string
		for _, rr := range resp.Answer {
			if ns, ok := rr.(*dns.NS); ok {
				hosts = append(hosts, ns.Ns)
			}
		}
		return hosts, nil
	}
	return nil, lastErr
}

func newClient(accessKeyID, accessKeySecret string) (*alidnssdk.Client, error) {
	cfg := new(openapi.Config).
		SetRegionId("cn-hangzhou").
		SetAccessKeyId(accessKeyID).
		SetAccessKeySecret(accessKeySecret)
	return alidnssdk.NewClient(cfg)
}
