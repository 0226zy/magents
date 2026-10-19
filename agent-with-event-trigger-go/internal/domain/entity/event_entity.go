// Package entity 存放领域层的协议 DTO 与聚合根：事件、消息、TODO、工具调用、
// 后台进程、轨迹快照与系统提示配置。只依赖标准库，application 与 infrastructure
// 都依赖这里，方向不允许反过来。
package entity

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

// EventType 是能唤醒 Agent 的事件种类。
type EventType string

// 外部输入事件。
const (
	EventWebMessage     EventType = "web_message"
	EventIMMessage      EventType = "im_message"
	EventEmailReply     EventType = "email_reply"
	EventGitHubPRUpdate EventType = "github_pr_update"
	EventTimerTrigger   EventType = "timer_trigger"
	EventFileChange     EventType = "file_change"
)

// 系统提醒事件：由 Monitor 产生，不来自外部请求。
const (
	EventUserTimeout    EventType = "user_timeout"
	EventProcessTimeout EventType = "process_timeout"
	EventSystemAlert    EventType = "system_alert"
)

// AllEventTypes 按声明顺序返回全部事件类型，供 CLI 提示与参数校验使用。
func AllEventTypes() []EventType {
	return []EventType{
		EventWebMessage, EventIMMessage, EventEmailReply, EventGitHubPRUpdate,
		EventTimerTrigger, EventFileChange,
		EventUserTimeout, EventProcessTimeout, EventSystemAlert,
	}
}

// ParseEventType 把字符串解析为事件类型，未知值返回 derr.ErrUnknownEventType。
func ParseEventType(s string) (EventType, error) {
	candidate := EventType(strings.TrimSpace(s))
	for _, known := range AllEventTypes() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("%w: %s", derr.ErrUnknownEventType, s)
}

// IsUserInteraction 报告该事件是否代表「用户刚刚说过话」。
// 只有这三种会刷新空闲计时，决定用户超时提醒何时触发。
func (t EventType) IsUserInteraction() bool {
	switch t {
	case EventWebMessage, EventIMMessage, EventEmailReply:
		return true
	default:
		return false
	}
}

// Event 是一次唤醒 Agent 的事件。
type Event struct {
	ID        string         `json:"event_id"`
	Type      EventType      `json:"event_type"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata"`
	Timestamp time.Time      `json:"timestamp"`
}

// eventSequence 给事件 ID 补一个单调序号。
//
// Python 版 ID 只有毫秒时间戳，同一毫秒内产生的事件（多个触发器同时到期、
// 客户端连发）会拿到同一个 ID，轨迹与客户端都无法区分它们。
var eventSequence atomic.Uint64

// NewEvent 构造事件并补齐缺省字段：空 ID 生成 evt_<unixmilli>_<序号>，
// nil metadata 换成空 map。这样调用方拿到的事件永远不需要判空。
func NewEvent(t EventType, content string, meta map[string]any, now time.Time) Event {
	if now.IsZero() {
		now = time.Now()
	}
	if meta == nil {
		meta = map[string]any{}
	}
	return Event{
		ID:        fmt.Sprintf("evt_%d_%d", now.UnixMilli(), eventSequence.Add(1)),
		Type:      t,
		Content:   content,
		Metadata:  meta,
		Timestamp: now,
	}
}

// WithID 返回指定了事件 ID 的副本；id 为空时保持原值。
func (e Event) WithID(id string) Event {
	if strings.TrimSpace(id) != "" {
		e.ID = id
	}
	return e
}

// ToUserMessage 把事件渲染成发给模型的 user 消息，让模型知道这次唤醒来自哪里。
// 渲染表与 Python 版 event_types.py 的 to_user_message() 逐字一致。
func (e Event) ToUserMessage() string {
	switch e.Type {
	case EventWebMessage:
		return fmt.Sprintf("[Web Interface] %s", e.Content)
	case EventIMMessage:
		return fmt.Sprintf("[IM from %s] %s", e.meta("sender", "Unknown"), e.Content)
	case EventEmailReply:
		return fmt.Sprintf("[Email Reply from %s]\nSubject: %s\n%s",
			e.meta("from", "Unknown"), e.meta("subject", "No Subject"), e.Content)
	case EventGitHubPRUpdate:
		return fmt.Sprintf("[GitHub PR #%s %s] %s",
			e.meta("pr_number", "Unknown"), e.meta("action", "updated"), e.Content)
	case EventTimerTrigger:
		return fmt.Sprintf("[Timer %s triggered] %s", e.meta("timer_id", "Unknown"), e.Content)
	case EventFileChange:
		return fmt.Sprintf("[File %s: %s] %s",
			e.meta("change", "modified"), e.meta("path", "Unknown"), e.Content)
	case EventUserTimeout:
		return fmt.Sprintf("[System Reminder] User has not responded for %s. %s",
			e.meta("duration", "unknown"), e.Content)
	case EventProcessTimeout:
		return fmt.Sprintf("[System Alert] Background process %s has been running for %s. %s",
			e.meta("process_id", "Unknown"), e.meta("duration", "unknown"), e.Content)
	case EventSystemAlert:
		return fmt.Sprintf("[System Alert: %s] %s", e.meta("alert_type", "general"), e.Content)
	default:
		return fmt.Sprintf("[%s] %s", e.Type, e.Content)
	}
}

// meta 取 metadata 里的值并渲染成字符串，缺失或空值时用 fallback。
// 统一走 fmt.Sprint，使 JSON 里的数字 42 与字符串 "42" 渲染结果相同。
func (e Event) meta(key, fallback string) string {
	raw, ok := e.Metadata[key]
	if !ok || raw == nil {
		return fallback
	}
	value := fmt.Sprint(raw)
	if value == "" {
		return fallback
	}
	return value
}
