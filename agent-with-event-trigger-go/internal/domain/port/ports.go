// Package port 定义 application 层依赖的下游能力端口。
// 接口参数用领域类型，由 infrastructure/adapter 实现，shared/deps 在组合根注入。
package port

import (
	"context"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
)

// ChatModelPort 是模型 chat completion 能力。
type ChatModelPort interface {
	Chat(ctx context.Context, messages []entity.Message, tools []entity.ToolDefinition,
		opts entity.ChatOptions) (*entity.ChatResponse, error)
	Model() string
	Provider() string
}

// ToolPort 是工具能力：向模型声明有哪些工具，并按名字执行。
type ToolPort interface {
	Definitions() []entity.ToolDefinition
	Has(name string) bool
	Execute(ctx context.Context, name string, args map[string]any) (any, error)
}

// WorkspacePort 暴露当前工作目录。
// 目录状态归内置工具适配器所有——execute_command 的 cd 会改它。
type WorkspacePort interface {
	CurrentDirectory() string
	Reset()
}

// MCPServerReport 是单个 MCP 服务器的加载结果。
type MCPServerReport struct {
	Name       string `json:"name"`
	Loaded     bool   `json:"loaded"`
	ToolsCount int    `json:"tools_count"`
	Error      string `json:"error,omitempty"`
}

// MCPLoadReport 是一次 MCP 加载的汇总。
// 逐 server 返回失败原因，避免 Python 版那种「三个服务器全部静默失败」。
type MCPLoadReport struct {
	Servers    []MCPServerReport `json:"servers"`
	ToolsCount int               `json:"tools_count"`
}

// MCPPort 是 MCP 工具能力，Load 可重复调用即 reload。
type MCPPort interface {
	ToolPort
	Load(ctx context.Context) (MCPLoadReport, error)
	ToolNames() []string
	Loaded() bool
	Close() error
}

// TrajectoryPort 是轨迹落盘能力。
type TrajectoryPort interface {
	Save(snapshot entity.TrajectorySnapshot) error
	Path() string
}

// Clock 是时间来源。Advance 支持 simulate_time_delay 的虚拟时间推进，
// 真实时钟实现为空操作；测试用受控时钟驱动超时逻辑，不依赖真实 sleep。
type Clock interface {
	Now() time.Time
	Advance(d time.Duration)
}

// Logger 是带级别的行日志。
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

// TriggerSource 是事件触发器：注册时实例化，触发时把事件推给 emit。
// Run 阻塞直到 ctx 取消。
type TriggerSource interface {
	Name() string
	Run(ctx context.Context, emit func(entity.Event))
}

// EventDispatcher 把一个事件交给处理者（真实 Agent 或离线模拟）。
type EventDispatcher interface {
	Dispatch(ctx context.Context, event entity.Event) error
}

// MailboxPort 是实验 6-1 依赖的邮件与日历能力。
type MailboxPort interface {
	ListAccounts(ctx context.Context) ([]mailbox.ProviderObject, error)
	ListFolders(ctx context.Context, accountID string) ([]mailbox.ProviderObject, error)
	ListEmails(ctx context.Context, accountID string, after time.Time, folder string, limit int) ([]mailbox.ProviderObject, error)
	GetEmail(ctx context.Context, emailID string) (mailbox.ProviderObject, error)
	UpdateEmailFolders(ctx context.Context, emailID string, folders []string) (mailbox.ProviderObject, error)
	SendEmail(ctx context.Context, accountID, recipient, subject, body string) (mailbox.ProviderObject, error)
	ListCalendars(ctx context.Context, accountID string) ([]mailbox.ProviderObject, error)
	ListCalendarEvents(ctx context.Context, accountID, calendarID string, start, end time.Time) ([]mailbox.ProviderObject, error)
	Calls() []mailbox.CallReceipt
	Close()
}

// ArtifactRef 是一份落盘产物的引用。
type ArtifactRef struct {
	Path   string
	Bytes  int
	SHA256 string
}

// ArtifactPort 负责实验产物落盘。
type ArtifactPort interface {
	WriteFile(relative string, data []byte) (ArtifactRef, error)
	// AppendLineDurable 追加一行并 fsync：高优通知必须在进程被杀前已经落到磁盘。
	AppendLineDurable(relative string, line []byte) (ArtifactRef, error)
}

// EvidencePort 负责证据文件落盘，写入前做凭据扫描。
type EvidencePort interface {
	Write(relative string, value any) (ArtifactRef, error)
	Manifest(excludeNames ...string) ([]ArtifactRef, error)
	Root() string
}
