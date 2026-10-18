package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0226zy/magents/dsh-go/internal/application/agent"
	"github.com/0226zy/magents/dsh-go/internal/application/project"
	"github.com/0226zy/magents/dsh-go/internal/application/session"
	"github.com/0226zy/magents/dsh-go/internal/domain/entity"
	"github.com/0226zy/magents/dsh-go/internal/domain/plugin"
	"github.com/0226zy/magents/dsh-go/internal/domain/port"
	"github.com/0226zy/magents/dsh-go/internal/infrastructure/adapter/llminfo"
	"github.com/0226zy/magents/dsh-go/internal/infrastructure/adapter/policy"
	"github.com/0226zy/magents/dsh-go/internal/infrastructure/adapter/storejsonl"
)

// ── 测试桩 ──────────────────────────────────────────────────────

// echoLLM 返回固定文本（无工具调用，回合一轮结束）。
type echoLLM struct{}

func (echoLLM) Chat(context.Context, []entity.Message, []entity.ToolDefinition) (*entity.ChatResponse, error) {
	return textResp("已完成。"), nil
}
func (echoLLM) Model() string { return "echo" }

// scriptLLM：第一轮请求 touch_file 工具，之后纯文本收尾（审批流测试用）。
type scriptLLM struct {
	mu    sync.Mutex
	calls int
}

func (l *scriptLLM) Chat(context.Context, []entity.Message, []entity.ToolDefinition) (*entity.ChatResponse, error) {
	l.mu.Lock()
	l.calls++
	n := l.calls
	l.mu.Unlock()
	if n == 1 {
		var resp entity.ChatResponse
		resp.Choices = append(resp.Choices, struct {
			Message      entity.Message `json:"message"`
			FinishReason string         `json:"finish_reason"`
		}{Message: entity.Message{Role: "assistant", ToolCalls: []entity.ToolCall{{
			ID: "c1", Type: "function",
			Function: entity.FunctionCall{Name: "touch_file", Arguments: "{}"},
		}}}, FinishReason: "tool_calls"})
		resp.Usage.TotalTokens = 10
		return &resp, nil
	}
	return textResp("收尾完成。"), nil
}
func (l *scriptLLM) Model() string { return "script" }

func textResp(text string) *entity.ChatResponse {
	var resp entity.ChatResponse
	resp.Choices = append(resp.Choices, struct {
		Message      entity.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	}{Message: entity.Message{Role: "assistant", Content: text}, FinishReason: "stop"})
	resp.Usage.TotalTokens = 10
	return &resp
}

// fakeTool 是非只读测试工具（会触发审批管道）。
type fakeTool struct{}

func (fakeTool) Definition() entity.ToolDefinition {
	return entity.ToolDefinition{Type: "function", Function: entity.FunctionSpec{
		Name:        "touch_file",
		Description: "测试工具：标记一次写入",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}
func (fakeTool) ConcurrencySafe() bool { return false }
func (fakeTool) Execute(context.Context, string, *port.ToolContext) entity.ToolResult {
	return entity.ToolResult{Content: "touched"}
}

// newTestServer 装配一个真实 httpapi 服务端（真 Manager / 真 policy 钩子 / 假 LLM）。
func newTestServer(t *testing.T, llm port.LLMPort, token string) (*httptest.Server, *session.Manager) {
	t.Helper()
	registry := plugin.NewRegistry()
	if err := registry.RegisterTool(fakeTool{}); err != nil {
		t.Fatal(err)
	}
	// hook 先于 loop 注册（hooks 快照在 NewLoop 时传入）；ModeOf 回调后绑定 manager。
	var manager *session.Manager
	registry.RegisterHook(policy.NewHook(func(id string) string {
		if manager == nil {
			return ""
		}
		return manager.ModeOf(id)
	}))
	resolver := func(string) (port.LLMPort, error) { return llm, nil }
	loop := agent.NewLoop(resolver, registry, registry.Hooks(), nil, "系统提示词",
		agent.Limits{MaxIterations: 5, TokenBudget: 10000, LivelockRepeat: 3, MaxRetries: 1, CircuitBreakerFailures: 3},
		func(error) (bool, time.Duration) { return false, 0 })
	store, err := storejsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager = session.NewManager(store, loop, nil, t.TempDir(), 3*time.Second)
	models, err := llminfo.NewFileRepo(filepath.Join(t.TempDir(), "models.yaml"), []entity.LLMInfo{
		{Name: "echo", Provider: "openai-compat", BaseURL: "http://127.0.0.1", Model: "echo", APIKey: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	orch := project.NewOrchestrator(manager, 5*time.Second)
	srv := httptest.NewServer(NewServer(manager, registry, models, orch, token, http.NotFoundHandler()).Handler())
	t.Cleanup(srv.Close)
	return srv, manager
}

// ── HTTP 辅助 ──────────────────────────────────────────────────

func doReq(t *testing.T, method, url, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

type evJSON struct {
	Seq    int    `json:"seq"`
	Kind   string `json:"kind"`
	Note   string `json:"note"`
	Marker *struct {
		Type   string `json:"type"`
		Detail string `json:"detail"`
	} `json:"marker"`
	Approval *struct {
		ID       string `json:"id"`
		By       string `json:"by"`
		Approved bool   `json:"approved"`
	} `json:"approval"`
	Message *struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

// waitEvents 轮询 events API，直到出现满足 want 的事件或超时，返回全部事件。
func waitEvents(t *testing.T, base, token, sessionID string, want func(evJSON) bool) []evJSON {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_, body := doReq(t, "GET", fmt.Sprintf("%s/v1/sessions/%s/events", base, sessionID), token, nil)
		raw, _ := json.Marshal(body["events"])
		var events []evJSON
		_ = json.Unmarshal(raw, &events)
		for _, ev := range events {
			if want(ev) {
				return events
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("等待事件超时")
	return nil
}

func turnDone(ev evJSON) bool { return ev.Marker != nil && ev.Marker.Type == "turn_done" }

// ── 测试 ──────────────────────────────────────────────────────

// 鉴权：/v1/* 需 Bearer；healthz 与非 /v1 路径放行。
func TestAuth(t *testing.T) {
	srv, _ := newTestServer(t, echoLLM{}, "secret")

	if code, _ := doReq(t, "GET", srv.URL+"/v1/sessions", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，got %d", code)
	}
	if code, _ := doReq(t, "GET", srv.URL+"/v1/sessions", "wrong", nil); code != http.StatusUnauthorized {
		t.Fatalf("错误 token 应 401，got %d", code)
	}
	if code, _ := doReq(t, "GET", srv.URL+"/v1/sessions", "secret", nil); code != http.StatusOK {
		t.Fatalf("正确 token 应 200，got %d", code)
	}
	if code, _ := doReq(t, "GET", srv.URL+"/healthz", "", nil); code != http.StatusOK {
		t.Fatalf("healthz 免鉴权，got %d", code)
	}
	if code, _ := doReq(t, "GET", srv.URL+"/", "", nil); code != http.StatusNotFound {
		// webUI 占位 handler 是 NotFound：重点是不被 401 拦截
		t.Fatalf("非 /v1 路径放行，got %d", code)
	}
}

// 空 token 服务端（本机开发）：全部放行。
func TestNoAuthWhenTokenEmpty(t *testing.T) {
	srv, _ := newTestServer(t, echoLLM{}, "")
	if code, _ := doReq(t, "GET", srv.URL+"/v1/sessions", "", nil); code != http.StatusOK {
		t.Fatalf("空 token 服务端应放行，got %d", code)
	}
}

// 会话创建 + 202 异步回合 + 事件回放。
func TestSessionTurn202(t *testing.T) {
	srv, _ := newTestServer(t, echoLLM{}, "secret")

	code, body := doReq(t, "POST", srv.URL+"/v1/sessions", "secret",
		map[string]any{"cwd": t.TempDir(), "mode": "auto-edit"})
	if code != http.StatusCreated {
		t.Fatalf("创建会话: %d %v", code, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("响应缺 id: %v", body)
	}

	code, body = doReq(t, "POST", srv.URL+"/v1/sessions/"+id+"/messages", "secret",
		map[string]any{"content": "hi"})
	if code != http.StatusAccepted {
		t.Fatalf("发起回合: %d %v", code, body)
	}

	events := waitEvents(t, srv.URL, "secret", id, turnDone)
	var hasAssistant bool
	for _, ev := range events {
		if ev.Message != nil && ev.Message.Role == "assistant" && ev.Message.Content == "已完成。" {
			hasAssistant = true
		}
	}
	if !hasAssistant {
		t.Fatal("回合结束后应能回放到 assistant 消息")
	}
}

// 审批流：suggest 模式下工具调用产生 pending 审批 → 拒绝 → 错误回注 → 回合收尾。
func TestApprovalFlow(t *testing.T) {
	srv, _ := newTestServer(t, &scriptLLM{}, "secret")

	_, body := doReq(t, "POST", srv.URL+"/v1/sessions", "secret",
		map[string]any{"cwd": t.TempDir(), "mode": "suggest"})
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("创建会话失败: %v", body)
	}
	if code, resp := doReq(t, "POST", srv.URL+"/v1/sessions/"+id+"/messages", "secret",
		map[string]any{"content": "go"}); code != http.StatusAccepted {
		t.Fatalf("发起回合: %d %v", code, resp)
	}

	// 等 pending 审批事件
	events := waitEvents(t, srv.URL, "secret", id, func(ev evJSON) bool {
		return ev.Kind == "approval" && ev.Approval != nil && ev.Approval.By == "pending"
	})
	var apprID string
	for _, ev := range events {
		if ev.Kind == "approval" && ev.Approval != nil && ev.Approval.By == "pending" {
			apprID = ev.Approval.ID
		}
	}
	if apprID == "" {
		t.Fatal("未找到 pending 审批 ID")
	}

	// 拒绝审批 → 工具结果被拦截回注 → 脚本 LLM 第二轮纯文本收尾
	if code, resp := doReq(t, "POST", srv.URL+"/v1/sessions/"+id+"/approvals/"+apprID, "secret",
		map[string]any{"approved": false}); code != http.StatusOK {
		t.Fatalf("裁决审批: %d %v", code, resp)
	}
	events = waitEvents(t, srv.URL, "secret", id, turnDone)
	var rejected bool
	for _, ev := range events {
		if ev.Message != nil && ev.Message.Role == "tool" && strings.Contains(ev.Message.Content, "操作被拒绝") {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("拒绝后应回注『操作被拒绝』的工具结果")
	}
}

// ticket：一次性短期凭证，供 EventSource 连接 SSE；消费即焚。
func TestTicketFlow(t *testing.T) {
	srv, _ := newTestServer(t, echoLLM{}, "secret")

	if code, _ := doReq(t, "POST", srv.URL+"/v1/tickets", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("签发 ticket 需鉴权，got %d", code)
	}
	code, body := doReq(t, "POST", srv.URL+"/v1/tickets", "secret", nil)
	if code != http.StatusCreated {
		t.Fatalf("签发 ticket: %d %v", code, body)
	}
	ticket, _ := body["ticket"].(string)
	if ticket == "" {
		t.Fatalf("响应缺 ticket: %v", body)
	}

	// 用 ticket 连 stream（无 Authorization 头）
	_, sess := doReq(t, "POST", srv.URL+"/v1/sessions", "secret", map[string]any{"cwd": t.TempDir()})
	id, _ := sess["id"].(string)
	resp, err := http.Get(fmt.Sprintf("%s/v1/sessions/%s/stream?ticket=%s", srv.URL, id, ticket))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ticket 连接 stream: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 同一张 ticket 第二次使用：已焚毁
	resp2, err := http.Get(fmt.Sprintf("%s/v1/sessions/%s/stream?ticket=%s", srv.URL, id, ticket))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ticket 应一次性，第二次使用应 401，got %d", resp2.StatusCode)
	}
}

// SSE 游标：from_seq 补发历史；事件带 id 行；Last-Event-ID 只发之后的事件。
func TestStreamBackfill(t *testing.T) {
	srv, _ := newTestServer(t, echoLLM{}, "secret")

	_, sess := doReq(t, "POST", srv.URL+"/v1/sessions", "secret", map[string]any{"cwd": t.TempDir()})
	id, _ := sess["id"].(string)
	doReq(t, "POST", srv.URL+"/v1/sessions/"+id+"/messages", "secret", map[string]any{"content": "hi"})
	events := waitEvents(t, srv.URL, "secret", id, turnDone)
	lastSeq := events[len(events)-1].Seq

	// from_seq=0：补发全部历史（回合已结束，读到的都是补发）
	lines := readSSEUntil(t, srv, id, "from_seq=0", "", turnDoneLine)
	if !containsLine(lines, ": connected") {
		t.Fatalf("缺少连接注释行: %v", lines)
	}
	if !containsLine(lines, "event: marker") || !containsPrefix(lines, "id: ") {
		t.Fatalf("补发应含 marker 事件与 id 行: %v", lines)
	}

	// Last-Event-ID=lastSeq：不再有任何事件数据（只有连接注释）
	lines = readSSEUntil(t, srv, id, "", fmt.Sprint(lastSeq), nil)
	for _, ln := range lines {
		if strings.HasPrefix(ln, "event:") {
			t.Fatalf("Last-Event-ID 之后不应再有补发事件，got %q", ln)
		}
	}
}

// 读 SSE 流：until 为 nil 时读到连接注释后等一小段静默期即返回。
func readSSEUntil(t *testing.T, srv *httptest.Server, sessionID, query, lastEventID string, until func(line string) bool) []string {
	t.Helper()
	url := fmt.Sprintf("%s/v1/sessions/%s/stream?%s", srv.URL, sessionID, query)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %d", resp.StatusCode)
	}

	var lines []string
	scanner := bufio.NewScanner(resp.Body)
	type res struct{ line string }
	ch := make(chan res)
	go func() {
		for scanner.Scan() {
			ch <- res{scanner.Text()}
		}
		close(ch)
	}()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case r, ok := <-ch:
			if !ok {
				return lines
			}
			lines = append(lines, r.line)
			if until != nil && until(r.line) {
				return lines
			}
		case <-deadline:
			return lines
		}
	}
}

func turnDoneLine(line string) bool {
	return strings.Contains(line, `"type":"turn_done"`)
}

func containsLine(lines []string, s string) bool {
	for _, l := range lines {
		if l == s {
			return true
		}
	}
	return false
}

func containsPrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}
