package cdn

import (
	"testing"

	cdn "github.com/alibabacloud-go/cdn-20180510/v9/client"
	"go.uber.org/zap"
)

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(logger)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestCheckOnline_InvalidCredentials(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(logger)
	// 凭证无效必须报错,而不是被当成"域名不存在"——那会让预检误判成域名已下线
	online, err := m.CheckOnline("invalid", "invalid", "example.com")
	if online {
		t.Error("expected not online for invalid credentials")
	}
	if err == nil {
		t.Error("expected an error for invalid credentials, got nil")
	}
}

func TestParseOSSSource(t *testing.T) {
	cases := []struct {
		content    string
		wantBucket string
		wantRegion string
	}{
		{"my-bucket.oss-cn-hangzhou.aliyuncs.com", "my-bucket", "cn-hangzhou"},
		{"my-bucket.oss-cn-hangzhou-internal.aliyuncs.com", "my-bucket", "cn-hangzhou"},
		{"my-bucket.oss-cn-beijing.aliyuncs.com:80", "my-bucket", "cn-beijing"},
		{"origin.example.com", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		content := tc.content
		sources := []*cdn.DescribeUserDomainsResponseBodyDomainsPageDataSourcesSource{{Content: &content}}
		bucket, region := parseOSSSource(sources)
		if bucket != tc.wantBucket || region != tc.wantRegion {
			t.Errorf("parseOSSSource(%q) = (%q, %q), want (%q, %q)", tc.content, bucket, region, tc.wantBucket, tc.wantRegion)
		}
	}
}
