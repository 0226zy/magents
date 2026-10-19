package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/agent"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/monitor"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

type scriptedChat struct {
	mu      sync.Mutex
	content string
	block   chan struct{}
}

func (c *scriptedChat) Chat(_ context.Context, _ []entity.Message, _ []entity.ToolDefinition,
	_ entity.ChatOptions) (*entity.ChatResponse, error) {

	if c.block != nil {
		<-c.block
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return &entity.ChatResponse{Choices: []entity.Choice{{
		Message: entity.ChoiceMessage{Role: "assistant", Content: c.content},
	}}}, nil
}

func (c *scriptedChat) Model() string    { return "fake-model" }
func (c *scriptedChat) Provider() string { return "fake" }

type noTools struct{}

func (noTools) Definitions() []entity.ToolDefinition { return nil }
func (noTools) Has(string) bool                      { return false }
func (noTools) Execute(context.Context, string, map[string]any) (any, error) {
	return nil, nil
}
func (noTools) CurrentDirectory() string { return "/tmp" }
func (noTools) Reset()                   {}

type systemClock struct{}

func (systemClock) Now() time.Time        { return time.Now() }
func (systemClock) Advance(time.Duration) {}

type silentLogger struct{}

func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}

func newTestServer(t *testing.T, chat *scriptedChat) (*httptest.Server, *agent.EventAgentService) {
	t.Helper()
	hints := entity.DefaultSystemHintConfig()
	hints.SaveTrajectory = false

	service := agent.New(agent.Deps{
		Chat: chat, Builtin: noTools{}, Workspace: noTools{},
		Clock: systemClock{}, Logger: silentLogger{}, Hints: hints,
		Options: entity.ChatOptions{MaxTokens: 100}, SystemPrompt: "测试", MaxIterations: 3,
	})
	mon := monitor.New(service, service, systemClock{}, silentLogger{}, monitor.Config{})

	server := httptest.NewServer(New(service, mon, silentLogger{}).Handler())
	t.Cleanup(server.Close)
	return server, service
}

func post(t *testing.T, server *httptest.Server, path string, body any) (int, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+path, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	payload := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return resp.StatusCode, payload
}

func get(t *testing.T, server *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	payload := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return resp.StatusCode, payload
}

func TestPostEventReturnsResultWithEventID(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: 收到"})

	status, payload := post(t, server, "/event", map[string]any{
		"event_type": "web_message", "content": "你好",
	})
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %v", status, payload)
	}
	if payload["final_answer"] != "收到" {
		t.Errorf("最终回答 = %v", payload["final_answer"])
	}
	// Python 版把 event_id 算出来却没放进响应，客户端无法关联请求与轨迹。
	if id, _ := payload["event_id"].(string); id == "" {
		t.Error("响应应带上 event_id")
	}
	if _, ok := payload["tool_calls_total"]; !ok {
		t.Error("响应应同时给出单次与累计工具调用数")
	}
}

// 未知事件类型是客户端错误，Python 版在这里抛 500。
func TestPostEventRejectsUnknownEventType(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	status, _ := post(t, server, "/event", map[string]any{
		"event_type": "telepathy", "content": "你好",
	})
	if status != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 400", status)
	}
}

func TestPostEventRejectsEmptyContent(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	status, _ := post(t, server, "/event", map[string]any{
		"event_type": "web_message", "content": "   ",
	})
	if status != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 400", status)
	}
}

// 进程注册要同时认 name 与 process_name：
// Python 的服务端声明 name、客户端却发 process_name，注册必然 422。
func TestRegisterProcessAcceptsBothFieldSpellings(t *testing.T) {
	server, service := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	for _, body := range []map[string]any{
		{"process_id": "proc_a", "name": "任务 A"},
		{"process_id": "proc_b", "process_name": "任务 B"},
	} {
		status, payload := post(t, server, "/process/register", body)
		if status != http.StatusOK {
			t.Fatalf("状态码 = %d，响应 = %v", status, payload)
		}
		if payload["process_name"] == "" {
			t.Errorf("响应应回显进程名: %v", payload)
		}
	}

	if got := service.Status().Processes; got != 2 {
		t.Errorf("已注册进程数 = %d，期望 2", got)
	}

	status, payload := post(t, server, "/process/unregister", map[string]any{"process_id": "proc_a"})
	if status != http.StatusOK || payload["existed"] != true {
		t.Errorf("注销响应 = %d %v", status, payload)
	}
}

func TestRegisterProcessRequiresID(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})
	if status, _ := post(t, server, "/process/register", map[string]any{"name": "无 ID"}); status != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 400", status)
	}
}

// Python 的客户端会调 /monitoring/start，但服务端根本没有这组端点。
func TestMonitoringEndpointsExist(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	status, payload := post(t, server, "/monitoring/start", map[string]any{
		"user_timeout_seconds": 5,
	})
	if status != http.StatusOK || payload["running"] != true {
		t.Fatalf("启动监控响应 = %d %v", status, payload)
	}
	if payload["user_timeout_seconds"] != float64(5) {
		t.Errorf("阈值未被覆盖: %v", payload)
	}

	status, payload = get(t, server, "/monitoring/status")
	if status != http.StatusOK || payload["running"] != true {
		t.Fatalf("监控状态 = %d %v", status, payload)
	}

	status, payload = post(t, server, "/monitoring/stop", nil)
	if status != http.StatusOK || payload["running"] != false {
		t.Fatalf("停止监控响应 = %d %v", status, payload)
	}
}

func TestResetClearsHistory(t *testing.T) {
	server, service := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	if status, _ := post(t, server, "/event", map[string]any{
		"event_type": "web_message", "content": "你好",
	}); status != http.StatusOK {
		t.Fatal("事件投递失败")
	}

	if status, payload := post(t, server, "/agent/reset", nil); status != http.StatusOK ||
		payload["success"] != true {
		t.Fatalf("重置响应 = %d %v", status, payload)
	}
	if got := len(service.History()); got != 1 {
		t.Errorf("重置后历史 = %d 条，期望只剩 system prompt", got)
	}
}

func TestStatusAndHealthAndRoot(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	if status, payload := get(t, server, "/health"); status != http.StatusOK ||
		payload["status"] != "healthy" {
		t.Errorf("健康检查 = %d %v", status, payload)
	}
	if status, payload := get(t, server, "/agent/status"); status != http.StatusOK ||
		payload["provider"] != "fake" {
		t.Errorf("状态端点 = %d %v", status, payload)
	}
	if status, payload := get(t, server, "/"); status != http.StatusOK ||
		payload["endpoints"] == nil {
		t.Errorf("根端点 = %d %v", status, payload)
	}
}

// /health 不能被正在跑的 LLM 调用阻塞：Python 用单把锁，状态查询会被长任务卡住。
func TestHealthIsNotBlockedByRunningEvent(t *testing.T) {
	gate := make(chan struct{})
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok", block: gate})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = post(t, server, "/event", map[string]any{
			"event_type": "web_message", "content": "慢请求",
		})
	}()

	// 等事件处理真正进入 LLM 调用。
	time.Sleep(50 * time.Millisecond)

	healthDone := make(chan int, 1)
	go func() {
		status, _ := get(t, server, "/health")
		healthDone <- status
	}()

	select {
	case status := <-healthDone:
		if status != http.StatusOK {
			t.Errorf("健康检查状态码 = %d", status)
		}
	case <-time.After(2 * time.Second):
		t.Error("健康检查被正在处理的事件阻塞了")
	}

	close(gate)
	<-done
}

func TestUnknownPathAndWrongMethod(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	if status, _ := get(t, server, "/nope"); status != http.StatusNotFound {
		t.Errorf("未知路径状态码 = %d，期望 404", status)
	}
	if status, _ := get(t, server, "/event"); status != http.StatusMethodNotAllowed {
		t.Errorf("方法不匹配状态码 = %d，期望 405", status)
	}
}

func TestMCPStatusWhenDisabled(t *testing.T) {
	server, _ := newTestServer(t, &scriptedChat{content: "FINAL ANSWER: ok"})

	status, payload := get(t, server, "/mcp/status")
	if status != http.StatusOK || payload["enabled"] != false {
		t.Errorf("MCP 状态 = %d %v", status, payload)
	}
	if status, _ := post(t, server, "/mcp/reload", nil); status != http.StatusBadRequest {
		t.Errorf("未启用 MCP 时重载状态码 = %d，期望 400", status)
	}
}
