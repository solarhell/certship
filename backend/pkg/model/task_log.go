package model

import "time"

// TaskLogEntry 任务日志条目
type TaskLogEntry struct {
	Time    time.Time `json:"time"`
	Content string    `json:"content"`
}
