package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNewFromEnvRequiresAPIKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	if _, err := NewFromEnv(); err == nil {
		t.Fatal("缺少 DEEPSEEK_API_KEY 时应返回错误")
	}
}

func TestNewFromEnvReadsConfig(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("DEEPSEEK_BASE_URL", "https://example.com/v1")
	t.Setenv("DEEPSEEK_MODEL", "deepseek-reasoner")

	c, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "https://example.com/v1" || c.model != "deepseek-reasoner" || c.apiKey != "sk-test" {
		t.Errorf("配置解析错误: %+v", c)
	}
}

func TestChatSendsBearerAndPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("错误路径: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-test" {
			t.Errorf("Authorization = %q", auth)
		}
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "deepseek-chat" {
			t.Errorf("Model = %q", req.Model)
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" {
			t.Errorf("消息格式错误: %+v", req.Messages)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{Choices: []struct {
			Message Message `json:"message"`
		}{{Message: Message{Role: "assistant", Content: "你好"}}}})
	}))
	defer srv.Close()

	c, err := NewClient("sk-test", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	answer, err := c.Chat(context.Background(), []Message{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: "问题"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "你好" {
		t.Errorf("answer = %q", answer)
	}
}

func TestChatRetriesOnRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(chatResponse{Error: &struct {
				Message string `json:"message"`
			}{Message: "rate limited"}})
			return
		}
		_ = json.NewEncoder(w).Encode(chatResponse{Choices: []struct {
			Message Message `json:"message"`
		}{{Message: Message{Content: "ok"}}}})
	}))
	defer srv.Close()

	c, err := NewClient("sk-test", WithBaseURL(srv.URL), WithMaxRetries(3))
	if err != nil {
		t.Fatal(err)
	}
	answer, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("429 应重试后成功: %v", err)
	}
	if answer != "ok" || calls.Load() != 2 {
		t.Errorf("answer=%q calls=%d", answer, calls.Load())
	}
}

func TestChatDoesNotRetryOnBadRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(chatResponse{Error: &struct {
			Message string `json:"message"`
		}{Message: "invalid request"}})
	}))
	defer srv.Close()

	c, err := NewClient("sk-test", WithBaseURL(srv.URL), WithMaxRetries(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("400 应直接失败")
	}
	if calls.Load() != 1 {
		t.Errorf("400 不应重试，调用次数 = %d", calls.Load())
	}
}
