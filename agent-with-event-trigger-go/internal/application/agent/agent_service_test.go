package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// fakeChat 按脚本逐轮返回预设响应，并记录每轮收到的消息。
type fakeChat struct {
	mu        sync.Mutex
	responses []entity.ChoiceMessage
	seen      [][]entity.Message
	calls     int
}

func (f *fakeChat) Chat(_ context.Context, messages []entity.Message, _ []entity.ToolDefinition,
	_ entity.ChatOptions) (*entity.ChatResponse, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	snapshot := make([]entity.Message, len(messages))
	copy(snapshot, messages)
	f.seen = append(f.seen, snapshot)

	index := min(f.calls, len(f.responses)-1)
	f.calls++
	return &entity.ChatResponse{Choices: []entity.Choice{{Message: f.responses[index]}}}, nil
}

func (f *fakeChat) Model() string    { return "fake-model" }
func (f *fakeChat) Provider() string { return "fake" }

func (f *fakeChat) lastMessages() []entity.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[len(f.seen)-1]
}

// fakeTools 是一个只认识 echo 的工具集。
type fakeTools struct {
	mu       sync.Mutex
	executed []string
	fail     bool
}

func (f *fakeTools) Definitions() []entity.ToolDefinition {
	return []entity.ToolDefinition{{
		Type:     "function",
		Function: entity.FunctionSpec{Name: "echo", Description: "回显输入"},
	}}
}

func (f *fakeTools) Has(name string) bool { return name == "echo" }

func (f *fakeTools) Execute(_ context.Context, name string, args map[string]any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executed = append(f.executed, name)
	if f.fail {
		return nil, errFakeTool
	}
	return map[string]any{"echoed": args["text"]}, nil
}

func (f *fakeTools) CurrentDirectory() string { return "/tmp/fake" }
func (f *fakeTools) Reset()                   {}

type fakeToolError struct{}

func (fakeToolError) Error() string { return "permission denied" }

var errFakeTool = fakeToolError{}

// stubClock 是可控时钟。
type stubClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stubClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stubClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type silentLogger struct{}

func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}

func toolCallMessage(id, name, args string) entity.ChoiceMessage {
	return entity.ChoiceMessage{
		Role: "assistant",
		ToolCalls: []entity.ToolCall{{
			ID: id, Type: "function",
			Function: entity.FunctionCall{Name: name, Arguments: args},
		}},
	}
}

func newTestService(t *testing.T, chat port.ChatModelPort, tools *fakeTools) *EventAgentService {
	t.Helper()
	hints := entity.DefaultSystemHintConfig()
	hints.SaveTrajectory = false

	return New(Deps{
		Chat:          chat,
		Builtin:       tools,
		Workspace:     tools,
		Clock:         &stubClock{now: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)},
		Logger:        silentLogger{},
		Hints:         hints,
		Options:       entity.ChatOptions{Temperature: 0.7, MaxTokens: 1024, ToolChoice: "auto"},
		SystemPrompt:  "你是测试用 Agent。",
		MaxIterations: 5,
	})
}

func TestHandleEventRunsToolLoopThenAnswers(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		toolCallMessage("call-1", "echo", `{"text":"hello"}`),
		{Role: "assistant", Content: "FINAL ANSWER: 已回显 hello"},
	}}
	tools := &fakeTools{}
	service := newTestService(t, chat, tools)

	result, err := service.HandleEvent(context.Background(),
		entity.NewEvent(entity.EventWebMessage, "回显 hello", nil, time.Now()))
	if err != nil {
		t.Fatalf("HandleEvent 返回错误: %v", err)
	}

	if result.FinalAnswer == nil || *result.FinalAnswer != "已回显 hello" {
		t.Fatalf("最终回答 = %v，期望剥掉 FINAL ANSWER: 前缀", result.FinalAnswer)
	}
	if result.Iterations != 2 {
		t.Errorf("轮次 = %d，期望 2", result.Iterations)
	}
	if result.ToolCallsCount != 1 {
		t.Errorf("本次工具调用数 = %d，期望 1", result.ToolCallsCount)
	}
	if len(tools.executed) != 1 || tools.executed[0] != "echo" {
		t.Errorf("实际执行的工具 = %v", tools.executed)
	}
}

// 纯文本回复即便不带 FINAL ANSWER: 也要终止，否则一句 "hi" 会烧满 max_iterations。
func TestHandleEventStopsOnPlainTextReply(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		{Role: "assistant", Content: "你好"},
	}}
	service := newTestService(t, chat, &fakeTools{})

	result, err := service.HandleEvent(context.Background(),
		entity.NewEvent(entity.EventIMMessage, "hi", nil, time.Now()))
	if err != nil {
		t.Fatalf("HandleEvent 返回错误: %v", err)
	}
	if result.Iterations != 1 {
		t.Errorf("轮次 = %d，期望 1", result.Iterations)
	}
	if result.FinalAnswer == nil || *result.FinalAnswer != "你好" {
		t.Errorf("最终回答 = %v", result.FinalAnswer)
	}
}

// system hint 只能出现在发往模型的消息副本里，不能污染常驻历史。
func TestSystemHintIsTransient(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		{Role: "assistant", Content: "FINAL ANSWER: 好的"},
	}}
	service := newTestService(t, chat, &fakeTools{})

	if _, err := service.HandleEvent(context.Background(),
		entity.NewEvent(entity.EventWebMessage, "你好", nil, time.Now())); err != nil {
		t.Fatal(err)
	}

	sent := chat.lastMessages()
	hint := sent[len(sent)-1].Content
	if !strings.Contains(hint, "SYSTEM STATE") {
		t.Fatalf("最后一条应是 system hint，实际 = %q", hint)
	}
	for _, message := range service.History() {
		if message.Content == hint {
			t.Fatal("system hint 不应进入常驻历史")
		}
	}
}

// 参数解析失败也必须回填 tool 消息：缺了对应响应的 tool_call_id
// 会让之后每次请求都被 provider 拒绝。
func TestInvalidToolArgumentsStillProduceToolReply(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		toolCallMessage("call-bad", "echo", `{"text": broken`),
		{Role: "assistant", Content: "FINAL ANSWER: 已处理"},
	}}
	tools := &fakeTools{}
	service := newTestService(t, chat, tools)

	if _, err := service.HandleEvent(context.Background(),
		entity.NewEvent(entity.EventWebMessage, "触发坏参数", nil, time.Now())); err != nil {
		t.Fatal(err)
	}

	if len(tools.executed) != 0 {
		t.Errorf("参数非法时不应真的执行工具，实际 = %v", tools.executed)
	}
	var replied bool
	for _, message := range service.History() {
		if message.Role == "tool" && message.ToolCallID == "call-bad" {
			replied = true
			if !strings.Contains(message.Content, "Invalid tool arguments") {
				t.Errorf("tool 响应未说明失败原因: %q", message.Content)
			}
		}
	}
	if !replied {
		t.Error("每个 tool_call_id 都必须有对应的 tool 响应")
	}
}

// 达到上限时不应报错，而是返回没有最终回答的结果。
func TestHandleEventStopsAtMaxIterations(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		toolCallMessage("call-loop", "echo", `{"text":"again"}`),
	}}
	service := newTestService(t, chat, &fakeTools{})

	result, err := service.HandleEvent(context.Background(),
		entity.NewEvent(entity.EventWebMessage, "无限循环", nil, time.Now()))
	if err != nil {
		t.Fatalf("达到上限不应返回错误: %v", err)
	}
	if result.Iterations != 5 {
		t.Errorf("轮次 = %d，期望等于 max_iterations=5", result.Iterations)
	}
	if result.FinalAnswer != nil {
		t.Errorf("达到上限时不应有最终回答，实际 = %v", *result.FinalAnswer)
	}
}

// Python 版把累计工具调用数当成单次值返回，客户端看到的数字会越滚越大。
func TestToolCallsCountIsPerEventWhileTotalAccumulates(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		toolCallMessage("call-1", "echo", `{"text":"a"}`),
		{Role: "assistant", Content: "FINAL ANSWER: 完成"},
	}}
	service := newTestService(t, chat, &fakeTools{})

	for range 2 {
		chat.mu.Lock()
		chat.calls = 0
		chat.mu.Unlock()

		result, err := service.HandleEvent(context.Background(),
			entity.NewEvent(entity.EventWebMessage, "再来一次", nil, time.Now()))
		if err != nil {
			t.Fatal(err)
		}
		if result.ToolCallsCount != 1 {
			t.Errorf("单次工具调用数 = %d，期望恒为 1", result.ToolCallsCount)
		}
	}
	if total := len(service.Records()); total != 2 {
		t.Errorf("累计留痕 = %d 条，期望 2", total)
	}
}

// 工具失败要被记录成 error 而不是抛给调用方，模型需要看到失败原因才能自我修正。
func TestToolFailureIsReportedToModel(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		toolCallMessage("call-fail", "echo", `{"text":"x"}`),
		{Role: "assistant", Content: "FINAL ANSWER: 已知悉失败"},
	}}
	service := newTestService(t, chat, &fakeTools{fail: true})

	if _, err := service.HandleEvent(context.Background(),
		entity.NewEvent(entity.EventWebMessage, "触发失败", nil, time.Now())); err != nil {
		t.Fatal(err)
	}

	records := service.Records()
	if len(records) != 1 || records[0].Error == "" {
		t.Fatalf("工具失败应被留痕，实际 = %+v", records)
	}
	if records[0].DurationMs < 0 {
		t.Errorf("耗时字段应被填写，实际 = %d", records[0].DurationMs)
	}

	var toolReply string
	for _, message := range service.History() {
		if message.Role == "tool" {
			toolReply = message.Content
		}
	}
	var payload map[string]any
	body := toolReply[strings.Index(toolReply, "{"):]
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("tool 响应不是合法 JSON: %q", toolReply)
	}
	if _, ok := payload["error"]; !ok {
		t.Errorf("tool 响应应含 error 字段: %v", payload)
	}
}

func TestResetClearsConversationState(t *testing.T) {
	chat := &fakeChat{responses: []entity.ChoiceMessage{
		{Role: "assistant", Content: "FINAL ANSWER: 好"},
	}}
	service := newTestService(t, chat, &fakeTools{})

	if _, err := service.HandleEvent(context.Background(),
		entity.NewEvent(entity.EventWebMessage, "你好", nil, time.Now())); err != nil {
		t.Fatal(err)
	}
	service.Reset()

	if got := len(service.History()); got != 1 {
		t.Errorf("重置后历史应只剩 system prompt，实际 %d 条", got)
	}
	if status := service.Status(); status.ToolCallsCount != 0 || status.TodoItems != 0 {
		t.Errorf("重置后计数未归零: %+v", status)
	}
}
