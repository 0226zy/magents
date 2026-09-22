package moonshot

import (
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/derr"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
)

// 领域类型已上移到 domain（entity / derr），这里用类型别名保持适配器内部
// 代码与其测试的引用不变。adapter 之外不要再用这些别名，直接依赖 domain。
type (
	Message           = entity.Message
	ToolCall          = entity.ToolCall
	FunctionCall      = entity.FunctionCall
	ChatRequest       = entity.ChatRequest
	ChatResponse      = entity.ChatResponse
	Choice            = entity.Choice
	ChoiceMessage     = entity.ChoiceMessage
	Usage             = entity.Usage
	APITurn           = entity.APITurn
	TurnError         = entity.TurnError
	HTTPRequestRecord = entity.HTTPRequestRecord
	FiberRequest      = entity.FiberRequest
	Recorder          = entity.Recorder
)

var (
	ErrRateLimit      = derr.ErrRateLimit
	NewRecorder       = entity.NewRecorder
	IsRateLimit       = derr.IsRateLimit
	IsTimeout         = derr.IsTimeout
	ReasoningSafeTemp = ReasoningSafeTemperature
)

// StatusError 的别名（指针类型，别名直接可用）。
type StatusError = derr.StatusError
