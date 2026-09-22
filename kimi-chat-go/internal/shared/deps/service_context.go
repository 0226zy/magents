// Package deps 是依赖注入容器（组合根）：
// 在这里把 infrastructure 的适配器实例化，注入 application 层。
// 这是全项目唯一允许同时 import application 与 infrastructure 的地方。
package deps

import (
	"time"

	"github.com/0226zy/magents/kimi-chat-go/internal/application/service"
	"github.com/0226zy/magents/kimi-chat-go/internal/infrastructure/adapter"
	"github.com/0226zy/magents/kimi-chat-go/internal/infrastructure/config"
)

// NewChatService 按配置装配应用服务。
func NewChatService(cfg *config.Config) (*service.ChatService, error) {
	client, err := adapter.NewMoonshotClient(adapter.MoonshotConfig{
		APIKey:      cfg.APIKey,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		Timeout:     time.Duration(cfg.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return service.NewChatService(client), nil
}
