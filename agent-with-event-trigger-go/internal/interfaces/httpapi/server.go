// Package httpapi 提供事件 Agent 的 HTTP 接口。
//
// Python 版有 server.py 与 server_fastapi.py 两份近乎重复的实现，这里只保留一份。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/agent"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/monitor"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// maxBodyBytes 限制请求体大小，避免单个请求把内存吃光。
const maxBodyBytes = 4 << 20

// Server 是 HTTP 接口层。
type Server struct {
	agent   *agent.EventAgentService
	monitor *monitor.Monitor
	log     port.Logger
	started time.Time

	// reloadMu 保证同一时刻只有一个 MCP 重载在跑。
	reloadMu  sync.Mutex
	reloading bool
}

// New 构造 HTTP 接口层。
func New(service *agent.EventAgentService, mon *monitor.Monitor, log port.Logger) *Server {
	return &Server{agent: service, monitor: mon, log: log, started: time.Now()}
}

// Handler 返回配置好路由的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /event", s.handleEvent)
	mux.HandleFunc("GET /agent/status", s.handleAgentStatus)
	mux.HandleFunc("POST /agent/reset", s.handleAgentReset)
	mux.HandleFunc("GET /mcp/status", s.handleMCPStatus)
	mux.HandleFunc("POST /mcp/reload", s.handleMCPReload)
	mux.HandleFunc("POST /process/register", s.handleProcessRegister)
	mux.HandleFunc("POST /process/unregister", s.handleProcessUnregister)
	mux.HandleFunc("POST /monitoring/start", s.handleMonitoringStart)
	mux.HandleFunc("POST /monitoring/stop", s.handleMonitoringStop)
	mux.HandleFunc("GET /monitoring/status", s.handleMonitoringStatus)
	// 兜底：区分 404 与 405，默认 mux 对方法不匹配只会给 404。
	mux.HandleFunc("/", s.handleFallback)
	return mux
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "Event-Driven Agent (Go)",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"POST /event":              "投递事件并同步获取处理结果",
			"GET /health":              "健康检查",
			"GET /agent/status":        "Agent 运行时状态",
			"POST /agent/reset":        "重置会话状态",
			"GET /mcp/status":          "MCP 工具加载状态",
			"POST /mcp/reload":         "重新加载 MCP 工具",
			"POST /process/register":   "注册后台进程",
			"POST /process/unregister": "注销后台进程",
			"POST /monitoring/start":   "启动超时监控",
			"POST /monitoring/stop":    "停止超时监控",
			"GET /monitoring/status":   "超时监控状态",
		},
		"event_types": entity.AllEventTypes(),
	})
}

// handleHealth 只读 stateMu，正在跑的 LLM 调用不会把健康检查卡住。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	status := s.agent.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "healthy",
		"agent_ready":      true,
		"uptime_seconds":   int(time.Since(s.started).Seconds()),
		"provider":         status.Provider,
		"model":            status.Model,
		"mcp_tools_loaded": status.MCPToolsLoaded,
	})
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	var request EventRequest
	if !s.bind(w, r, &request) {
		return
	}
	event, err := request.Validate()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.agent.HandleEvent(r.Context(), event)
	if err != nil && !errors.Is(err, derr.ErrMaxIterations) {
		s.log.Error("处理事件失败: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAgentStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.agent.Status())
}

func (s *Server) handleAgentReset(w http.ResponseWriter, _ *http.Request) {
	s.agent.Reset()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Agent 状态已重置",
	})
}

func (s *Server) handleMCPStatus(w http.ResponseWriter, _ *http.Request) {
	mcp := s.agent.MCP()
	if mcp == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false, "loaded": false, "tools_count": 0,
			"tools": []string{}, "servers": map[string][]string{},
		})
		return
	}
	names := mcp.ToolNames()
	payload := map[string]any{
		"enabled":     true,
		"loaded":      mcp.Loaded(),
		"tools_count": len(names),
		"tools":       names,
	}
	if grouped, ok := mcp.(interface{ ToolsByServer() map[string][]string }); ok {
		payload["servers"] = grouped.ToolsByServer()
	}
	if loading, ok := mcp.(interface{ Loading() bool }); ok {
		payload["loading"] = loading.Loading()
	}
	writeJSON(w, http.StatusOK, payload)
}

// handleMCPReload 在后台重载。重载期间再次调用返回 409，而不是并发启两次子进程。
func (s *Server) handleMCPReload(w http.ResponseWriter, _ *http.Request) {
	if s.agent.MCP() == nil {
		writeError(w, http.StatusBadRequest, "MCP 未启用")
		return
	}

	s.reloadMu.Lock()
	if s.reloading {
		s.reloadMu.Unlock()
		writeError(w, http.StatusConflict, "MCP 工具正在加载中")
		return
	}
	s.reloading = true
	s.reloadMu.Unlock()

	go func() {
		defer func() {
			s.reloadMu.Lock()
			s.reloading = false
			s.reloadMu.Unlock()
		}()
		// 脱离请求 context：请求早已返回，用它会让加载立刻被取消。
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.agent.LoadMCP(ctx); err != nil {
			s.log.Error("MCP 重新加载失败: %v", err)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"message": "MCP 工具重新加载已启动",
	})
}

func (s *Server) handleProcessRegister(w http.ResponseWriter, r *http.Request) {
	var request ProcessRequest
	if !s.bind(w, r, &request) {
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.agent.RegisterProcess(request.ProcessID, request.ResolvedName(), request.Metadata)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"process_id":   request.ProcessID,
		"process_name": request.ResolvedName(),
	})
}

func (s *Server) handleProcessUnregister(w http.ResponseWriter, r *http.Request) {
	var request ProcessRequest
	if !s.bind(w, r, &request) {
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	existed := s.agent.UnregisterProcess(request.ProcessID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"process_id": request.ProcessID,
		"existed":    existed,
	})
}

func (s *Server) handleMonitoringStart(w http.ResponseWriter, r *http.Request) {
	var request MonitoringConfigRequest
	if !s.bind(w, r, &request) {
		return
	}
	s.monitor.Reconfigure(monitor.Config{
		Interval:       seconds(request.IntervalSeconds),
		UserTimeout:    seconds(request.UserTimeoutSeconds),
		ProcessTimeout: seconds(request.ProcessTimeoutSeconds),
	})
	// 已在运行时先停再起，新阈值才会被循环读到。
	s.monitor.Stop()
	// 监控生命周期与进程绑定，不能挂在某个请求的 context 上。
	s.monitor.Start(context.Background())
	writeJSON(w, http.StatusOK, monitoringPayload(s.monitor, "超时监控已启动"))
}

func (s *Server) handleMonitoringStop(w http.ResponseWriter, _ *http.Request) {
	s.monitor.Stop()
	writeJSON(w, http.StatusOK, monitoringPayload(s.monitor, "超时监控已停止"))
}

func (s *Server) handleMonitoringStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, monitoringPayload(s.monitor, ""))
}

func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	if allowed, known := routeMethods[r.URL.Path]; known {
		w.Header().Set("Allow", allowed)
		writeError(w, http.StatusMethodNotAllowed, "方法不被允许，请使用 "+allowed)
		return
	}
	writeError(w, http.StatusNotFound, "未知路径: "+r.URL.Path)
}

// routeMethods 让兜底处理器能区分「路径不存在」与「方法用错了」。
var routeMethods = map[string]string{
	"/":                   "GET",
	"/health":             "GET",
	"/event":              "POST",
	"/agent/status":       "GET",
	"/agent/reset":        "POST",
	"/mcp/status":         "GET",
	"/mcp/reload":         "POST",
	"/process/register":   "POST",
	"/process/unregister": "POST",
	"/monitoring/start":   "POST",
	"/monitoring/stop":    "POST",
	"/monitoring/status":  "GET",
}

// bind 读取并解析请求体，失败时自行写出 400。
func (s *Server) bind(w http.ResponseWriter, r *http.Request, target any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取请求体失败: "+err.Error())
		return false
	}
	if err := decodeJSON(raw, target); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return false
	}
	return true
}

func monitoringPayload(m *monitor.Monitor, message string) map[string]any {
	cfg := m.Config()
	payload := map[string]any{
		"success":                 true,
		"running":                 m.Running(),
		"interval_seconds":        cfg.Interval.Seconds(),
		"user_timeout_seconds":    cfg.UserTimeout.Seconds(),
		"process_timeout_seconds": cfg.ProcessTimeout.Seconds(),
	}
	if message != "" {
		payload["message"] = message
	}
	return payload
}

func seconds(value *float64) time.Duration {
	if value == nil || *value <= 0 {
		return 0
	}
	return time.Duration(*value * float64(time.Second))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"success": false, "detail": message})
}
