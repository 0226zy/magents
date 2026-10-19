// Command client 向事件驱动 Agent 服务发送事件。
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
	opts, err := cli.ParseClientOptions(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cli.RunClient(ctx, opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "执行失败: %v\n", err)
		os.Exit(1)
	}
}
