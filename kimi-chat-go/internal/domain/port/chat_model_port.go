// Package port 定义 application 层依赖的下游能力端口。
// 由 infrastructure/adapter 实现。
package port

import (
	"context"

	"github.com/0226zy/magents/kimi-chat-go/internal/domain/entity"
)

// ChatModelPort 是模型 chat completion 能力。
type ChatModelPort interface {
	Chat(ctx context.Context, messages []entity.Message) (*entity.ChatResponse, error)
}
