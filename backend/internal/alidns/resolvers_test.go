package alidns

import (
	"testing"
)

func TestParseResolvers(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"补默认端口", "223.5.5.5", []string{"223.5.5.5:53"}},
		{"保留显式端口", "223.5.5.5:5353", []string{"223.5.5.5:5353"}},
		{"多个并去掉空白", " 223.5.5.5 , 119.29.29.29:53 ", []string{"223.5.5.5:53", "119.29.29.29:53"}},
		{"忽略空项", "223.5.5.5,,", []string{"223.5.5.5:53"}},
		{"IPv6 补端口", "2400:3200::1", []string{"[2400:3200::1]:53"}},
		{"全空返回 nil", "  ,  ", nil},
		{"空串返回 nil", "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseResolvers(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseResolvers(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ParseResolvers(%q)[%d] = %q, want %q", tc.raw, i, got[i], tc.want[i])
				}
			}
		})
	}
}
