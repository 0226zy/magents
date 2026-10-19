package agent

import (
	"context"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// Reset 清空会话历史、TODO、计数与进程表。
// 同时持有 runMu：否则会在事件处理到一半时抽掉历史。
func (s *EventAgentService) Reset() {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.stateMu.Lock()
	s.resetStateLocked()
	s.stateMu.Unlock()

	if s.workspace != nil {
		s.workspace.Reset()
	}
	s.log.Info("Agent 状态已重置")
}

func (s *EventAgentService) resetStateLocked() {
	s.history = []entity.Message{{Role: "system", Content: s.prompt}}
	s.todos.Clear()
	s.counter.Reset()
	s.records = nil
	s.processes.Clear()
	s.lastUserInteraction = s.clock.Now()
}

// Status 返回运行时状态。只取 stateMu，不会被正在进行的事件处理阻塞。
func (s *EventAgentService) Status() entity.AgentStatus {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	status := entity.AgentStatus{
		Provider:         s.chat.Provider(),
		Model:            s.chat.Model(),
		CurrentDirectory: s.currentDirectory(),
		ToolCallsCount:   len(s.records),
		TodoItems:        s.todos.Len(),
		HistoryMessages:  len(s.history),
		Processes:        s.processes.Len(),
	}
	if s.mcp != nil {
		status.MCPToolsLoaded = s.mcp.Loaded()
		status.MCPToolsCount = len(s.mcp.ToolNames())
	}
	return status
}

// LoadMCP 加载（或重新加载）MCP 工具。
func (s *EventAgentService) LoadMCP(ctx context.Context) (port.MCPLoadReport, error) {
	if s.mcp == nil {
		return port.MCPLoadReport{}, nil
	}
	return s.mcp.Load(ctx)
}

// MCP 暴露 MCP 端口，供 HTTP 层查询状态；未启用时为 nil。
func (s *EventAgentService) MCP() port.MCPPort { return s.mcp }

// RegisterProcess 登记一个后台进程供超时监控。
func (s *EventAgentService) RegisterProcess(id, name string, metadata map[string]any) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.processes.Register(entity.BackgroundProcess{
		ID:        id,
		Name:      name,
		StartTime: s.clock.Now(),
		Metadata:  metadata,
	})
}

// UnregisterProcess 注销进程，返回它此前是否存在。
func (s *EventAgentService) UnregisterProcess(id string) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.processes.Unregister(id)
}

// LastUserInteraction 返回最近一次用户交互时刻，供超时监控判断空闲时长。
func (s *EventAgentService) LastUserInteraction() time.Time {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.lastUserInteraction
}

// OverdueProcesses 返回超时且尚未提醒过的进程。
func (s *EventAgentService) OverdueProcesses(now time.Time, threshold time.Duration) []entity.BackgroundProcess {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.processes.Overdue(now, threshold)
}

// MarkProcessReminded 标记某进程已提醒，保证每个进程只提醒一次。
func (s *EventAgentService) MarkProcessReminded(id string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.processes.MarkReminded(id)
}

// History 返回会话历史的副本，供测试与调试使用。
func (s *EventAgentService) History() []entity.Message {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	out := make([]entity.Message, len(s.history))
	copy(out, s.history)
	return out
}

// Records 返回工具调用留痕的副本。
func (s *EventAgentService) Records() []entity.ToolCallRecord {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	out := make([]entity.ToolCallRecord, len(s.records))
	copy(out, s.records)
	return out
}

// Todos 返回 TODO 条目的副本。
func (s *EventAgentService) Todos() []entity.TodoItem {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.todos.Items()
}
