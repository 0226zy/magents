// Command kimi-chat 是对 Kimi（Moonshot）模型 API 的最小调用示例：
// 按 config.yaml 中的配置，携带 system + user 两条消息调用一次 chat completion，
// 对应书中 2.2 节「Agent 如何调用大模型：理解 API 的上下文结构」。
//
// 进程入口只负责装配：读配置 → deps 容器 → interfaces 转发层。
//
// 用法：
//
//	kimi-chat "Hello, who are you?"
//	kimi-chat -config config.yaml "用一句话解释什么是 Agent"
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/0226zy/magents/kimi-chat-go/internal/interfaces"
)

func main() {
	handler := &interfaces.Handler{}
	if err := handler.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
