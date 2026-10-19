package entity

import (
	"fmt"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

// TodoStatus 是 TODO 条目的状态。
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
	TodoCancelled  TodoStatus = "cancelled"
)

// AllTodoStatuses 按声明顺序返回全部状态，供工具 schema 的 enum 使用。
func AllTodoStatuses() []TodoStatus {
	return []TodoStatus{TodoPending, TodoInProgress, TodoCompleted, TodoCancelled}
}

// ParseTodoStatus 解析状态字符串，非法值返回 derr.ErrInvalidTodoStatus。
func ParseTodoStatus(s string) (TodoStatus, error) {
	candidate := TodoStatus(strings.TrimSpace(s))
	for _, known := range AllTodoStatuses() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("%w: %s", derr.ErrInvalidTodoStatus, s)
}

// Symbol 返回状态在 TODO 列表里的显示符号。
func (s TodoStatus) Symbol() string {
	switch s {
	case TodoPending:
		return "⏳"
	case TodoInProgress:
		return "🔄"
	case TodoCompleted:
		return "✅"
	case TodoCancelled:
		return "❌"
	default:
		return "❓"
	}
}

// TodoItem 是一条 TODO。
type TodoItem struct {
	ID        int        `json:"id"`
	Content   string     `json:"content"`
	Status    TodoStatus `json:"status"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt *string    `json:"updated_at"`
}

// TodoUpdate 是一次状态变更请求。状态在构造时已经过 ParseTodoStatus 校验，
// 因此聚合根只需处理「ID 不存在」一种情况。
type TodoUpdate struct {
	ID     int
	Status TodoStatus
}

// RewriteResult 是重写 TODO 列表的结果统计。
type RewriteResult struct {
	Kept  int `json:"kept_items"`
	New   int `json:"new_items"`
	Total int `json:"total_items"`
}

// UpdateResult 是批量更新状态的结果统计。
type UpdateResult struct {
	Updated int `json:"updated_items"`
	Total   int `json:"total_items"`
}

// TodoList 是 TODO 聚合根。非线程安全，由持有它的应用服务加锁。
type TodoList struct {
	items  []TodoItem
	nextID int
}

// Rewrite 用新条目替换未完成的任务，保留已完成与已取消的历史。
// ID 单调递增且永不复用：模型在对话里用 ID 指代任务，复用会让历史引用指向错误条目。
func (l *TodoList) Rewrite(contents []string, now time.Time) RewriteResult {
	l.ensureInit()

	kept := make([]TodoItem, 0, len(l.items))
	for _, item := range l.items {
		if item.Status == TodoCompleted || item.Status == TodoCancelled {
			kept = append(kept, item)
		}
	}

	created := now.Format(time.RFC3339)
	for _, content := range contents {
		kept = append(kept, TodoItem{
			ID:        l.nextID,
			Content:   content,
			Status:    TodoPending,
			CreatedAt: created,
		})
		l.nextID++
	}

	keptCount := len(kept) - len(contents)
	l.items = kept
	return RewriteResult{Kept: keptCount, New: len(contents), Total: len(l.items)}
}

// Update 批量更新状态。未知 ID 静默跳过且不计数，与 Python 行为一致。
func (l *TodoList) Update(updates []TodoUpdate, now time.Time) UpdateResult {
	l.ensureInit()

	updatedAt := now.Format(time.RFC3339)
	changed := 0
	for _, update := range updates {
		for i := range l.items {
			if l.items[i].ID != update.ID {
				continue
			}
			stamp := updatedAt
			l.items[i].Status = update.Status
			l.items[i].UpdatedAt = &stamp
			changed++
			break
		}
	}
	return UpdateResult{Updated: changed, Total: len(l.items)}
}

// Format 渲染 TODO 列表，供 system hint 使用。
func (l *TodoList) Format() string {
	if len(l.items) == 0 {
		return "TODO List: Empty"
	}
	var b strings.Builder
	b.WriteString("TODO List:")
	for _, item := range l.items {
		fmt.Fprintf(&b, "\n  [%d] %s %s (%s)", item.ID, item.Status.Symbol(), item.Content, item.Status)
	}
	return b.String()
}

// Items 返回条目副本，调用方无法透过它改到聚合根内部状态。
func (l *TodoList) Items() []TodoItem {
	out := make([]TodoItem, len(l.items))
	copy(out, l.items)
	return out
}

// Len 返回条目数量。
func (l *TodoList) Len() int { return len(l.items) }

// Clear 清空列表并把 ID 计数器复位，仅由 Agent 的 Reset 调用。
func (l *TodoList) Clear() {
	l.items = nil
	l.nextID = 1
}

func (l *TodoList) ensureInit() {
	if l.nextID == 0 {
		l.nextID = 1
	}
}
