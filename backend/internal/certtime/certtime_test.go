package certtime

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // RFC3339，空表示应当解析失败
	}{
		{
			// 线上真实取到的格式：carbon 的自动推断认不出它，
			// 曾导致有效证书被当成不存在而每轮重绑
			"OSS 的 OpenSSL 风格",
			"Nov 26 05:05:23 2026 GMT",
			"2026-11-26T05:05:23Z",
		},
		{"OSS 个位数日期补空格", "Aug  5 05:05:24 2026 GMT", "2026-08-05T05:05:24Z"},
		{"OSS 两位数日期", "Aug 28 05:05:24 2026 GMT", "2026-08-28T05:05:24Z"},
		{"RFC3339", "2026-11-26T05:05:23Z", "2026-11-26T05:05:23Z"},
		{"带时区偏移", "2026-11-26T13:05:23+08:00", "2026-11-26T05:05:23Z"},
		{"空串", "", ""},
		{"无法解析", "not a time", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Parse(tc.in)
			if tc.want == "" {
				if ok {
					t.Errorf("Parse(%q) = %v, 期望解析失败", tc.in, got)
				}
				return
			}
			if !ok {
				t.Fatalf("Parse(%q) 解析失败，期望 %s", tc.in, tc.want)
			}
			if got.UTC().Format(time.RFC3339) != tc.want {
				t.Errorf("Parse(%q) = %s, want %s", tc.in, got.UTC().Format(time.RFC3339), tc.want)
			}
		})
	}
}
