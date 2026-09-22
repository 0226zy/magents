// Package deps 是依赖注入容器（组合根）：
// 在这里把 infrastructure 的适配器实例化，注入 application 层。
// 这是全项目唯一允许同时 import application 与 infrastructure 的地方。
package deps

import (
	"time"

	"github.com/0226zy/magents/agent-core-loop-go/internal/application/service"
	"github.com/0226zy/magents/agent-core-loop-go/internal/infrastructure/adapter"
	"github.com/0226zy/magents/agent-core-loop-go/internal/infrastructure/config"
)

// Container 持有装配好的全部依赖。
type Container struct {
	Trajectory *adapter.Trajectory
	RawLog     *adapter.RawFile // enable_raw_log=false 时为 nil
	Service    *service.AgentService
}

// New 按配置装配：轨迹日志（+raw）→ Moonshot 客户端 → 工具注册表 → 应用服务。
func New(cfg *config.Config) (*Container, error) {
	var (
		trajectory *adapter.Trajectory
		rawLog     *adapter.RawFile
		err        error
	)
	if cfg.RawLogEnabled() {
		trajectory, rawLog, err = adapter.NewTrajectoryPair(cfg.LogDir)
	} else {
		trajectory, err = adapter.NewTrajectory(cfg.LogDir)
	}
	if err != nil {
		return nil, err
	}

	client, err := adapter.NewMoonshotClient(adapter.MoonshotConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		trajectory.Close()
		if rawLog != nil {
			rawLog.Close()
		}
		return nil, err
	}

	return &Container{
		Trajectory: trajectory,
		RawLog:     rawLog,
		Service:    service.NewAgentService(client, adapter.NewToolRegistry(), trajectory, rawLog, cfg.MaxIterations),
	}, nil
}

// Close 关闭全部日志文件。
func (c *Container) Close() {
	c.Trajectory.Close()
	if c.RawLog != nil {
		c.RawLog.Close()
	}
}
