package discovery

import (
	"testing"

	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

// 对账的全部安全性都压在 CanObserve 上:它一旦把"没扫到"和"扫不到"混为一谈,
// 一次 API 抖动就会把整批还活着的域名判定成下线。
func TestCoverageCanObserve(t *testing.T) {
	c := newCoverage()
	c.addOSSBuckets("主账号", []string{"bucket-a", "bucket-b"})
	c.addCDNAccount("主账号")
	c.addOSSBuckets("备用账号", []string{"bucket-c"}) // 这个账号的 CDN 没扫通

	cases := []struct {
		name    string
		origin  entdomain.Origin
		account string
		bucket  string
		want    bool
	}{
		{"OSS 域名在已扫通的 bucket 下", entdomain.OriginOss, "主账号", "bucket-a", true},
		{"OSS 域名所在 bucket 本轮没扫通", entdomain.OriginOss, "主账号", "bucket-x", false},
		{"OSS 域名所属账号完全没扫", entdomain.OriginOss, "陌生账号", "bucket-a", false},
		{"OSS 域名没有 bucket 信息时不下结论", entdomain.OriginOss, "主账号", "", false},
		{"CDN 域名所属账号已扫通", entdomain.OriginCdn, "主账号", "", true},
		{"CDN 域名所属账号的 CDN 没扫通", entdomain.OriginCdn, "备用账号", "bucket-c", false},
		{"两侧都有的域名需要两侧都看得见", entdomain.OriginBoth, "主账号", "bucket-b", true},
		{"两侧都有但只扫通了 OSS 时不下结论", entdomain.OriginBoth, "备用账号", "bucket-c", false},
		{"两侧都有但 bucket 没扫通时不下结论", entdomain.OriginBoth, "主账号", "bucket-x", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.CanObserve(tc.origin, tc.account, tc.bucket); got != tc.want {
				t.Errorf("CanObserve(%v, %q, %q) = %v, want %v", tc.origin, tc.account, tc.bucket, got, tc.want)
			}
		})
	}
}

func TestCoverageIsEmpty(t *testing.T) {
	c := newCoverage()
	if !c.IsEmpty() {
		t.Error("空 coverage 应当报告为空")
	}
	c.addCDNAccount("主账号")
	if c.IsEmpty() {
		t.Error("扫通了 CDN 就不算空")
	}
}

func TestDeployTarget(t *testing.T) {
	cases := []struct {
		origin entdomain.Origin
		want   entdomain.DeployTarget
	}{
		{entdomain.OriginOss, entdomain.DeployTargetOss},
		{entdomain.OriginCdn, entdomain.DeployTargetCdn},
		// 两侧都有时 bucket 只是回源源站,证书该上 CDN
		{entdomain.OriginBoth, entdomain.DeployTargetCdn},
	}
	for _, tc := range cases {
		got := DomainInfo{Origin: tc.origin}.DeployTarget()
		if got != tc.want {
			t.Errorf("origin=%v DeployTarget() = %v, want %v", tc.origin, got, tc.want)
		}
	}
}
