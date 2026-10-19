// Command experiment61 执行实验 6-1：以真实邮箱为事件源，验证事件驱动 Agent
// 在会议邀请、客户投诉、营销邮件三类场景下的端到端处理，并产出脱敏证据。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/interfaces/cli"
)

func main() {
	opts, err := cli.ParseExperimentOptions(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dir, err := cli.RunExperiment(ctx, opts, os.Stdout)
	if dir != "" {
		fmt.Println(dir)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "执行失败: %v\n", err)
		os.Exit(1)
	}
}
