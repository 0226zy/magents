package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

func captureRequest(t *testing.T, status int, body string) (*LLMClient, *map[string]any) {
	t.Helper()
	captured := map[string]any{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &captured); err != nil {
			t.Errorf("请求体不是合法 JSON: %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	client, err := NewLLMClient(LLMConfig{
		Provider: "kimi", APIKey: "test-key", BaseURL: server.URL, Model: "kimi-k3",
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, &captured
}

// 没有工具时不能发 tool_choice：部分 provider 会直接拒绝这种组合。
func TestChatOmitsToolChoiceWhenNoTools(t *testing.T) {
	client, captured := captureRequest(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"好"}}]}`)

	_, err := client.Chat(context.Background(),
		[]entity.Message{{Role: "user", Content: "你好"}}, nil,
		entity.ChatOptions{Temperature: 0.7, MaxTokens: 100, ToolChoice: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := (*captured)["tool_choice"]; present {
		t.Error("无工具时不应发送 tool_choice")
	}
}

// 推理模型的温度必须固定为 1，否则 provider 会报参数错误。
func TestChatClampsTemperatureForReasoningModels(t *testing.T) {
	client, captured := captureRequest(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"好"}}]}`)

	_, err := client.Chat(context.Background(),
		[]entity.Message{{Role: "user", Content: "你好"}}, nil,
		entity.ChatOptions{Temperature: entity.SafeTemperature("kimi-k3", 0.7), MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*captured)["temperature"]; got != float64(1) {
		t.Errorf("温度 = %v，kimi-k3 应被钳制为 1", got)
	}
}

func TestChatWrapsProviderError(t *testing.T) {
	client, _ := captureRequest(t, http.StatusBadRequest,
		`{"error":{"message":"tokenization failed","type":"invalid_request_error"}}`)

	_, err := client.Chat(context.Background(),
		[]entity.Message{{Role: "user", Content: "你好"}}, nil, entity.ChatOptions{MaxTokens: 10})
	if !errors.Is(err, derr.ErrProviderAPI) {
		t.Fatalf("错误 = %v，期望包装为 ErrProviderAPI", err)
	}
}

// reasoning_content 属于响应侧字段，回传给 provider 会直接 400。
func TestResponseReasoningContentIsNotEchoedBack(t *testing.T) {
	client, captured := captureRequest(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"答案",`+
			`"reasoning_content":"内部思考过程"}}]}`)

	resp, err := client.Chat(context.Background(),
		[]entity.Message{{Role: "user", Content: "你好"}}, nil, entity.ChatOptions{MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.ReasoningContent != "内部思考过程" {
		t.Fatal("响应应保留 reasoning_content 供调试")
	}

	// 把这条 assistant 消息放回历史再发一次，请求体里不应出现 reasoning_content。
	history := []entity.Message{
		{Role: "user", Content: "你好"},
		resp.Choices[0].Message.ToAssistantMessage(),
	}
	if _, err := client.Chat(context.Background(), history, nil,
		entity.ChatOptions{MaxTokens: 10}); err != nil {
		t.Fatal(err)
	}

	encoded, _ := json.Marshal(*captured)
	if strings.Contains(string(encoded), "reasoning_content") {
		t.Errorf("请求体不应含 reasoning_content: %s", encoded)
	}
}
