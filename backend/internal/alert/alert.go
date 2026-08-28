// Package alert 决定一条域名状态该不该发通知。
//
// 存在的理由:一个坏掉的域名每个扫描周期失败一次,就每个周期推一条飞书,
// 几天后没人再看这个群。告警应该反映"状态变化",而不是"又失败了一次"。
package alert

import "time"

// State 域名对外呈现的状态
type State string

const (
	StateOK       State = "ok"
	StateFailed   State = "failed"
	StateBlocked  State = "blocked"
	StateMissing  State = "missing"
	StateArchived State = "archived"
)

// RepeatInterval 状态持续不变时的重复提醒间隔。
// 只提醒一次容易被淹没,每轮提醒又是刷屏,一周一次是折中。
const RepeatInterval = 7 * 24 * time.Hour

// ShouldNotify 判断本次是否值得打扰人。
//
// prev/prevAt 是上一次实际发出通知时的状态与时间(prev 为空表示从未通知过)。
//
// 规则:
//   - 状态变了 → 通知
//   - 状态没变且仍是异常 → 每 RepeatInterval 提醒一次
//   - 恢复正常 → 只在之前确实报过异常时通知,避免首轮把所有正常域名刷一遍
func ShouldNotify(prev State, prevAt *time.Time, next State, now time.Time) bool {
	if next == StateOK {
		return prev != "" && prev != StateOK
	}
	if prev != next {
		return true
	}
	if prevAt == nil {
		return true
	}
	return now.Sub(*prevAt) >= RepeatInterval
}
