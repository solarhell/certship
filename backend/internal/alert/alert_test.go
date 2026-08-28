package alert

import (
	"testing"
	"time"
)

func TestShouldNotify(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	justNow := now.Add(-time.Hour)
	longAgo := now.Add(-8 * 24 * time.Hour)

	cases := []struct {
		name   string
		prev   State
		prevAt *time.Time
		next   State
		want   bool
	}{
		{"首次失败要通知", "", nil, StateFailed, true},
		{"刚通知过的同一失败不再刷屏", StateFailed, &justNow, StateFailed, false},
		{"失败持续超过一周再提醒一次", StateFailed, &longAgo, StateFailed, true},
		{"失败转阻塞是状态变化", StateFailed, &justNow, StateBlocked, true},
		{"从失败恢复要通知", StateFailed, &justNow, StateOK, true},
		{"从未报过错就不报恢复", "", nil, StateOK, false},
		{"一直正常不重复通知", StateOK, &longAgo, StateOK, false},
		{"域名下线要通知", StateOK, &justNow, StateMissing, true},
		{"下线转归档是状态变化", StateMissing, &justNow, StateArchived, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldNotify(tc.prev, tc.prevAt, tc.next, now); got != tc.want {
				t.Errorf("ShouldNotify(%q, %v, %q) = %v, want %v", tc.prev, tc.prevAt, tc.next, got, tc.want)
			}
		})
	}
}
