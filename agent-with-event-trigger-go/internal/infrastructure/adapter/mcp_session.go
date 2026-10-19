package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// mcpProtocolVersion 是本客户端声明支持的 MCP 协议版本。
const mcpProtocolVersion = "2024-11-05"

// mcpRequestTimeout 是单次 JSON-RPC 往返的上限。
const mcpRequestTimeout = 60 * time.Second

// jsonRPCRequest 是一条 JSON-RPC 2.0 请求。
// ID 为 nil 时是通知（notification），不等待响应。
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC 错误 %d: %s", e.Code, e.Message)
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonRPCError   `json:"error"`
}

// MCPTool 是 MCP 服务器声明的一个工具。
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolsCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

// mcpSession 是一个常驻的 MCP stdio 子进程会话。
//
// Python 版每次工具调用都重新 spawn 一个解释器进程；这里保持会话常驻，
// 进程意外退出后由 MCPManager 懒重连。
type mcpSession struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	nextID  int64
	closed  bool
	command string
	args    []string
}

func newMCPSession(ctx context.Context, command string, args, env []string) (*mcpSession, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = io.Discard // 服务器的日志走 stderr，不混进我们的终端输出

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 MCP 服务器 %s: %w", command, err)
	}

	session := &mcpSession{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 1<<20),
		command: command,
		args:    args,
	}
	if err := session.handshake(ctx); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

// handshake 完成 initialize 请求与 notifications/initialized 通知。
func (s *mcpSession) handshake(ctx context.Context) error {
	_, err := s.call(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "agent-with-event-trigger-go",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("MCP initialize 失败: %w", err)
	}
	return s.notify("notifications/initialized", map[string]any{})
}

// ListTools 拉取服务器声明的工具列表。
func (s *mcpSession) ListTools(ctx context.Context) ([]MCPTool, error) {
	raw, err := s.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result toolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("解析 tools/list 响应失败: %w", err)
	}
	return result.Tools, nil
}

// CallTool 执行一个工具，把返回的文本内容块拼成单个字符串。
func (s *mcpSession) CallTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, err := s.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", false, err
	}
	var result toolsCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", false, fmt.Errorf("解析 tools/call 响应失败: %w", err)
	}

	var texts []string
	for _, block := range result.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return joinLines(texts), result.IsError, nil
}

func (s *mcpSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("MCP 会话已关闭")
	}

	s.nextID++
	id := s.nextID
	if err := s.writeLocked(jsonRPCRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, mcpRequestTimeout)
	defer cancel()

	type readResult struct {
		raw json.RawMessage
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		raw, err := s.readResponse(id)
		done <- readResult{raw, err}
	}()

	select {
	case <-callCtx.Done():
		// 超时后会话状态不可信：后续响应会与下一次请求错位，只能丢弃整个会话。
		s.closeLocked()
		return nil, fmt.Errorf("MCP %s 超时: %w", method, callCtx.Err())
	case result := <-done:
		return result.raw, result.err
	}
}

func (s *mcpSession) notify(method string, params any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("MCP 会话已关闭")
	}
	return s.writeLocked(jsonRPCRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (s *mcpSession) writeLocked(req jsonRPCRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := s.stdin.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("写入 MCP 请求失败: %w", err)
	}
	return nil
}

// readResponse 读到匹配 id 的响应为止，跳过服务器主动发来的通知。
func (s *mcpSession) readResponse(id int64) (json.RawMessage, error) {
	for {
		line, err := s.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("读取 MCP 响应失败: %w", err)
		}
		if len(line) == 0 {
			continue
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // 非 JSON 行（部分服务器会把日志打到 stdout）直接跳过
		}
		if resp.ID == nil || *resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Alive 报告子进程是否还活着。
func (s *mcpSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

// Close 终止子进程。
func (s *mcpSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}

func (s *mcpSession) closeLocked() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.stdin != nil {
		s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
	return nil
}

func joinLines(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	default:
		out := items[0]
		for _, item := range items[1:] {
			out += "\n" + item
		}
		return out
	}
}
