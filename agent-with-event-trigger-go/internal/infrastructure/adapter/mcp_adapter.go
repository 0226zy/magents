package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// MCPServerSpec 声明一个要连接的 MCP 服务器。
type MCPServerSpec struct {
	Name    string
	Command string
	Args    []string
	Env     []string
}

type mcpToolBinding struct {
	server     string
	remoteName string
	definition entity.ToolDefinition
}

// MCPManager 管理多个 MCP stdio 服务器，实现 port.MCPPort。
//
// 工具名加 "<server>_" 前缀：OpenAI 兼容接口拒绝含点号的函数名，
// 而不加前缀时两个服务器的同名工具会互相覆盖。
type MCPManager struct {
	specs []MCPServerSpec
	log   port.Logger

	mu       sync.RWMutex
	sessions map[string]*mcpSession
	bindings map[string]mcpToolBinding
	order    []string
	loaded   bool
	loading  bool
}

// NewMCPManager 构造管理器，此时不连接任何服务器。
func NewMCPManager(specs []MCPServerSpec, log port.Logger) *MCPManager {
	if log == nil {
		log = NopLogger{}
	}
	return &MCPManager{
		specs:    specs,
		log:      log,
		sessions: map[string]*mcpSession{},
		bindings: map[string]mcpToolBinding{},
	}
}

// Load 连接全部服务器并发现工具，可重复调用即 reload。
// 逐个服务器回报成功与失败原因，不做静默降级。
func (m *MCPManager) Load(ctx context.Context) (port.MCPLoadReport, error) {
	m.mu.Lock()
	if m.loading {
		m.mu.Unlock()
		return port.MCPLoadReport{}, derr.ErrMCPLoading
	}
	m.loading = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.loading = false
		m.mu.Unlock()
	}()

	m.closeAllSessions()

	sessions := map[string]*mcpSession{}
	bindings := map[string]mcpToolBinding{}
	var order []string
	report := port.MCPLoadReport{}

	for _, spec := range m.specs {
		serverReport := port.MCPServerReport{Name: spec.Name}

		session, err := newMCPSession(ctx, spec.Command, spec.Args, spec.Env)
		if err != nil {
			serverReport.Error = err.Error()
			report.Servers = append(report.Servers, serverReport)
			m.log.Warn("MCP 服务器 %s 连接失败: %v", spec.Name, err)
			continue
		}

		tools, err := session.ListTools(ctx)
		if err != nil {
			session.Close()
			serverReport.Error = err.Error()
			report.Servers = append(report.Servers, serverReport)
			m.log.Warn("MCP 服务器 %s 列举工具失败: %v", spec.Name, err)
			continue
		}

		sessions[spec.Name] = session
		for _, tool := range tools {
			key := spec.Name + "_" + tool.Name
			bindings[key] = mcpToolBinding{
				server:     spec.Name,
				remoteName: tool.Name,
				definition: entity.ToolDefinition{
					Type: "function",
					Function: entity.FunctionSpec{
						Name:        key,
						Description: firstNonEmpty(tool.Description, tool.Name),
						Parameters:  decodeSchema(tool.InputSchema),
					},
				},
			}
			order = append(order, key)
		}

		serverReport.Loaded = true
		serverReport.ToolsCount = len(tools)
		report.Servers = append(report.Servers, serverReport)
		report.ToolsCount += len(tools)
		m.log.Info("MCP 服务器 %s 已加载 %d 个工具", spec.Name, len(tools))
	}

	sort.Strings(order)

	m.mu.Lock()
	m.sessions = sessions
	m.bindings = bindings
	m.order = order
	m.loaded = len(bindings) > 0
	m.mu.Unlock()

	return report, nil
}

// Loaded 报告是否至少有一个工具可用。
func (m *MCPManager) Loaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

// Loading 报告是否正在加载。
func (m *MCPManager) Loading() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loading
}

// ToolNames 返回全部工具名（已排序）。
func (m *MCPManager) ToolNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}

// Definitions 返回提供给模型的工具声明。
func (m *MCPManager) Definitions() []entity.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	defs := make([]entity.ToolDefinition, 0, len(m.order))
	for _, name := range m.order {
		defs = append(defs, m.bindings[name].definition)
	}
	return defs
}

// Has 报告是否提供该工具。
func (m *MCPManager) Has(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.bindings[name]
	return ok
}

// Execute 调用一个 MCP 工具；会话已死时先重连再调用。
func (m *MCPManager) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	m.mu.RLock()
	binding, ok := m.bindings[name]
	session := m.sessions[binding.server]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", derr.ErrUnknownTool, name)
	}

	if session == nil || !session.Alive() {
		reconnected, err := m.reconnect(ctx, binding.server)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		session = reconnected
	}

	text, isError, err := session.CallTool(ctx, binding.remoteName, args)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if isError {
		return map[string]any{"success": false, "error": text}, nil
	}
	return map[string]any{"success": true, "result": text}, nil
}

// reconnect 重建某个服务器的会话。工具绑定保持不变——服务器重启后工具集不变，
// 重新发现只会拖慢这次调用。
func (m *MCPManager) reconnect(ctx context.Context, serverName string) (*mcpSession, error) {
	var spec *MCPServerSpec
	for i := range m.specs {
		if m.specs[i].Name == serverName {
			spec = &m.specs[i]
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("未知的 MCP 服务器: %s", serverName)
	}

	session, err := newMCPSession(ctx, spec.Command, spec.Args, spec.Env)
	if err != nil {
		return nil, fmt.Errorf("重连 MCP 服务器 %s 失败: %w", serverName, err)
	}

	m.mu.Lock()
	if old := m.sessions[serverName]; old != nil {
		old.Close()
	}
	m.sessions[serverName] = session
	m.mu.Unlock()

	m.log.Info("MCP 服务器 %s 已重连", serverName)
	return session, nil
}

// Close 关闭全部会话。
func (m *MCPManager) Close() error {
	m.closeAllSessions()
	m.mu.Lock()
	m.bindings = map[string]mcpToolBinding{}
	m.order = nil
	m.loaded = false
	m.mu.Unlock()
	return nil
}

func (m *MCPManager) closeAllSessions() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[string]*mcpSession{}
	m.mu.Unlock()
	for _, session := range sessions {
		session.Close()
	}
}

// ToolsByServer 按服务器分组返回工具名，供 /mcp/status 使用。
func (m *MCPManager) ToolsByServer() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	grouped := map[string][]string{}
	for _, name := range m.order {
		server := m.bindings[name].server
		grouped[server] = append(grouped[server], name)
	}
	return grouped
}

// decodeSchema 还原 provider 给出的 JSON Schema；缺失或非法时退化成空对象 schema。
func decodeSchema(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return schema
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
