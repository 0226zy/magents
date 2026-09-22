package moonshot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const chatBody = `{
  "id": "chat-1",
  "choices": [{
    "finish_reason": "stop",
    "message": {"content": "answer", "reasoning_content": "thinking", "tool_calls": null}
  }],
  "usage": {
    "prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15,
    "prompt_tokens_details": {"cached_tokens": 4},
    "completion_tokens_details": {"reasoning_tokens": 3}
  }
}`

func newChat(t *testing.T, model string, handler http.HandlerFunc) (*ChatClient, *Recorder) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	recorder := &Recorder{}
	return NewChatClient("secret-key", server.URL+"/", model, recorder, server.Client()), recorder
}

func TestChatRequestShapeMatchesPythonKwargs(t *testing.T) {
	var gotBody []byte
	var gotPath string
	client, recorder := newChat(t, "kimi-k3", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Write([]byte(chatBody))
	})

	declaration := json.RawMessage(`{"type":"function","function":{"name":"web_search","parameters":{},"extra":1}}`)
	response, err := client.Chat(context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		[]json.RawMessage{declaration})
	if err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("端点不对: %s", gotPath)
	}
	if response.Choices[0].Message.ReasoningContent != "thinking" {
		t.Fatalf("reasoning_content 未解析: %+v", response.Choices[0].Message)
	}
	if response.Usage.PromptTokensDetails.CachedTokens != 4 ||
		response.Usage.CompletionTokensDetails.ReasoningTokens != 3 {
		t.Fatalf("usage 明细未解析: %+v", response.Usage)
	}

	var request struct {
		Model           string            `json:"model"`
		Temperature     float64           `json:"temperature"`
		MaxTokens       int               `json:"max_tokens"`
		ReasoningEffort string            `json:"reasoning_effort"`
		Tools           []json.RawMessage `json:"tools"`
		Messages        []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &request); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v", err)
	}
	if request.Model != "kimi-k3" || request.MaxTokens != 32768 {
		t.Fatalf("model / max_tokens 不对: %+v", request)
	}
	if request.Temperature != 1 {
		t.Fatalf("kimi-k3 必须用 temperature=1，实际 %v", request.Temperature)
	}
	if request.ReasoningEffort != "max" {
		t.Fatalf("kimi-k3 必须带 reasoning_effort=max，实际 %q", request.ReasoningEffort)
	}
	// 声明原样透传：provider schema 的未知字段不能被丢弃。
	if !strings.Contains(string(request.Tools[0]), `"extra":1`) {
		t.Fatalf("工具声明未原样透传: %s", request.Tools[0])
	}

	turns := recorder.Turns()
	if len(turns) != 1 || turns[0].Kind != "chat_completion" {
		t.Fatalf("凭据不对: %+v", turns)
	}
	assertCredentialFreeTurn(t, turns[0])
}

func TestChatOmitsReasoningEffortForNonK3Models(t *testing.T) {
	var gotBody []byte
	client, _ := newChat(t, "deepseek-chat", func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Write([]byte(chatBody))
	})
	client.SetTemperature(0.2)

	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}

	var request map[string]json.RawMessage
	json.Unmarshal(gotBody, &request)
	if _, ok := request["reasoning_effort"]; ok {
		t.Fatalf("非推理模型不应带 reasoning_effort: %s", gotBody)
	}
	if _, ok := request["tools"]; ok {
		t.Fatalf("没有声明时不应带 tools 字段: %s", gotBody)
	}
	if string(request["temperature"]) != "0.2" {
		t.Fatalf("非推理模型应沿用请求的温度: %s", request["temperature"])
	}
}

func TestChatSerializesEmptyAssistantContent(t *testing.T) {
	var gotBody []byte
	client, _ := newChat(t, "kimi-k3", func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Write([]byte(chatBody))
	})

	history := []Message{{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCall{{
			ID: "call-1", Type: "function",
			Function: FunctionCall{Name: "web_search", Arguments: `{"query":"x"}`},
		}},
	}}
	if _, err := client.Chat(context.Background(), history, nil); err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}

	var request struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	json.Unmarshal(gotBody, &request)
	content, ok := request.Messages[0]["content"]
	if !ok || string(content) != `""` {
		// assistant 消息即使内容为空也必须带 "content": ""。
		t.Fatalf("空 content 必须被序列化: %s", gotBody)
	}
}

func TestChatRejectsResponseWithoutChoices(t *testing.T) {
	client, recorder := newChat(t, "kimi-k3", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"chat-1","choices":[]}`))
	})

	if _, err := client.Chat(context.Background(), nil, nil); err == nil ||
		!strings.Contains(err.Error(), "没有 choices") {
		t.Fatalf("空 choices 必须报错，实际: %v", err)
	}
	if turns := recorder.Turns(); len(turns) != 1 || turns[0].Error == nil {
		t.Fatalf("失败也要留凭据: %+v", turns)
	}
}

func TestChatMapsTooManyRequestsToRateLimit(t *testing.T) {
	client, recorder := newChat(t, "kimi-k3", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	})

	_, err := client.Chat(context.Background(), nil, nil)
	if !IsRateLimit(err) {
		t.Fatalf("429 必须识别为速率限制，实际: %v", err)
	}
	turns := recorder.Turns()
	if turns[0].Error.Class != "RateLimitError" {
		t.Fatalf("错误分类不对: %s", turns[0].Error.Class)
	}
	if !strings.Contains(string(turns[0].Error.Response), "rate limited") {
		t.Fatalf("429 body 未进入证据: %s", turns[0].Error.Response)
	}
}

func TestChatContextCancellationIsReportedAsTimeout(t *testing.T) {
	client, _ := newChat(t, "kimi-k3", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Chat(ctx, nil, nil)
	if err == nil {
		t.Fatal("取消的请求必须报错")
	}
}

func TestReasoningSafeTemperature(t *testing.T) {
	cases := []struct {
		model string
		want  float64
	}{
		{"kimi-k3", 1},
		{"openai/gpt-5.6-luna", 1},
		{"deepseek-chat", 0.2},
	}
	for _, tc := range cases {
		if got := ReasoningSafeTemperature(tc.model, 0.2); got != tc.want {
			t.Fatalf("%s 期望温度 %v，实际 %v", tc.model, tc.want, got)
		}
	}
}

func TestToAssistantMessageDropsReasoningContent(t *testing.T) {
	message := ChoiceMessage{
		Content:          "answer",
		ReasoningContent: "internal thinking",
		ToolCalls:        []ToolCall{{ID: "call-1", Type: "function"}},
	}

	serialized, err := json.Marshal(message.ToAssistantMessage())
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(string(serialized), "reasoning") {
		t.Fatalf("回传消息不得含 reasoning_content: %s", serialized)
	}
	if !strings.Contains(string(serialized), `"role":"assistant"`) {
		t.Fatalf("角色不对: %s", serialized)
	}
}

func TestHasWebSearchDeclarationRequiresParametersObject(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"完整声明", `{"type":"function","function":{"name":"web_search","parameters":{"type":"object"}}}`, true},
		{"parameters 缺失", `{"type":"function","function":{"name":"web_search"}}`, false},
		{"parameters 为 null", `{"type":"function","function":{"name":"web_search","parameters":null}}`, false},
		{"parameters 非对象", `{"type":"function","function":{"name":"web_search","parameters":[]}}`, false},
		{"名字不对", `{"type":"function","function":{"name":"other","parameters":{}}}`, false},
		{"类型不对", `{"type":"custom","function":{"name":"web_search","parameters":{}}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasWebSearchDeclaration([]json.RawMessage{json.RawMessage(tc.raw)})
			if got != tc.want {
				t.Fatalf("期望 %v，实际 %v", tc.want, got)
			}
		})
	}
}
