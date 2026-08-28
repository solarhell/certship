package domainsync

import (
	"testing"
	"time"

	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

func TestNextPresence(t *testing.T) {
	const (
		grace   = 72 * time.Hour
		archive = 168 * time.Hour
	)

	cases := []struct {
		name        string
		current     entdomain.Presence
		absent      time.Duration
		wantNext    entdomain.Presence
		wantChanged bool
	}{
		{
			// 运维迁 bucket 常见"先解绑再绑",中间这一下不该把域名判死
			"宽限期内不动",
			entdomain.PresencePresent, 10 * time.Hour,
			entdomain.PresencePresent, false,
		},
		{"刚过宽限期标记 missing", entdomain.PresencePresent, grace + time.Minute, entdomain.PresenceMissing, true},
		{"已经是 missing 不重复标记", entdomain.PresenceMissing, grace + time.Hour, entdomain.PresenceMissing, false},
		{"missing 足够久后归档", entdomain.PresenceMissing, grace + archive + time.Minute, entdomain.PresenceArchived, true},
		{"已归档不再变化", entdomain.PresenceArchived, grace + archive + 1000*time.Hour, entdomain.PresenceArchived, false},
		{
			// 进程停了很久再启动时,直接从 present 跳到 archived 是对的
			"长期缺席可以一步到归档",
			entdomain.PresencePresent, grace + archive + time.Hour,
			entdomain.PresenceArchived, true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, changed := nextPresence(tc.current, tc.absent, grace, archive)
			if next != tc.wantNext || changed != tc.wantChanged {
				t.Errorf("nextPresence(%v, %v) = (%v, %v), want (%v, %v)",
					tc.current, tc.absent, next, changed, tc.wantNext, tc.wantChanged)
			}
		})
	}
}
