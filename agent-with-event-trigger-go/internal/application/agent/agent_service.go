// Package agent 实现事件驱动 Agent 的核心用例：收到一个事件，带着常驻会话历史
// 与 system hint 反复调用模型，执行模型请求的工具，直到模型给出最终回答。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// simulatedStepDelay 是 simulate_time_delay 模式下每轮推进的虚拟时长。
const simulatedStepDelay = 5 * time.Second

// Deps 是构造 EventAgentService 所需的依赖。
type Deps struct {
	Chat       port.ChatModelPort
	Builtin    port.ToolPort
	Workspace  port.WorkspacePort
	MCP        port.MCPPort        // 可为 nil
	Trajectory port.TrajectoryPort // 可为 nil
	Clock      port.Clock
	Logger     port.Logger
	Hints      entity.SystemHintConfig
	Options    entity.ChatOptions
	// SystemStateFn 渲染 system hint 的系统状态段。由组合根注入，
	// 让应用层不必知道 runtime / os 这些基础设施细节。
	SystemStateFn func(now, cwd string) string
	SystemPrompt  string
	MaxIterations int
}

// EventAgentService 是「处理一个事件」的应用服务。
type EventAgentService struct {
	chat       port.ChatModelPort
	builtin    port.ToolPort
	mcp        port.MCPPort
	workspace  port.WorkspacePort
	trajectory port.TrajectoryPort
	clock      port.Clock
	log        port.Logger
	hints      entity.SystemHintConfig
	opts       entity.ChatOptions
	systemFn   func(now, cwd string) string
	prompt     string
	maxIter    int

	// runMu 串行化整个事件处理（含全部 LLM 往返），持有时间是秒到分钟级。
	runMu sync.Mutex
	// stateMu 只保护下面的状态读写，持有时间是微秒级。
	// 分成两把锁，/agent/status 与 /health 才不会被正在跑的 LLM 调用阻塞。
	stateMu             sync.RWMutex
	history             []entity.Message
	todos               entity.TodoList
	counter             entity.ToolCounter
	records             []entity.ToolCallRecord
	processes           entity.ProcessRegistry
	lastUserInteraction time.Time
}

// New 构造事件 Agent 服务。
func New(deps Deps) *EventAgentService {
	if deps.Clock == nil {
		panic("agent: Clock 不能为空")
	}
	if deps.Logger == nil {
		panic("agent: Logger 不能为空")
	}
	if deps.MaxIterations <= 0 {
		deps.MaxIterations = 20
	}
	if deps.SystemStateFn == nil {
		deps.SystemStateFn = func(now, cwd string) string {
			return "Current Time: " + now + "\nCurrent Directory: " + cwd
		}
	}

	service := &EventAgentService{
		chat:       deps.Chat,
		builtin:    deps.Builtin,
		mcp:        deps.MCP,
		workspace:  deps.Workspace,
		trajectory: deps.Trajectory,
		clock:      deps.Clock,
		log:        deps.Logger,
		hints:      deps.Hints,
		opts:       deps.Options,
		systemFn:   deps.SystemStateFn,
		prompt:     deps.SystemPrompt,
		maxIter:    deps.MaxIterations,
	}
	service.resetStateLocked()
	return service
}

// Dispatch 让 Agent 满足 port.EventDispatcher，可直接挂到事件循环上。
func (s *EventAgentService) Dispatch(ctx context.Context, event entity.Event) error {
	_, err := s.HandleEvent(ctx, event)
	return err
}

// HandleEvent 处理一个事件：把它渲染成 user 消息，然后运行工具循环直到模型给出回答。
func (s *EventAgentService) HandleEvent(ctx context.Context, event entity.Event) (entity.EventResult, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.log.Info("收到事件 %s (%s): %s", event.ID, event.Type, event.Content)

	message := event.ToUserMessage()
	if s.hints.EnableTimestamps {
		message = "[" + s.timestamp() + "] " + message
	}

	s.stateMu.Lock()
	if event.Type.IsUserInteraction() {
		s.lastUserInteraction = s.clock.Now()
	}
	s.history = append(s.history, entity.Message{Role: "user", Content: message})
	baseRecords := len(s.records)
	s.stateMu.Unlock()

	var finalAnswer *string
	iteration := 0

	for iteration = 1; iteration <= s.maxIter; iteration++ {
		if s.hints.SimulateTimeDelay {
			s.clock.Advance(simulatedStepDelay)
		}
		s.saveTrajectory(iteration, nil)

		resp, err := s.chat.Chat(ctx, s.messagesForRequest(), s.activeTools(), s.opts)
		if err != nil {
			s.saveTrajectory(iteration, nil)
			return s.buildResult(event.ID, iteration, baseRecords, nil),
				fmt.Errorf("第 %d 轮调用模型失败: %w", iteration, err)
		}
		if len(resp.Choices) == 0 {
			s.saveTrajectory(iteration, nil)
			return s.buildResult(event.ID, iteration, baseRecords, nil),
				fmt.Errorf("第 %d 轮响应中没有 choices", iteration)
		}

		assistant := resp.Choices[0].Message
		s.stateMu.Lock()
		s.history = append(s.history, assistant.ToAssistantMessage())
		s.stateMu.Unlock()

		// 终止路径一：没有工具调用。纯文本回复即便不带 FINAL ANSWER: 也是终止信号，
		// 否则一句 "hi" 会被反复重发直到烧满 max_iterations。
		if len(assistant.ToolCalls) == 0 {
			if text := strings.TrimSpace(assistant.Content); text != "" {
				answer, _ := entity.ExtractFinalAnswer(text)
				finalAnswer = &answer
				s.log.Info("模型给出最终回答（无工具调用）")
			} else {
				s.log.Warn("模型返回空内容且无工具调用，提前结束以免空转")
			}
			break
		}

		s.runToolCalls(ctx, assistant.ToolCalls)

		// 终止路径二：工具调用与 FINAL ANSWER: 同轮出现（罕见但 Python 有这个分支）。
		if answer, marked := entity.ExtractFinalAnswer(assistant.Content); marked {
			finalAnswer = &answer
			s.log.Info("模型在工具调用的同一轮给出了最终回答")
			break
		}
	}

	if iteration > s.maxIter {
		iteration = s.maxIter
		s.log.Warn("达到 max_iterations=%d 上限，模型仍在请求工具调用", s.maxIter)
	}

	s.saveTrajectory(iteration, finalAnswer)
	return s.buildResult(event.ID, iteration, baseRecords, finalAnswer), nil
}

// messagesForRequest 返回本轮要发给模型的消息：历史的副本 + 临时 system hint。
//
// 必须是真拷贝。直接 append(s.history, hint) 在 cap 足够时会写进 history 的底层数组，
// 下一轮再 append 真实消息时就把 hint 的位置覆盖掉了——历史会悄悄少一条。
func (s *EventAgentService) messagesForRequest() []entity.Message {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	messages := make([]entity.Message, len(s.history), len(s.history)+1)
	copy(messages, s.history)
	if hint := s.systemHintLocked(); hint != "" {
		messages = append(messages, entity.Message{Role: "user", Content: hint})
	}
	return messages
}

// runToolCalls 依次执行模型请求的工具，并把结果回填到历史。
func (s *EventAgentService) runToolCalls(ctx context.Context, calls []entity.ToolCall) {
	for _, call := range calls {
		name := call.Function.Name
		raw := call.Function.Arguments
		if strings.TrimSpace(raw) == "" {
			raw = "{}"
		}

		var args map[string]any
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			// 参数坏掉也必须回填一条 tool 消息：带 tool_calls 的 assistant 消息
			// 已经进了历史，缺了对应的 tool 响应，之后每次请求都会被 provider 拒绝。
			detail := fmt.Sprintf("Invalid tool arguments (not valid JSON): %v. Raw arguments: %s",
				err, truncate(raw, 500))
			s.log.Warn("工具 %s 参数解析失败: %s", name, detail)
			s.recordAndReply(call.ID, name, nil, nil, detail, 1, 0)
			continue
		}

		s.stateMu.Lock()
		callNumber := s.counter.Next(name, s.hints.EnableToolCounter)
		s.stateMu.Unlock()

		s.log.Info("执行工具 %s (第 %d 次)", name, callNumber)
		started := time.Now()
		result, err := s.executeTool(ctx, name, args)
		duration := time.Since(started).Milliseconds()

		errText := ""
		if err != nil {
			errText = s.describeToolError(err, name, args)
			s.log.Warn("工具 %s 失败: %s", name, truncate(oneLine(errText), 150))
			result = nil
		}
		s.recordAndReply(call.ID, name, args, result, errText, callNumber, duration)
	}
}

// recordAndReply 留痕一次工具调用并把结果写回历史。
func (s *EventAgentService) recordAndReply(toolCallID, name string, args map[string]any,
	result any, errText string, callNumber int, durationMs int64) {

	payload := result
	if errText != "" {
		payload = map[string]any{"error": errText}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte(fmt.Sprintf("%q", fmt.Sprint(payload)))
	}

	content := entity.ToolResultPrefix(s.timestamp(), callNumber, name, s.hints) + string(encoded)

	s.stateMu.Lock()
	s.records = append(s.records, entity.ToolCallRecord{
		ToolName:   name,
		Arguments:  args,
		Result:     result,
		Error:      errText,
		CallNumber: callNumber,
		Timestamp:  s.clock.Now().Format(time.RFC3339),
		DurationMs: durationMs,
	})
	s.history = append(s.history, entity.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		Content:    content,
	})
	s.stateMu.Unlock()
}

// executeTool 把工具调用分派到 TODO 工具、MCP 工具或内置工具。
func (s *EventAgentService) executeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case toolRewriteTodoList:
		return s.rewriteTodoList(args)
	case toolUpdateTodoStatus:
		return s.updateTodoStatus(args)
	}
	if s.mcp != nil && s.mcp.Has(name) {
		return s.mcp.Execute(ctx, name, args)
	}
	if s.builtin != nil && s.builtin.Has(name) {
		return s.builtin.Execute(ctx, name, args)
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

// activeTools 返回本轮提供给模型的工具声明。
// MCP 加载成功后用 MCP 工具，否则用内置工具；TODO 工具两种情况下都追加。
func (s *EventAgentService) activeTools() []entity.ToolDefinition {
	var defs []entity.ToolDefinition
	if s.mcp != nil && s.mcp.Loaded() {
		defs = append(defs, s.mcp.Definitions()...)
	} else if s.builtin != nil {
		defs = append(defs, s.builtin.Definitions()...)
	}
	if s.hints.EnableTodoList {
		defs = append(defs, todoToolDefinitions()...)
	}
	return defs
}

// systemHintLocked 渲染 system hint，调用方须持有 stateMu 的读锁。
func (s *EventAgentService) systemHintLocked() string {
	var sections []string
	if s.hints.EnableSystemState {
		sections = append(sections, "=== SYSTEM STATE ===\n"+
			s.systemFn(s.timestamp(), s.currentDirectory())+"\n")
	}
	if s.hints.EnableTodoList && s.todos.Len() > 0 {
		sections = append(sections, "=== CURRENT TASKS ===\n"+s.todos.Format()+"\n")
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n")
}

func (s *EventAgentService) currentDirectory() string {
	if s.workspace == nil {
		return ""
	}
	return s.workspace.CurrentDirectory()
}

func (s *EventAgentService) timestamp() string {
	return s.clock.Now().Format(s.hints.Layout())
}

func (s *EventAgentService) buildResult(eventID string, iterations, baseRecords int,
	finalAnswer *string) entity.EventResult {

	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	trajectoryPath := ""
	if s.trajectory != nil && s.hints.SaveTrajectory {
		trajectoryPath = s.trajectory.Path()
	}
	return entity.EventResult{
		EventID:        eventID,
		FinalAnswer:    finalAnswer,
		Iterations:     iterations,
		ToolCallsCount: len(s.records) - baseRecords,
		ToolCallsTotal: len(s.records),
		TodoItems:      s.todos.Len(),
		Success:        finalAnswer != nil,
		TrajectoryFile: trajectoryPath,
	}
}

func (s *EventAgentService) saveTrajectory(iteration int, finalAnswer *string) {
	if s.trajectory == nil || !s.hints.SaveTrajectory {
		return
	}

	s.stateMu.RLock()
	snapshot := entity.TrajectorySnapshot{
		Timestamp:           s.clock.Now().Format(time.RFC3339),
		Iteration:           iteration,
		Provider:            s.chat.Provider(),
		Model:               s.chat.Model(),
		ConversationHistory: append([]entity.Message(nil), s.history...),
		ToolCalls:           append([]entity.ToolCallRecord(nil), s.records...),
		TodoList:            s.todos.Items(),
		CurrentDirectory:    s.currentDirectory(),
		FinalAnswer:         finalAnswer,
		BackgroundProcesses: s.processes.Snapshot(),
		Config:              s.hints,
	}
	s.stateMu.RUnlock()

	if err := s.trajectory.Save(snapshot); err != nil {
		s.log.Warn("保存轨迹失败: %v", err)
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func oneLine(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}
