package alidns

import (
	"net"
	"strings"
)

// ParseResolvers 把配置里逗号分隔的解析器列表规范化成 host:port。
// 没写端口的补 53;整体为空则返回 nil,调用方据此退回系统解析器。
func ParseResolvers(raw string) []string {
	var out []string
	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(item); err != nil {
			item = net.JoinHostPort(item, "53")
		}
		out = append(out, item)
	}
	return out
}

// systemLookupNS 用系统解析器查 NS,仅在没有配置 resolvers 时使用
func systemLookupNS(zone string) ([]string, error) {
	nss, err := net.LookupNS(zone)
	if err != nil {
		return nil, err
	}
	hosts := make([]string, 0, len(nss))
	for _, ns := range nss {
		hosts = append(hosts, ns.Host)
	}
	return hosts, nil
}
