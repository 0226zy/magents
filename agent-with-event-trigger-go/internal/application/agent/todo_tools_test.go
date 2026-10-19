package agent

import (
	"errors"
	"testing"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

func TestRewriteTodoListReportsCounts(t *testing.T) {
	service := newTestService(t, &fakeChat{}, &fakeTools{})

	result, err := service.rewriteTodoList(map[string]any{
		"items": []any{"查日历", "写草稿"},
	})
	if err != nil {
		t.Fatalf("rewriteTodoList 返回错误: %v", err)
	}
	payload := result.(map[string]any)
	if payload["new_items"] != 2 || payload["total_items"] != 2 {
		t.Errorf("统计结果 = %v", payload)
	}
}

// 整批里只要有一个非法状态，已经合法的那部分也不能生效。
func TestUpdateTodoStatusIsAllOrNothing(t *testing.T) {
	service := newTestService(t, &fakeChat{}, &fakeTools{})
	if _, err := service.rewriteTodoList(map[string]any{
		"items": []any{"第一项", "第二项"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := service.updateTodoStatus(map[string]any{
		"updates": []any{
			map[string]any{"id": float64(1), "status": "completed"},
			map[string]any{"id": float64(2), "status": "已完成"},
		},
	})
	if !errors.Is(err, derr.ErrInvalidTodoStatus) {
		t.Fatalf("错误 = %v，期望 ErrInvalidTodoStatus", err)
	}

	for _, item := range service.Todos() {
		if item.Status != entity.TodoPending {
			t.Errorf("条目 %d 状态 = %q，整批失败时应保持 pending", item.ID, item.Status)
		}
	}
}

func TestUpdateTodoStatusAppliesValidBatch(t *testing.T) {
	service := newTestService(t, &fakeChat{}, &fakeTools{})
	if _, err := service.rewriteTodoList(map[string]any{
		"items": []any{"第一项", "第二项"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.updateTodoStatus(map[string]any{
		"updates": []any{
			map[string]any{"id": float64(1), "status": "completed"},
			map[string]any{"id": float64(2), "status": "in_progress"},
		},
	}); err != nil {
		t.Fatalf("updateTodoStatus 返回错误: %v", err)
	}

	items := service.Todos()
	if items[0].Status != entity.TodoCompleted || items[1].Status != entity.TodoInProgress {
		t.Errorf("状态未按预期更新: %+v", items)
	}
}
