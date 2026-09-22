// Package port 定义 application 层依赖的下游能力端口。
// 接口参数用领域类型（entity），infrastructure/adapter 负责实现。
package port

import (
	"context"
	"encoding/json"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
)

// ChatCaller 是 ReAct 循环需要的 chat completion 能力。
// 由 infrastructure/adapter/moonshot 的 ChatClient 实现。
type ChatCaller interface {
	Chat(ctx context.Context, messages []entity.Message, tools []json.RawMessage) (*entity.ChatResponse, error)
}

// FormulaRunner 是 ReAct 循环需要的 Formula 能力。
// 由 infrastructure/adapter/moonshot 的 FormulaClient 实现。
type FormulaRunner interface {
	Tools(ctx context.Context) ([]json.RawMessage, error)
	ExecuteFiber(ctx context.Context, name, rawArguments string) (string, error)
}
