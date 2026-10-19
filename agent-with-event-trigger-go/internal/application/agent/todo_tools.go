package agent

import (
	"fmt"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// TODO 工具名。这两个工具操作的是 Agent 自身状态，不是外部能力，
// 因此在应用层处理，不下沉到 ToolPort——否则工具适配器要反向依赖应用层状态。
const (
	toolRewriteTodoList  = "rewrite_todo_list"
	toolUpdateTodoStatus = "update_todo_status"
)

// todoToolDefinitions 返回两个 TODO 工具的声明。
func todoToolDefinitions() []entity.ToolDefinition {
	statuses := make([]string, 0, len(entity.AllTodoStatuses()))
	for _, status := range entity.AllTodoStatuses() {
		statuses = append(statuses, string(status))
	}

	return []entity.ToolDefinition{
		{
			Type: "function",
			Function: entity.FunctionSpec{
				Name:        toolRewriteTodoList,
				Description: "Rewrite the TODO list with new pending items (keeps completed/cancelled items)",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "List of new TODO items to add as pending",
						},
					},
					"required": []string{"items"},
				},
			},
		},
		{
			Type: "function",
			Function: entity.FunctionSpec{
				Name:        toolUpdateTodoStatus,
				Description: "Update the status of existing TODO items",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"updates": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"id": map[string]any{
										"type":        "integer",
										"description": "TODO item ID",
									},
									"status": map[string]any{
										"type":        "string",
										"enum":        statuses,
										"description": "New status for the item",
									},
								},
								"required": []string{"id", "status"},
							},
							"description": "List of TODO items to update with their new status",
						},
					},
					"required": []string{"updates"},
				},
			},
		},
	}
}

// rewriteTodoList 执行 rewrite_todo_list。
func (s *EventAgentService) rewriteTodoList(args map[string]any) (any, error) {
	raw, ok := args["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("参数 items 必须是字符串数组")
	}
	contents := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("参数 items 的元素必须是字符串，收到 %T", item)
		}
		contents = append(contents, text)
	}

	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	result := s.todos.Rewrite(contents, s.clock.Now())
	return map[string]any{
		"success":     true,
		"kept_items":  result.Kept,
		"new_items":   result.New,
		"total_items": result.Total,
	}, nil
}

// updateTodoStatus 执行 update_todo_status。
//
// 先把全部更新解析并校验完，任一状态非法就整批不生效。Python 在循环中途抛异常，
// 前面的更新已经落地，调用方从返回值里看不出发生了半更新。
func (s *EventAgentService) updateTodoStatus(args map[string]any) (any, error) {
	raw, ok := args["updates"].([]any)
	if !ok {
		return nil, fmt.Errorf("参数 updates 必须是对象数组")
	}

	updates := make([]entity.TodoUpdate, 0, len(raw))
	for _, item := range raw {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("参数 updates 的元素必须是对象，收到 %T", item)
		}
		id, ok := numberToInt(fields["id"])
		if !ok {
			return nil, fmt.Errorf("参数 updates 的元素缺少整数字段 id")
		}
		statusText, ok := fields["status"].(string)
		if !ok {
			return nil, fmt.Errorf("参数 updates 的元素缺少字符串字段 status")
		}
		status, err := entity.ParseTodoStatus(statusText)
		if err != nil {
			return nil, err
		}
		updates = append(updates, entity.TodoUpdate{ID: id, Status: status})
	}

	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	result := s.todos.Update(updates, s.clock.Now())
	return map[string]any{
		"success":       true,
		"updated_items": result.Updated,
		"total_items":   result.Total,
	}, nil
}

// numberToInt 接受 JSON 解码出的 float64 与原生整数。
func numberToInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	default:
		return 0, false
	}
}
