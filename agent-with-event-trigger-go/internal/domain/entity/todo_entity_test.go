package entity

import (
	"strings"
	"testing"
	"time"
)

func fixedTime() time.Time {
	return time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
}

// Rewrite 保留已完成与已取消的历史，只替换还没做完的部分。
func TestTodoListRewriteKeepsFinishedItems(t *testing.T) {
	var list TodoList
	list.Rewrite([]string{"查日历", "写草稿"}, fixedTime())
	list.Update([]TodoUpdate{{ID: 1, Status: TodoCompleted}}, fixedTime())

	result := list.Rewrite([]string{"归档邮件"}, fixedTime())
	if result.Kept != 1 || result.New != 1 || result.Total != 2 {
		t.Fatalf("Rewrite 结果 = %+v，期望 kept=1 new=1 total=2", result)
	}

	items := list.Items()
	if items[0].Content != "查日历" || items[0].Status != TodoCompleted {
		t.Errorf("已完成条目应被保留，实际 = %+v", items[0])
	}
	if items[1].Content != "归档邮件" {
		t.Errorf("新条目应追加在后，实际 = %+v", items[1])
	}
}

// ID 单调递增且永不复用：模型在对话里用 ID 指代任务，
// 复用会让之前说的「完成第 2 项」指到一条全新的任务上。
func TestTodoListIDsAreNeverReused(t *testing.T) {
	var list TodoList
	list.Rewrite([]string{"甲", "乙"}, fixedTime())
	list.Rewrite([]string{"丙"}, fixedTime())

	items := list.Items()
	if len(items) != 1 {
		t.Fatalf("未完成条目应被替换，当前 %d 条", len(items))
	}
	if items[0].ID != 3 {
		t.Errorf("新条目 ID = %d，期望 3（1、2 已用过）", items[0].ID)
	}
}

func TestTodoListUpdateSkipsUnknownID(t *testing.T) {
	var list TodoList
	list.Rewrite([]string{"唯一一项"}, fixedTime())

	result := list.Update([]TodoUpdate{
		{ID: 1, Status: TodoInProgress},
		{ID: 99, Status: TodoCompleted},
	}, fixedTime())

	if result.Updated != 1 {
		t.Errorf("更新计数 = %d，未知 ID 不应计入", result.Updated)
	}
	if list.Items()[0].Status != TodoInProgress {
		t.Errorf("已知 ID 应被更新，实际状态 = %q", list.Items()[0].Status)
	}
	if list.Items()[0].UpdatedAt == nil {
		t.Error("更新后应写入 updated_at")
	}
}

func TestTodoListFormatShowsStatusSymbols(t *testing.T) {
	var list TodoList
	if got := list.Format(); got != "TODO List: Empty" {
		t.Errorf("空列表渲染 = %q", got)
	}

	list.Rewrite([]string{"甲", "乙"}, fixedTime())
	list.Update([]TodoUpdate{{ID: 1, Status: TodoCompleted}}, fixedTime())

	formatted := list.Format()
	if !strings.Contains(formatted, TodoCompleted.Symbol()) ||
		!strings.Contains(formatted, TodoPending.Symbol()) {
		t.Errorf("渲染结果应同时含已完成与待办符号:\n%s", formatted)
	}
}

func TestParseTodoStatusRejectsUnknown(t *testing.T) {
	if _, err := ParseTodoStatus("completed"); err != nil {
		t.Fatalf("合法状态被拒: %v", err)
	}
	if _, err := ParseTodoStatus("done"); err == nil {
		t.Fatal("非法状态应报错")
	}
}
