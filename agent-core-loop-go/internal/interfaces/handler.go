// Package interfaces 是对外协议适配层（CLI）：解析参数、调应用服务、打印结果。
// 不含业务逻辑，业务全在 application/service。
package interfaces

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-core-loop-go/internal/infrastructure/config"
	"github.com/0226zy/magents/agent-core-loop-go/internal/shared/deps"
)

// Handler 是 CLI 入口的转发层。
type Handler struct{}

// Run 解析命令行参数并运行一次 Agent 核心循环。
func (h *Handler) Run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	configPath := flags.String("config", "config.yaml", "模型 SDK 配置文件路径")
	if err := flags.Parse(args); err != nil {
		return err
	}

	question := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if question == "" {
		fmt.Fprintf(os.Stderr, "用法: %s [-config 配置文件] \"你的问题\"\n", filepath.Base(os.Args[0]))
		return errors.New("缺少问题参数")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("配置加载失败: %w", err)
	}

	container, err := deps.New(cfg)
	if err != nil {
		return err
	}
	defer container.Close()

	if container.RawLog != nil {
		container.Trajectory.Event("=== 启动: model=%s base_url=%s 日志文件=%s 原始响应文件=%s",
			cfg.Model, cfg.BaseURL, container.Trajectory.Path(), container.RawLog.Path())
	} else {
		container.Trajectory.Event("=== 启动: model=%s base_url=%s 日志文件=%s (原始响应记录已关闭)",
			cfg.Model, cfg.BaseURL, container.Trajectory.Path())
	}

	final, err := container.Service.Execute(ctx, cfg.SystemPrompt, question)
	if err != nil {
		container.Trajectory.Event("!!! 运行失败: %v", err)
		return err
	}

	fmt.Printf("\n最终回答: %s\n", final)
	container.Trajectory.Event("=== 完成，轨迹已写入 %s", container.Trajectory.Path())
	return nil
}

// ExitCode 把运行错误映射为进程退出码，供 main 使用。
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, derr.ErrMaxIterations):
		return 3
	default:
		return 1
	}
}
