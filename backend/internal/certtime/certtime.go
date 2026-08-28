// Package certtime 解析云厂商返回的证书有效期。
//
// 单独抽出来是因为各家格式不一致:OSS 的 ListCname 返回 OpenSSL 风格的
// "Nov 26 05:05:23 2026 GMT",CDN 返回 ISO8601。carbon 的自动推断认不出前者,
// 会把有效证书当成"没有证书",导致每轮重复绑定、甚至给云上已有有效证书的域名
// 重新签发。所以这里显式列出 layout,推断只作兜底。
package certtime

import (
	"time"

	carbon "github.com/dromara/carbon/v2"
)

// layouts 按优先级排列,第一个匹配即返回
var layouts = []string{
	// OSS ListCname:OpenSSL 的 ASN1_TIME_print 格式,日为个位数时补空格
	"Jan _2 15:04:05 2006 MST",
	time.RFC3339,
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05",
}

// Parse 解析证书有效期。无法解析或结果为零值时返回 ok=false。
func Parse(s string) (t time.Time, ok bool) {
	if s == "" {
		return time.Time{}, false
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, s); err == nil && !parsed.IsZero() {
			return parsed, true
		}
	}

	// 兜底:交给 carbon 推断,能认出上面没列到的格式
	c := carbon.Parse(s)
	if c.IsInvalid() || c.IsZero() {
		return time.Time{}, false
	}
	return c.StdTime(), true
}
