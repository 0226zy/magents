package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/entity"
)

// fakeLLM 模拟两轮对话：第一轮请求工具，第二轮给出最终回答。
type fakeLLM struct {
	calls    int
	toolName string
}

func (f *fakeLLM) Chat(_ context.Context, messages []entity.Message, tools []entity.ToolDefinition) (*entity.ChatResponse, error) {
	f.calls++
	resp := &entity.ChatResponse{Raw: json.RawMessage(`{"id":"chat","choices":[...]}`)}
	resp.Choices = make([]struct {
		Message      entity.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	}, 1)
	switch f.calls {
	case 1:
		resp.Choices[0].Message = entity.Message{
			Role: "assistant",
			ToolCalls: []entity.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: entity.FunctionCall{
					Name:      f.toolName,
					Arguments: `{"timezone":"America/Vancouver"}`,
				},
			}},
		}
	default:
		resp.Choices[0].Message = entity.Message{Role: "assistant", Content: "现在温哥华是周六，天气晴朗。"}
		resp.Choices[0].FinishReason = "stop"
	}
	return resp, nil
}

type fakeTools struct {
	executed int
}

func (f *fakeTools) Definitions() []entity.ToolDefinition {
	return []entity.ToolDefinition{{
		Type:     "function",
		Function: entity.FunctionSpec{Name: "get_current_time", Description: "time", Parameters: map[string]any{}},
	}}
}

func (f *fakeTools) Execute(name, _ string) (string, error) {
	f.executed++
	return `{"datetime":"2026-09-22T05:18:47"}`, nil
}

type fakeTrajectory struct{ lines []string }

func (f *fakeTrajectory) Event(format string, args ...any) {
	f.lines = append(f.lines, fmt.Sprintf(format, args...))
}
func (f *fakeTrajectory) EventJSON(label string, value any) {
	payload, _ := json.Marshal(value)
	f.lines = append(f.lines, fmt.Sprintf("%s: %s", label, payload))
}

type fakeRawLog struct {
	records [][]byte
}

func (f *fakeRawLog) LogResponse(iteration int, rawJSON []byte) {
	f.records = append(f.records, rawJSON)
}

func TestExecuteExecutesToolThenReturnsFinalAnswer(t *testing.T) {
	llm := &fakeLLM{toolName: "get_current_time"}
	tl := &fakeTools{}
	traj := &fakeTrajectory{}
	raw := &fakeRawLog{}
	svc := NewAgentService(llm, tl, traj, raw, 8)

	final, err := svc.Execute(context.Background(), "system", "温哥华现在几点？")
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if !strings.Contains(final, "温哥华") {
		t.Errorf("最终回答不符合预期: %q", final)
	}
	if llm.calls != 2 {
		t.Errorf("模型调用次数 = %d，期望 2", llm.calls)
	}
	if tl.executed != 1 {
		t.Errorf("工具执行次数 = %d，期望 1", tl.executed)
	}
	if len(traj.lines) == 0 {
		t.Error("轨迹没有任何输出")
	}
	if len(raw.records) != 2 {
		t.Errorf("raw 记录数 = %d，期望每轮一条（共 2）", len(raw.records))
	}
}

// stuckLLM 每次都请求同一个工具，用于验证 max_iterations 保护。
type stuckLLM struct{ calls int }

func (s *stuckLLM) Chat(_ context.Context, _ []entity.Message, _ []entity.ToolDefinition) (*entity.ChatResponse, error) {
	s.calls++
	resp := &entity.ChatResponse{}
	resp.Choices = make([]struct {
		Message      entity.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	}, 1)
	resp.Choices[0].Message = entity.Message{
		Role: "assistant",
		ToolCalls: []entity.ToolCall{{
			ID:       "call_x",
			Type:     "function",
			Function: entity.FunctionCall{Name: "get_current_time", Arguments: "{}"},
		}},
	}
	return resp, nil
}

func TestExecuteRespectsMaxIterations(t *testing.T) {
	svc := NewAgentService(&stuckLLM{}, &fakeTools{}, &fakeTrajectory{}, nil, 3)
	_, err := svc.Execute(context.Background(), "system", "问题")
	if !errors.Is(err, derr.ErrMaxIterations) {
		t.Errorf("err = %v，期望 derr.ErrMaxIterations", err)
	}
}
