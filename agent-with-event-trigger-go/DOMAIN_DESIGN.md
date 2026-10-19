# 关键领域代码设计说明（Go）

[DESIGN.md](DESIGN.md) 讲分层与协作；本文档精确到**类型、方法签名、不变量、算法与测试点**，
是写代码时的唯一依据。所有 `domain` 包只依赖标准库。

---

## 1. `domain/entity/event_entity.go`：事件

```go
type EventType string

const (
    EventWebMessage     EventType = "web_message"
    EventIMMessage      EventType = "im_message"
    EventEmailReply     EventType = "email_reply"
    EventGitHubPRUpdate EventType = "github_pr_update"
    EventTimerTrigger   EventType = "timer_trigger"
    EventFileChange     EventType = "file_change"
    EventUserTimeout    EventType = "user_timeout"
    EventProcessTimeout EventType = "process_timeout"
    EventSystemAlert    EventType = "system_alert"
)

func AllEventTypes() []EventType
func ParseEventType(s string) (EventType, error)  // 未知 -> fmt.Errorf("%w: %s", derr.ErrUnknownEventType, s)
func (t EventType) IsUserInteraction() bool       // web / im / email 三种为 true

type Event struct {
    ID        string         `json:"event_id"`
    Type      EventType      `json:"event_type"`
    Content   string         `json:"content"`
    Metadata  map[string]any `json:"metadata"`
    Timestamp time.Time      `json:"timestamp"`
}

func NewEvent(t EventType, content string, meta map[string]any, now time.Time) Event
func (e Event) ToUserMessage() string
```

### 不变量

- `NewEvent` 在 `meta == nil` 时填入空 map，`now` 零值时用 `time.Now()`。
  调用方永远不需要判空。
- ID 形如 `evt_<unixmilli>_<单调序号>`。序号不可省：Python 版只有毫秒时间戳，
  同一毫秒内产生的事件（多个触发器同时到期、客户端连发）会拿到同一个 ID，
  轨迹与客户端都无法区分它们。
- `IsUserInteraction()` 的三种类型会刷新 `lastUserInteraction`，直接决定用户超时提醒的时机。

### `ToUserMessage` 渲染表（与 `event_types.py` 逐字一致）

| 事件类型 | 输出 | metadata 键（缺省值） |
|---|---|---|
| `web_message` | `[Web Interface] {content}` | — |
| `im_message` | `[IM from {sender}] {content}` | `sender`（`Unknown`） |
| `email_reply` | `[Email Reply from {from}]\nSubject: {subject}\n{content}` | `from`（`Unknown`）、`subject`（`No Subject`） |
| `github_pr_update` | `[GitHub PR #{pr_number} {action}] {content}` | `pr_number`（`Unknown`）、`action`（`updated`） |
| `timer_trigger` | `[Timer {timer_id} triggered] {content}` | `timer_id`（`Unknown`） |
| `file_change` | `[File {change}: {path}] {content}` | `path`（`Unknown`）、`change`（`modified`） |
| `user_timeout` | `[System Reminder] User has not responded for {duration}. {content}` | `duration`（`unknown`） |
| `process_timeout` | `[System Alert] Background process {process_id} has been running for {duration}. {content}` | `process_id`（`Unknown`）、`duration`（`unknown`） |
| `system_alert` | `[System Alert: {alert_type}] {content}` | `alert_type`（`general`） |
| 其他 | `[{type}] {content}` | — |

metadata 取值统一走 `metaString(key, fallback)`：值为 `nil` 或空字符串时用 fallback，
否则 `fmt.Sprint(v)`。这让 JSON 里的 `42` 与 `"42"` 渲染结果相同——Python 的 f-string 行为一致。

### 测试点

- 9 种类型表驱动，逐字比对输出。
- metadata 缺键 / 为 nil / 值是数字三种情况。
- `ParseEventType("nope")` 可被 `errors.Is(err, derr.ErrUnknownEventType)` 命中。

---

## 2. `domain/entity/message_entity.go`：协议消息

```go
// 请求侧：刻意不含 reasoning_content / refusal
type Message struct {
    Role       string     `json:"role"`
    Content    string     `json:"content,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"`     // 恒为 "function"
    Function FunctionCall `json:"function"`
}
type FunctionCall struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"`     // 模型生成的原始 JSON 字符串
}

type ToolDefinition struct {
    Type     string       `json:"type"`
    Function FunctionSpec `json:"function"`
}
type FunctionSpec struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Parameters  any    `json:"parameters"`  // JSON Schema；MCP 工具原样透传 provider 的 schema
}

// 响应侧：比请求侧多 reasoning_content，只能读、不能回传
type ChoiceMessage struct {
    Role             string     `json:"role"`
    Content          string     `json:"content"`
    ReasoningContent string     `json:"reasoning_content"`
    ToolCalls        []ToolCall `json:"tool_calls"`
}
func (m ChoiceMessage) ToAssistantMessage() Message   // 结构上丢弃 ReasoningContent

type ChatOptions struct {
    Temperature float64
    MaxTokens   int
    ToolChoice  string        // "auto"
}

type ChatResponse struct {
    Choices []Choice        `json:"choices"`
    Usage   Usage           `json:"usage"`
    Raw     json.RawMessage `json:"-"`
}
```

### 两个纯函数

```go
// 推理模型只接受 temperature=1；模型名小写并把 "/" 换成 "-" 后匹配
func SafeTemperature(model string, requested float64) float64

// 有 "FINAL ANSWER:" 标记时取标记之后的内容并 TrimSpace，marked=true
// 无标记时返回 TrimSpace(content)，marked=false
func ExtractFinalAnswer(content string) (answer string, marked bool)
```

`SafeTemperature` 的匹配集合为 `{"kimi-k3", "gpt-5"}`，与 Python `_reasoning_safe_temperature`
一致。把 `/` 归一成 `-` 是为了让 `openai/gpt-5` 这类 OpenRouter 模型名也能命中。

### 不变量

- **历史里的 assistant 消息永远不含 `reasoning_content`**。这不是靠记得清理，而是类型里
  没有这个字段——与 [web-search-agent-go](../web-search-agent-go/internal/domain/entity/message_entity.go)
  同一思路。违反它的后果是 Moonshot 返回 `400 tokenization failed`。
- `FunctionCall.Arguments` 保存模型生成的**原文**。执行前解析成 map 供工具使用，
  但记录与回传都用原文，避免重新序列化丢字段。

### 测试点

- `SafeTemperature`：`kimi-k3` / `openai/gpt-5` / `kimi-k2.6` / `doubao-*` 四类。
- `ExtractFinalAnswer`：有标记 / 无标记 / 标记后为空 / 多个标记（取第一个之后的全部）。
- 序列化 `ToAssistantMessage()` 的结果，断言 JSON 里不含 `reasoning_content`。

---

## 3. `domain/entity/todo_entity.go`：TODO 聚合根

```go
type TodoStatus string
const (
    TodoPending    TodoStatus = "pending"
    TodoInProgress TodoStatus = "in_progress"
    TodoCompleted  TodoStatus = "completed"
    TodoCancelled  TodoStatus = "cancelled"
)
func ParseTodoStatus(s string) (TodoStatus, error)  // 非法 -> derr.ErrInvalidTodoStatus
func (s TodoStatus) Symbol() string                 // ⏳ 🔄 ✅ ❌，未知为 ❓

type TodoItem struct {
    ID        int        `json:"id"`
    Content   string     `json:"content"`
    Status    TodoStatus `json:"status"`
    CreatedAt string     `json:"created_at"`
    UpdatedAt *string    `json:"updated_at"`
}

type TodoUpdate struct { ID int; Status TodoStatus }

type TodoList struct {   // 非线程安全，由调用方（EventAgentService）持锁
    items  []TodoItem
    nextID int           // 从 1 开始
}

func (l *TodoList) Rewrite(contents []string, now time.Time) RewriteResult
func (l *TodoList) Update(updates []TodoUpdate, now time.Time) UpdateResult
func (l *TodoList) Format() string
func (l *TodoList) Items() []TodoItem   // 深拷贝
func (l *TodoList) Len() int
func (l *TodoList) Clear()

type RewriteResult struct { Kept, New, Total int }
type UpdateResult  struct { Updated, Total int }
```

### 不变量

1. **ID 单调递增且永不复用**：`nextID` 只增不减，`Clear()` 才回到 1。
   模型在对话中用 ID 指代任务，复用 ID 会让历史里的引用指向错误条目。
2. **`Rewrite` 只保留 completed / cancelled**：pending 与 in_progress 被新列表替换。
   保留的条目排在前，新条目接在后。
3. **`Update` 是全有或全无**：状态字符串的合法性在 `interfaces`/应用层解析 `TodoUpdate` 时
   已经校验（`ParseTodoStatus`），所以聚合根只处理「ID 不存在」——静默跳过且不计数，与
   Python 行为一致。调用方若传入了非法状态字符串，在构造 `TodoUpdate` 阶段就会失败，
   整批不生效，不会出现 Python 那样的半更新。
4. **不强制「同时只有一个 in_progress」**：这是系统提示词对模型的要求，不是数据约束。
   聚合根强制它会让模型的合法中间状态被拒绝。

### `Format()` 输出

```
TODO List:
  [1] ✅ 读取配置文件 (completed)
  [2] 🔄 解析依赖 (in_progress)
```

空列表输出单行 `TODO List: Empty`。

### 测试点

- `Rewrite` 两次后 ID 不复用；completed 被保留、pending 被丢弃。
- `Update` 未知 ID 不计数；`Updated` 计数准确；`UpdatedAt` 被写入。
- `Format()` 空列表与非空列表。

---

## 4. `domain/entity/tool_call_entity.go`：工具调用记录与计数

```go
type ToolCallRecord struct {
    ToolName   string         `json:"tool_name"`
    Arguments  map[string]any `json:"arguments"`
    Result     any            `json:"result"`
    Error      string         `json:"error,omitempty"`
    CallNumber int            `json:"call_number"`
    Timestamp  string         `json:"timestamp"`
    DurationMs int64          `json:"duration_ms"`
}

type ToolCounter struct { counts map[string]int }
func (c *ToolCounter) Next(name string, enabled bool) int   // enabled=false 恒返回 1
func (c *ToolCounter) Total() int
func (c *ToolCounter) Reset()

func ToolResultPrefix(timestamp string, callNumber int, toolName string, cfg SystemHintConfig) string
```

### 不变量

- 计数**跨事件累积**，只有 `Reset()` 清零。这是设计意图：模型看到 `Tool call #7` 才知道
  自己在原地打转。
- `DurationMs` 必须写入真实耗时（Python 算了但没赋值，字段永远是 `null`）。
- `ToolResultPrefix` 的输出形如 `[2026-10-12 10:30:00] [Tool call #3 for 'read_file']\n`；
  两个开关都关闭时返回空串，此时工具结果不带任何前缀。

---

## 5. `domain/entity`：其余实体

### 5.1 `hint_config_entity.go`

```go
type SystemHintConfig struct {
    EnableTimestamps     bool   `json:"enable_timestamps"`
    EnableToolCounter    bool   `json:"enable_tool_counter"`
    EnableTodoList       bool   `json:"enable_todo_list"`
    EnableDetailedErrors bool   `json:"enable_detailed_errors"`
    EnableSystemState    bool   `json:"enable_system_state"`
    TimestampFormat      string `json:"timestamp_format"`     // Go layout，默认 "2006-01-02 15:04:05"
    SimulateTimeDelay    bool   `json:"simulate_time_delay"`
    SaveTrajectory       bool   `json:"save_trajectory"`
    TrajectoryFile       string `json:"trajectory_file"`
}
func DefaultSystemHintConfig() SystemHintConfig
```

JSON 键名与 Python 轨迹文件里的 `config` 段对齐，`timestamp_format` 例外：Python 存的是
`%Y-%m-%d %H:%M:%S`，Go 存的是等价的 Go layout，无法共用同一字符串。

### 5.2 `process_entity.go`

```go
type BackgroundProcess struct {
    ID        string         `json:"-"`
    Name      string         `json:"name"`
    StartTime time.Time      `json:"start_time"`
    Metadata  map[string]any `json:"metadata"`
    Reminded  bool           `json:"reminded"`
}

type ProcessRegistry struct { items map[string]*BackgroundProcess }
func (r *ProcessRegistry) Register(p BackgroundProcess)
func (r *ProcessRegistry) Unregister(id string) bool
func (r *ProcessRegistry) Overdue(now time.Time, threshold time.Duration) []BackgroundProcess  // 仅未提醒过的，按 ID 排序
func (r *ProcessRegistry) MarkReminded(id string)
func (r *ProcessRegistry) Len() int
func (r *ProcessRegistry) Snapshot() map[string]BackgroundProcess
func (r *ProcessRegistry) Clear()
```

`Overdue` 按 ID 排序输出，保证提醒事件的顺序可复现（Go map 遍历顺序随机）。

### 5.3 `trajectory_entity.go`

```go
type TrajectorySnapshot struct {
    Timestamp           string                       `json:"timestamp"`
    Iteration           int                          `json:"iteration"`
    Provider            string                       `json:"provider"`
    Model               string                       `json:"model"`
    ConversationHistory []Message                    `json:"conversation_history"`
    ToolCalls           []ToolCallRecord             `json:"tool_calls"`
    TodoList            []TodoItem                   `json:"todo_list"`
    CurrentDirectory    string                       `json:"current_directory"`
    FinalAnswer         *string                      `json:"final_answer"`
    BackgroundProcesses map[string]BackgroundProcess `json:"background_processes"`
    Config              SystemHintConfig             `json:"config"`
}
```

顶层键名与 Python 轨迹文件逐字一致，便于用同一个脚本比对两种实现的运行轨迹。

### 5.4 `result_entity.go`

```go
type EventResult struct {
    EventID            string   `json:"event_id"`
    FinalAnswer        *string  `json:"final_answer"`
    Iterations         int      `json:"iterations"`
    ToolCallsCount     int      `json:"tool_calls_count"`   // 本次事件
    ToolCallsTotal     int      `json:"tool_calls_total"`   // 累计
    TodoItems          int      `json:"todo_items"`
    Success            bool     `json:"success"`            // FinalAnswer != nil
    TrajectoryFile     string   `json:"trajectory_file,omitempty"`
}

type AgentStatus struct { Provider, Model, CurrentDirectory string
                          ToolCallsCount, TodoItems, MCPToolsCount int
                          MCPToolsLoaded bool }
```

---

## 6. `domain/port`：端口签名

```go
type ChatModelPort interface {
    Chat(ctx context.Context, msgs []entity.Message, tools []entity.ToolDefinition,
         opts entity.ChatOptions) (*entity.ChatResponse, error)
    Model() string
    Provider() string
}

type ToolPort interface {
    Definitions() []entity.ToolDefinition
    Has(name string) bool
    Execute(ctx context.Context, name string, args map[string]any) (any, error)
}

type WorkspacePort interface {
    CurrentDirectory() string
    Reset()
}

type MCPPort interface {
    ToolPort
    Load(ctx context.Context) (MCPLoadReport, error)   // 可重复调用即 reload
    ToolNames() []string
    Loaded() bool
    Close() error
}

type MCPLoadReport struct {
    Servers    []MCPServerReport   // 逐 server 成功/失败与原因
    ToolsCount int
}

type TrajectoryPort interface {
    Save(snapshot entity.TrajectorySnapshot) error
    Path() string
}

type Clock interface {
    Now() time.Time
    Advance(d time.Duration)   // 仅模拟时钟有效，真实时钟是 no-op
}

type Logger interface {
    Info(format string, args ...any)
    Warn(format string, args ...any)
    Error(format string, args ...any)
}

type TriggerSource interface {
    Name() string
    Run(ctx context.Context, emit func(entity.Event))   // 阻塞直到 ctx 取消
}

type EventDispatcher interface {
    Dispatch(ctx context.Context, e entity.Event) error
}

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
    BaseURL() string
}

type ArtifactPort interface {
    WriteFile(relative string, data []byte) (ArtifactRef, error)
    AppendLineDurable(relative string, line []byte) (ArtifactRef, error)   // 追加后 fsync
}
type ArtifactRef struct { Path string; Bytes int; SHA256 string }
```

`Clock.Advance` 存在的原因：Python 的 `simulate_time_delay` 会在每轮推进 5 秒的虚拟时间，
让离线演示的时间戳看起来像真的在流逝。真实时钟实现为空操作。

---

## 7. `domain/derr`：哨兵错误与映射

```go
var (
    ErrUnknownEventType        = errors.New("未知的事件类型")
    ErrInvalidTodoStatus       = errors.New("非法的 TODO 状态")
    ErrUnknownTool             = errors.New("未注册的工具")
    ErrInvalidToolArguments    = errors.New("工具参数不是合法 JSON")
    ErrMCPLoading              = errors.New("MCP 工具正在加载")
    ErrAgentNotReady           = errors.New("Agent 尚未初始化")
    ErrMaxIterations           = errors.New("达到 max_iterations 上限")
    ErrAmbiguousClassification = errors.New("邮件分类结果不唯一")
    ErrInvalidMeetingInterval  = errors.New("会议邮件缺少合法的 START_UTC/END_UTC 区间")
    ErrCredentialLeak          = errors.New("证据内容疑似包含凭据")
    ErrMissingCredentials      = errors.New("缺少 UNIPILE_DSN / UNIPILE_ACCESS_TOKEN")
    ErrNoAPIKey                = errors.New("未找到可用的模型 API Key")
)
```

| 错误 | 处理 |
|---|---|
| `ErrUnknownEventType` | HTTP 400 |
| `ErrMCPLoading` | HTTP 409 |
| `ErrAgentNotReady` | HTTP 500 |
| `ErrInvalidTodoStatus` / `ErrUnknownTool` / `ErrInvalidToolArguments` | 不中断循环，作为 `tool` 消息回传给模型 |
| `ErrMaxIterations` | 不算请求失败，`/event` 返回 `success=false` |
| `ErrAmbiguousClassification` / `ErrInvalidMeetingInterval` | 实验 6-1 判 `failed` |
| `ErrMissingCredentials` 与 401 | 实验 6-1 判 `blocked` |
| `ErrCredentialLeak` | 拒绝落盘，不创建文件 |

跨层判断一律 `errors.Is`，禁止字符串比较。

---

## 8. `domain/mailbox`：实验 6-1 纯规则（无 IO）

```go
type ProviderObject = map[string]any
```

provider 返回的对象形状不受我们控制，保留原始 map；脱敏与哈希都在 map 上做，
避免解码进 struct 时静默丢掉未声明字段（证据的完整性依赖这一点）。

### 8.1 解析与分类

```go
func ParseProviderTime(v any) (time.Time, bool)
func EmailText(e ProviderObject) string
func Classify(e ProviderObject) (Classification, error)
func MeetingInterval(e ProviderObject) (start, end time.Time, err error)
func EventOverlaps(ev ProviderObject, start, end time.Time) bool
func ProviderOrder(a, b ProviderObject) bool     // FIFO 排序：先 provider 时间，再 id
```

- `ParseProviderTime` 接受字符串，或 `{date_time|dateTime|datetime|date}` 四选一的对象；
  末尾 `Z` 归一为 `+00:00`；无时区时按 UTC。解析失败返回 `ok=false`，调用方据此跳过。
- `EmailText` 依次尝试 `body_plain`、`body`、`text`，取第一个非空白值。
- `Classify` 三条规则（在 `subject + "\n" + 正文` 的小写文本上匹配）：

  | 分类 | 条件 |
  |---|---|
  | `meeting_invitation` | 含 `meeting invitation` 且含 `start_utc:` 且含 `end_utc:` |
  | `customer_complaint` | 含 `customer complaint` 且匹配 `(?i)order\s*#[a-z0-9-]+` |
  | `marketing` | 含 `marketing` 且（含 `unsubscribe` 或含 `newsletter`） |

  **命中数必须恰好为 1**，否则 `ErrAmbiguousClassification`。这条是实验的防伪措施：
  一封同时像会议又像营销的邮件会让后续工作流的归属变得可争辩。
- `MeetingInterval` 用 `(?i)START_UTC:\s*(\S+)` / `END_UTC:` 提取，要求 `end > start`。
- `EventOverlaps` 用半开区间 `evStart < end && evEnd > start`；
  `is_cancelled` 为真或 `status == "cancelled"` 的事件不算冲突。

### 8.2 脱敏

```go
func Redact(v any) any
func RedactKeyed(v any, key string) any
func SHA256Hex(b []byte) string
func SHA256String(s string) string
```

`RedactKeyed` 的规则按**固定顺序**判断，顺序本身是语义的一部分：

1. 值是字符串且整体匹配 `^[^@\s]+@[^@\s]+\.[^@\s]+$` → `{"sha256": <前20位>, "present": true, "kind": "email_address"}`
2. key（小写）含 `token` / `authorization` / `api_key` / `credential` → `"<redacted>"`
3. key ∈ {`body`,`body_plain`,`text`,`content`} 且值是字符串 → `{"sha256": <全量>, "characters": <长度>}`
4. key == `subject` → 原样保留（实验主题不含个人信息，且要用它证明场景保真）
5. key 以 `_id` 结尾，或 ∈ {`id`,`identifier`,`email`,`from`,`to`}，且值是字符串或整数 → `{"sha256": <前20位>, "present": <非空>}`
6. map → 逐键递归；slice → 逐元素用**父级 key** 递归（与 Python 一致）
7. 其余原样返回

`characters` 用 `utf8.RuneCountInString` 计数，对齐 Python 的 `len(str)`——Python 的 `len`
数的是码点而不是字节。用 `len()` 会让中文正文的字符数对不上。

### 8.3 规范 JSON

```go
func CanonicalJSON(v any) []byte
```

对应 Python 的 `json.dumps(ensure_ascii=False, sort_keys=True, separators=(",", ":"))`：
键排序、无空格、不转义 HTML、非 ASCII 原样输出。Go 的 `encoding/json` 对 map 本来就按键排序，
但默认会把 `<`、`>`、`&` 转成 `\u003c` 等，必须用 `Encoder.SetEscapeHTML(false)` 关掉，
并去掉 `Encode` 自动追加的换行。两边字节一致是「凭据未出现在证据中」这类子串检查成立的前提。

### 8.4 事件与工作流

```go
type CanonicalEvent struct {
    Sequence int            `json:"sequence"`
    Source   EventSource    `json:"source"`
    Channel  string         `json:"channel"`      // 恒为 "unipile_mailbox_poll"
    Content  EventContent   `json:"content"`
    Context  EventContext   `json:"context"`
    ProviderReceiptSHA256 string `json:"provider_receipt_sha256"`
}

type Workflow struct {
    Sequence       int            `json:"sequence"`
    EmailIDSHA256  string         `json:"email_id_sha256"`
    Classification Classification `json:"classification"`
    CalendarCheck  *CalendarCheck `json:"calendar_check,omitempty"`
    Draft          *Draft         `json:"draft,omitempty"`
    Extracted      *Extracted     `json:"extracted,omitempty"`
    Notification   *Notification  `json:"notification,omitempty"`
    Archive        *Archive       `json:"archive,omitempty"`
}
```

Python 用 dict 表示工作流，验收时靠 `.get("calendar_check", {}).get("performed")` 这样的链式
取值——某个环节拼错键名时静默变成 `None`，验收门悄悄判 false 而不报错。Go 改成强类型字段，
拼错键名编译期就失败。JSON 键名保持一致，证据格式不变。

### 8.5 验收门

```go
func DeriveAcceptance(in AcceptanceInput) Acceptance

type AcceptanceInput struct {
    Events           []CanonicalEvent
    Workflows        []Workflow
    Calls            []CallReceipt
    SeedReceipts     []ProviderObject
    CredentialSecret string
    DSNSecret        string
}
type Acceptance struct {
    Status string          `json:"status"`   // passed / failed
    Gates  []Gate          `json:"-"`
    GatesJSON map[string]bool `json:"gates"`
}
```

10 个门，名称与判定逐字沿用 Python：

| 门 | 判定 |
|---|---|
| `three_real_inbound_unipile_events` | 恰好 3 个事件，均 provider=unipile、channel=unipile_mailbox_poll、有回执哈希 |
| `fifo_event_queue` | 事件与工作流的 sequence 均为 `[0,1,2]` |
| `exact_three_scenario_classifications` | 三种分类齐全且工作流数为 3 |
| `meeting_calendar_checked_and_reply_drafted` | 日历已查、冲突判定有值、草稿 > 100 字节、哈希 64 位 |
| `complaint_extracted_and_high_priority_notification_delivered` | 订单号非空、需人工跟进、priority=high、已投递、文件哈希 64 位 |
| `marketing_archived_and_verified_through_provider` | `update_object == "EmailUpdated"` 且回读校验通过 |
| `required_unipile_calls_succeeded` | 调用非空且全部 success |
| `three_seed_messages_sent_through_unipile` | 3 条 seed 回执 |
| `credentials_and_dsn_absent_from_receipts` | token 与 DSN 均未出现在规范 JSON 中 |
| `no_simulation_markers_in_control_plane` | 去掉 `response_sha256` 后的 calls 不含 `mock/placeholder/synthetic/simulated` |

`Gates` 保留切片形式是为了输出顺序确定；`GatesJSON` 是落盘用的 map，Go 的 map 序列化按键
排序，与 Python 的 dict 插入序不同——这只影响 `summary.json` 里 gates 的排列，不影响任何判定。

### 8.6 测试点

Python 的 4 个离线契约测试全部移植：

1. 分类唯一 + 会议区间精确（含歧义邮件必须报错）。
2. 401 被记录成回执并抛错，且密钥不出现在回执里。
3. 三封邮件乱序输入，FIFO 队列按 provider 时间排序；验收 10 项全通过；草稿 > 100 字节。
4. 归档回读校验置 false 时，验收转为 `failed` 且对应门为 false。

补充：脱敏 6 条规则的表驱动测试、`CanonicalJSON` 的字节断言（含非 ASCII 与 `&`）。

---

## 9. `application/agent`：核心算法

### 9.1 结构

```go
type EventAgentService struct {
    chat       port.ChatModelPort
    builtin    port.ToolPort
    mcp        port.MCPPort        // 可为 nil
    workspace  port.WorkspacePort
    trajectory port.TrajectoryPort // 可为 nil
    clock      port.Clock
    log        port.Logger
    cfg        entity.SystemHintConfig
    opts       entity.ChatOptions
    maxIter    int

    runMu   sync.Mutex      // 串行化整个事件处理
    stateMu sync.RWMutex    // 保护下面的状态
    history   []entity.Message
    todos     entity.TodoList
    counter   entity.ToolCounter
    records   []entity.ToolCallRecord
    processes entity.ProcessRegistry
    lastUserInteraction time.Time
}
```

### 9.2 `HandleEvent` 算法

```
runMu.Lock(); defer Unlock()

msg := event.ToUserMessage()
若 cfg.EnableTimestamps: msg = "[" + 当前时间戳 + "] " + msg
若 event.Type.IsUserInteraction(): lastUserInteraction = clock.Now()
history += {role: user, content: msg}
baseToolCalls := len(records)        // 用于算「本次事件」的工具调用数

for iter := 1; iter <= maxIter; iter++ {
    clock.Advance(5s)                // 仅模拟时钟有效
    saveTrajectory(iter, nil)

    send := copy(history)
    若 systemHint != "": send += {role: user, content: systemHint}   // 不写回 history

    resp, err := chat.Chat(ctx, send, activeTools(), opts)
    err != nil → 保存轨迹并返回错误

    assistant := resp.Choices[0].Message
    history += assistant.ToAssistantMessage()

    若 len(assistant.ToolCalls) == 0:
        content != "" → finalAnswer = ExtractFinalAnswer(content)
        content == "" → 记 warn（空响应且无工具调用，继续循环只会白烧轮次）
        break

    for _, tc := range assistant.ToolCalls:
        args, parseErr := 解析 tc.Function.Arguments
        若 parseErr != nil:
            记录错误 record；回填 {role: tool, tool_call_id: tc.ID, content: {"error": ...}}
            continue                 // 关键：不能 return，否则这个 tool_call_id 永远没有响应
        n := counter.Next(name, cfg.EnableToolCounter)
        result, execErr := executeTool(ctx, name, args)
        记录 record（含 DurationMs）
        回填 {role: tool, tool_call_id: tc.ID, content: prefix + JSON(result)}

    若 ExtractFinalAnswer(assistant.Content).marked:
        finalAnswer = 提取结果; break       // 工具调用与 FINAL ANSWER 同轮出现（罕见）
}

saveTrajectory(iter, finalAnswer)
return EventResult{...}
```

两处容易写错的地方：

- **`send := copy(history)`** 必须是真拷贝。直接 `append(history, hint)` 在 cap 足够时会
  写进 history 的底层数组，下一轮再 append 就会覆盖掉 hint 位置上的真实消息。
- **参数解析失败后 `continue` 而不是 `return`**：assistant 消息（含 `tool_calls`）已经进了
  history；若此时返回，这个 `tool_call_id` 就永远没有对应的 `tool` 响应，之后每一次请求都会
  被 provider 以 400 拒绝，Agent 彻底卡死。Python 版有这个修复，Go 版必须保留。

### 9.3 工具集与执行分派

```go
func (s *EventAgentService) activeTools() []entity.ToolDefinition {
    // MCP 已加载 → MCP 工具；否则 → 内置工具
    // 两种情况下都追加 TODO 工具（若 cfg.EnableTodoList）
}

func (s *EventAgentService) executeTool(ctx, name, args) (any, error) {
    switch {
    case name == "rewrite_todo_list"  : 应用层处理
    case name == "update_todo_status" : 应用层处理
    case s.mcp != nil && s.mcp.Has(name): s.mcp.Execute(...)
    case s.builtin.Has(name)          : s.builtin.Execute(...)
    default                           : derr.ErrUnknownTool
    }
}
```

TODO 工具不下沉到 `ToolPort`：它操作的是 Agent 自己的状态，不是外部能力。放进 `ToolPort`
会迫使工具适配器反向依赖应用层状态。

### 9.4 System hint

```
=== SYSTEM STATE ===
Current Time: 2026-10-12 10:30:00
Current Directory: /Users/x/work
System: darwin (25.6.0)
Shell Environment: macOS Terminal (zsh/bash)
Go Version: go1.26.0

=== CURRENT TASKS ===
TODO List:
  [1] 🔄 读取配置 (in_progress)
```

两个开关都关闭、或 TODO 为空且系统状态关闭时返回空串，此时不追加 hint 消息。
Python 输出的是 `Python Version`，Go 对应输出 `Go Version`——这是运行时事实的如实呈现。

### 9.5 错误详情

`cfg.EnableDetailedErrors` 打开时，工具错误渲染为：

```
Tool 'read_file' failed with *fs.PathError: open /nope: no such file or directory
Arguments: {
  "file_path": "/nope"
}
Suggestions: Verify the file/directory path exists | Check the current working directory | ...
```

建议表按错误文本关键词匹配（permission / not found / syntax / timeout / import），
与 Python 的 `_get_error_suggestions` 对齐。

### 9.6 测试点

| 用例 | 断言 |
|---|---|
| 无工具调用直接回答 | 1 轮结束，`Success=true` |
| 带 `FINAL ANSWER:` | 取标记之后的内容 |
| 一轮工具调用后回答 | 2 轮，history 含 assistant+tool，`ToolCallsCount=1` |
| 模型永远请求工具 | 到达 `maxIter`，`Success=false` |
| 工具参数是坏 JSON | 仍回填 tool 消息，`tool_call_id` 全部有响应 |
| system hint | 不出现在 `history`，但出现在发给模型的消息里 |
| 两次事件 | 工具计数累积；`Reset()` 后归零 |
| 空响应且无工具调用 | 立刻结束，不烧满 `maxIter` |

---

## 10. `application/monitor`：超时监控

```go
type Monitor struct {
    agent     AgentPort       // LastUserInteraction / OverdueProcesses / MarkProcessReminded
    dispatch  port.EventDispatcher
    clock     port.Clock
    cfg       Config          // Interval / UserTimeout / ProcessTimeout
}
func (m *Monitor) Run(ctx context.Context)
func (m *Monitor) Tick(ctx context.Context)    // 单步，供测试直接驱动
```

去重规则：

- **用户超时**：空闲超过阈值时发一次 `user_timeout`，随后置 `userReminded=true`；
  一旦 `LastUserInteraction` 前进（用户又说话了）就重置为 false。不重置的话只会提醒一次；
  每次 tick 都提醒则会刷屏。
- **进程超时**：每个进程只提醒一次，由 `ProcessRegistry.MarkReminded` 记录。

`Tick` 单独暴露，测试用受控时钟直接调用，不依赖真实 sleep。

---

## 11. `application/eventloop`：事件循环

```go
type EventLoop struct {
    queue    chan entity.Event
    dispatch port.EventDispatcher
    triggers []port.TriggerSource
    log      port.Logger
    processed atomic.Int64
}
func (l *EventLoop) AddTrigger(t port.TriggerSource)
func (l *EventLoop) Run(ctx context.Context) error     // 阻塞到 ctx 取消且队列排空
func (l *EventLoop) Processed() int64
```

- 所有触发器并发运行，事件推进同一个带缓冲 channel；单个 goroutine 消费，分发天然串行。
- 单个事件的处理错误只记日志，不终止循环——一个事件失败不该让整个系统停摆。
- `ctx` 取消后先停触发器，再把队列里剩余事件排空（给 `drainTimeout` 的预算），然后返回。

---

## 12. `application/mailbox`：实验 6-1 编排

```go
func (s *ExperimentService) Run(ctx context.Context, in RunInput) (Summary, error)
```

```
1. 读 protocol.json，抓取官方 schema 回执（失败只记录，不致命）
2. 校验凭据 → 缺失则 status=blocked 直接落盘
3. ListAccounts → 选 listener / sender / calendar 账号 → 推断收件地址
4. 发 3 封带 campaign 标记的邮件
5. 轮询收件箱直到 3 封齐（超时则 failed），跳过 role=sent 的回声
6. 按 ProviderOrder 排 FIFO，逐封 Classify 并执行工作流
7. DeriveAcceptance → summary.json / manifest.json / latest.json
```

无论成功失败，`defer` 都要写出 summary 与 manifest——**失败的运行同样是证据**，
这是 Python 版 `finally` 块的语义，必须保留。

---

## 13. 跨层错误处理约定

- 领域与应用层返回 `error`，用 `%w` 包装保留链。
- `interfaces` 层负责把错误映射成 HTTP 状态码或进程退出码，是唯一做这个映射的地方。
- 判断一律 `errors.Is` / `errors.As`。
