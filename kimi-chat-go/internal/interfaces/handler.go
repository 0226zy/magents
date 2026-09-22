// Package interfaces 是对外协议适配层（CLI）：解析参数、调应用服务、打印结果。
// 不含业务逻辑，业务全在 application/service。
package interfaces

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0226zy/magents/kimi-chat-go/internal/infrastructure/config"
	"github.com/0226zy/magents/kimi-chat-go/internal/shared/deps"
)

// Handler 是 CLI 入口的转发层。
type Handler struct{}

// Run 解析命令行参数并发起一次 chat completion。
func (h *Handler) Run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("kimi-chat", flag.ContinueOnError)
	configPath := flags.String("config", "config.yaml", "模型 SDK 配置文件路径")
	if err := flags.Parse(args); err != nil {
		return err
	}

	question := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if question == "" {
		fmt.Fprintf(os.Stderr, "用法: %s [-config 配置文件] \"你的问题\"\n", filepath.Base(os.Args[0]))
		return fmt.Errorf("缺少问题参数")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("配置加载失败: %w", err)
	}

	svc, err := deps.NewChatService(cfg)
	if err != nil {
		return err
	}

	resp, err := svc.Execute(ctx, cfg.SystemPrompt, question)
	if err != nil {
		return fmt.Errorf("调用失败: %w", err)
	}

	fmt.Println(resp.Choices[0].Message.Content)
	fmt.Fprintf(os.Stderr, "\n[model=%s finish=%s tokens: prompt=%d completion=%d total=%d]\n",
		cfg.Model, resp.Choices[0].FinishReason,
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	return nil
}
