// Package port 定义 application 层依赖的下游能力端口。
// 接口参数用领域类型（entity），由 infrastructure/adapter 实现。
package port

import (
	"context"

	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/entity"
)

// ChatModelPort 是模型 chat completion 能力。
// 由 infrastructure/adapter 的 Moonshot 客户端实现。
type ChatModelPort interface {
	Chat(ctx context.Context, messages []entity.Message, tools []entity.ToolDefinition) (*entity.ChatResponse, error)
}

// ToolPort 是工具执行能力：向模型声明有哪些工具，并按名字执行调用。
type ToolPort interface {
	Definitions() []entity.ToolDefinition
	Execute(name string, argumentsJSON string) (string, error)
}

// TrajectoryPort 是轨迹日志能力：把循环过程同时写到终端和日志文件。
type TrajectoryPort interface {
	// Event 记录一条轨迹事件（已含时间戳前缀，由实现负责格式化）。
	Event(format string, args ...any)
	// EventJSON 把一个结构化对象（如发给模型的完整消息列表）以单行 JSON 记录。
	EventJSON(label string, value any)
}

// RawLogPort 是模型原始响应日志能力：只写单独文件，不上终端。
type RawLogPort interface {
	LogResponse(iteration int, rawJSON []byte)
}
