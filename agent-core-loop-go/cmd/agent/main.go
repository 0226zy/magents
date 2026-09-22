// Command agent 运行书中 2.2.4 节的最简 Agent 核心循环。
// 进程入口只负责装配：读配置 → deps 容器 → interfaces 转发层。
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/0226zy/magents/agent-core-loop-go/internal/interfaces"
)

func main() {
	handler := &interfaces.Handler{}
	if err := handler.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(interfaces.ExitCode(err))
	}
}
