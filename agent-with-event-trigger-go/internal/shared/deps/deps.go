// Package deps 是组合根：唯一同时 import application 与 infrastructure 的包。
// 其它任何地方都不应出现这种跨向依赖。
package deps

import (
	"context"
	"fmt"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/agent"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/monitor"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/adapter"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/config"
)

// SupportedProviders 透出可选的模型提供商，供接口层渲染帮助文本。
func SupportedProviders() []string { return config.SupportedProviders() }

// Runtime 是装配完成的运行时对象集合。
type Runtime struct {
	Config  *config.Config
	Agent   *agent.EventAgentService
	Monitor *monitor.Monitor
	Logger  port.Logger

	closers []func()
}

// Close 释放 MCP 子进程等资源。
func (r *Runtime) Close() {
	for i := len(r.closers) - 1; i >= 0; i-- {
		r.closers[i]()
	}
}

// Build 按配置装配整套运行时。
func Build(cfg *config.Config, log port.Logger) (*Runtime, error) {
	if log == nil {
		log = adapter.NewStdLogger()
	}
	if err := cfg.Resolve(); err != nil {
		return nil, fmt.Errorf("解析模型提供商配置: %w", err)
	}

	chat, err := adapter.NewLLMClient(adapter.LLMConfig{
		Provider: cfg.ResolvedProvider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
		Timeout:  time.Duration(cfg.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if cfg.FellBackToRouter {
		log.Warn("未找到 %s 的 API Key，已回落到 OpenRouter（模型 %s）", cfg.Provider, cfg.Model)
	}

	runtime := &Runtime{Config: cfg, Logger: log}
	hints := cfg.HintConfig()

	// simulate_time_delay 下用虚拟时钟：时间由事件循环推进，不真的 sleep。
	var clock port.Clock = adapter.NewSystemClock()
	if hints.SimulateTimeDelay {
		clock = adapter.NewSimulatedClock(time.Now())
	}

	var trajectory port.TrajectoryPort = adapter.NopTrajectory{}
	if hints.SaveTrajectory {
		file, err := adapter.NewTrajectoryFile(hints.TrajectoryFile)
		if err != nil {
			return nil, err
		}
		trajectory = file
	}

	var mcpPort port.MCPPort
	if cfg.MCP.Enabled && len(cfg.MCP.Servers) > 0 {
		manager := adapter.NewMCPManager(mcpSpecs(cfg), log)
		mcpPort = manager
		runtime.closers = append(runtime.closers, func() { _ = manager.Close() })
	}

	builtin := adapter.NewBuiltinTools()
	service := agent.New(agent.Deps{
		Chat:          chat,
		Builtin:       builtin,
		Workspace:     builtin,
		MCP:           mcpPort,
		Trajectory:    trajectory,
		Clock:         clock,
		Logger:        log,
		Hints:         hints,
		Options:       cfg.ChatOptions(cfg.Model),
		SystemStateFn: adapter.SystemState,
		SystemPrompt:  cfg.SystemPrompt,
		MaxIterations: cfg.MaxIterations,
	})

	runtime.Agent = service
	runtime.Monitor = monitor.New(service, service, clock, log, monitor.Config{
		Interval:       time.Duration(cfg.Monitor.IntervalSeconds) * time.Second,
		UserTimeout:    time.Duration(cfg.Monitor.UserTimeoutSeconds) * time.Second,
		ProcessTimeout: time.Duration(cfg.Monitor.ProcessTimeoutSeconds) * time.Second,
	})
	return runtime, nil
}

// LoadMCPTools 在前台加载 MCP 工具，逐个回报加载结果。
func (r *Runtime) LoadMCPTools(ctx context.Context) {
	if r.Agent.MCP() == nil {
		return
	}
	report, err := r.Agent.LoadMCP(ctx)
	if err != nil {
		r.Logger.Error("MCP 工具加载失败: %v", err)
		return
	}
	for _, server := range report.Servers {
		if server.Error != "" {
			r.Logger.Warn("MCP 服务器 %s 加载失败: %s", server.Name, server.Error)
			continue
		}
		r.Logger.Info("MCP 服务器 %s 已加载 %d 个工具", server.Name, server.ToolsCount)
	}
}

func mcpSpecs(cfg *config.Config) []adapter.MCPServerSpec {
	specs := make([]adapter.MCPServerSpec, 0, len(cfg.MCP.Servers))
	for _, server := range cfg.MCP.Servers {
		specs = append(specs, adapter.MCPServerSpec{
			Name:    server.Name,
			Command: server.Command,
			Args:    server.Args,
			Env:     server.Env,
		})
	}
	return specs
}
