package entity

import (
	"fmt"
	"strings"
)

// ToolCallRecord 是一次工具调用的完整留痕，写进轨迹文件。
type ToolCallRecord struct {
	ToolName   string         `json:"tool_name"`
	Arguments  map[string]any `json:"arguments"`
	Result     any            `json:"result"`
	Error      string         `json:"error,omitempty"`
	CallNumber int            `json:"call_number"`
	Timestamp  string         `json:"timestamp"`
	DurationMs int64          `json:"duration_ms"`
}

// ToolCounter 按工具名累计调用次数。
//
// 计数跨事件累积，只有 Reset 清零：模型看到 "Tool call #7" 才意识到自己在原地打转，
// 这正是系统提示词里要求它换策略的依据。
type ToolCounter struct {
	counts map[string]int
	total  int
}

// Next 记录一次调用并返回该工具的调用序号。enabled 为 false 时恒返回 1。
func (c *ToolCounter) Next(name string, enabled bool) int {
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	c.total++
	if !enabled {
		return 1
	}
	c.counts[name]++
	return c.counts[name]
}

// Total 返回累计调用次数。
func (c *ToolCounter) Total() int { return c.total }

// Reset 清零全部计数。
func (c *ToolCounter) Reset() {
	c.counts = map[string]int{}
	c.total = 0
}

// ToolResultPrefix 生成工具结果前缀，如 "[2026-10-12 10:30:00] [Tool call #3 for 'read_file']\n"。
// 两个开关都关闭时返回空串，此时工具结果不带任何前缀。
func ToolResultPrefix(timestamp string, callNumber int, toolName string, cfg SystemHintConfig) string {
	parts := make([]string, 0, 2)
	if cfg.EnableTimestamps {
		parts = append(parts, fmt.Sprintf("[%s]", timestamp))
	}
	if cfg.EnableToolCounter {
		parts = append(parts, fmt.Sprintf("[Tool call #%d for '%s']", callNumber, toolName))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + "\n"
}
