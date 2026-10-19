// Command eventloop 演示多触发器汇流到统一事件队列，由 Agent 逐个处理。
// 加 -mock 可在没有任何 API Key 的环境下跑通「触发 → 唤醒 → 处理」闭环。
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
	opts, err := cli.ParseEventLoopOptions(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cli.RunEventLoop(ctx, opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "执行失败: %v\n", err)
		os.Exit(1)
	}
}
