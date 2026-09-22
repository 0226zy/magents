package react

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/port"
)

// SystemPrompt 对应 agent.py:299-314。
const SystemPrompt = `你是 Kimi，一个智能搜索助手。

请按照以下步骤处理：
1. 分析用户问题，识别关键信息需求
2. 使用 web_search 官方工具搜索相关信息
3. 如果需要更多信息，可以多次调用搜索工具
4. 综合所有信息，生成准确、全面的答案

注意：
- 搜索时使用精准的关键词
- 优先获取最新、最权威的信息
- 答案要结构清晰，有理有据
`

// DefaultMaxParallelTools 是单轮内并发执行工具的上限。
// 实测一轮最多 5 次并发搜索；无限并发在更大的任务上会触发 429，
// 而 429 会级联成超时。
const DefaultMaxParallelTools = 5

// Options 控制一个 Agent 的行为。
type Options struct {
	// ReuseDeclaration 决定工具声明的缓存作用域。
	//
	// 默认 false，与 Python 版一致：每个问题重新拉取一次声明，让每个独立问题
	// 留下自己的真实凭据（验收断言 one_real_formula_declaration_fetch 要求恰好
	// 一次）。批量场景可显式放宽，但实验路径必须保持默认。
	ReuseDeclaration bool
	// MaxParallelTools 是单轮内并发执行工具的上限，零值取 DefaultMaxParallelTools。
	MaxParallelTools int
	// Timeout 是单次 provider 调用的预算，零值表示不额外设限。
	Timeout time.Duration
	// Verbose 打开后实时打印 ReAct 轨迹。
	Verbose bool
	// Output 是 verbose 模式的输出目标，零值为 os.Stdout。
	Output io.Writer
	// Recorder 在每个问题开始时被清空，与 Python 版重置 api_turns 的语义一致。
	Recorder *entity.Recorder
	// NoSearch 对应 OpenRouter 兜底通道：Formula 工具不可用，模型仅凭自身知识作答。
	NoSearch bool
}

// Agent 不是并发安全的：Run 会重置内部状态。
// 批量场景请为每个问题创建独立 Agent，共享底层 *http.Client 即可。
type Agent struct {
	chat    port.ChatCaller
	formula port.FormulaRunner
	options Options

	history []entity.Message
	trace   []Step
	tools   []json.RawMessage
}

// New 构造一个 Agent。
func New(chat port.ChatCaller, formula port.FormulaRunner, options Options) *Agent {
	if options.MaxParallelTools <= 0 {
		options.MaxParallelTools = DefaultMaxParallelTools
	}
	if options.Output == nil {
		options.Output = os.Stdout
	}
	return &Agent{chat: chat, formula: formula, options: options}
}

// History 返回本次运行的消息历史副本。
func (a *Agent) History() []entity.Message {
	return append([]entity.Message(nil), a.history...)
}

// Trace 返回本次运行的 ReAct 轨迹副本。
func (a *Agent) Trace() []Step {
	return append([]Step(nil), a.trace...)
}

// Run 执行一次完整的 ReAct 循环。
//
// 返回值与 Python 版 search_and_answer 语义对齐：永不返回 error，
// 失败以兜底文案 + FailureKind 表达。
func (a *Agent) Run(ctx context.Context, question string, maxIterations int) Result {
	a.reset(question)

	for iteration := 1; iteration <= maxIterations; iteration++ {
		response, err := a.callChat(ctx)
		if err != nil {
			return a.providerFailure(err)
		}
		choice := response.Choices[0]

		if choice.Message.ReasoningContent != "" {
			a.emit(Step{Iteration: iteration, Type: StepThought, Content: choice.Message.ReasoningContent})
		}

		switch choice.FinishReason {
		case "tool_calls":
			a.history = append(a.history, choice.Message.ToAssistantMessage())
			if err := a.runToolCalls(ctx, iteration, choice.Message.ToolCalls); err != nil {
				return a.providerFailure(err)
			}
			// 继续下一轮迭代

		case "length":
			// 输出预算（max_tokens）耗尽导致截断：返回已生成内容并明确标注，
			// 而不是把半截答案当作完整答案，也不误报「无法获取足够信息」
			// （content 为空时，思考过程已耗尽整个预算）。
			return a.truncatedAnswer(iteration, choice.Message.Content)

		default: // "stop" 等
			if choice.Message.Content != "" {
				return a.finalAnswer(iteration, choice.Message.Content)
			}
			// content 为空时 Python 版会退出 while 循环并返回 NO_INFO_MESSAGE，
			// 而不是继续迭代。这里复现同一行为。
			return Result{Answer: NoInfoMessage, Failure: FailureNoInfo, Trace: a.Trace()}
		}
	}
	return Result{Answer: MaxIterationsMessage, Failure: FailureMaxIterations, Trace: a.Trace()}
}

func (a *Agent) reset(question string) {
	a.history = []entity.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: question},
	}
	a.trace = nil
	if !a.options.ReuseDeclaration {
		a.tools = nil
	}
	if a.options.Recorder != nil {
		a.options.Recorder.Reset()
	}
}

func (a *Agent) emit(step Step) {
	a.trace = append(a.trace, step)
	if a.options.Verbose {
		fmt.Fprintln(a.options.Output, FormatTraceStep(step, DefaultTraceMaxLen))
	}
}

func (a *Agent) providerFailure(err error) Result {
	answer := fmt.Sprintf("%s: %s%s", SearchErrorPrefix, err, errorHint(err, a.options.Timeout))
	return Result{Answer: answer, Failure: FailureProvider, Trace: a.Trace()}
}

func (a *Agent) finalAnswer(iteration int, content string) Result {
	a.emit(Step{Iteration: iteration, Type: StepAnswer, Content: content})
	a.history = append(a.history, entity.Message{Role: "assistant", Content: content})
	return Result{Answer: content, Trace: a.Trace()}
}

func (a *Agent) truncatedAnswer(iteration int, content string) Result {
	partial := strings.TrimSpace(content)
	final := truncationNote
	if partial != "" {
		final = partial + "\n\n" + truncationNote
	}
	a.emit(Step{Iteration: iteration, Type: StepAnswer, Content: final})
	// 存入历史时保留截断提示，否则复用历史时可能把不完整回答当作普通回答。
	a.history = append(a.history, entity.Message{Role: "assistant", Content: final})
	return Result{Answer: final, Failure: FailureTruncated, Trace: a.Trace()}
}

func (a *Agent) callChat(ctx context.Context) (*entity.ChatResponse, error) {
	tools, err := a.ensureTools(ctx)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := a.withTimeout(ctx)
	defer cancel()
	return a.chat.Chat(callCtx, a.history, tools)
}

// ensureTools 拉取（或复用）provider 的权威工具声明。
func (a *Agent) ensureTools(ctx context.Context) ([]json.RawMessage, error) {
	if a.options.NoSearch {
		return nil, nil
	}
	if a.tools != nil {
		return a.tools, nil
	}
	callCtx, cancel := a.withTimeout(ctx)
	defer cancel()
	tools, err := a.formula.Tools(callCtx)
	if err != nil {
		return nil, err
	}
	a.tools = tools
	return tools, nil
}

func (a *Agent) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if a.options.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, a.options.Timeout)
}

// runToolCalls 并发执行同一轮里的全部 web_search，但按原顺序回填历史。
//
// 顺序很关键：tool_call_id 与内容必须配对，且验收断言
// fiber_requests_match_model_actions 要求 fiber 数与行动数严格相等。
//
// 轨迹上 action 步骤先全部发出、observation 步骤后全部发出，与 Python 版
// 「action→observation 交替」不同。这是并行化带来的唯一轨迹变更：18 项断言
// 只看步骤类型的存在性、tool 字段与 iteration 分组，不看交错顺序。
func (a *Agent) runToolCalls(ctx context.Context, iteration int, calls []entity.ToolCall) error {
	for _, call := range calls {
		a.emit(Step{
			Iteration: iteration,
			Type:      StepAction,
			Tool:      call.Function.Name,
			Args:      parseArgsLenient(call.Function.Arguments),
		})
	}

	// results 用索引赋值而非 channel 收集，顺序必须与 calls 一致。
	results := make([]string, len(calls))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(a.options.MaxParallelTools)
	for i, call := range calls {
		i, call := i, call
		group.Go(func() error {
			if call.Function.Name != "web_search" {
				results[i] = fmt.Sprintf("Error: unable to find tool by name '%s'", call.Function.Name)
				return nil
			}
			callCtx, cancel := a.withTimeout(groupCtx)
			defer cancel()
			// 原始 arguments 字符串，不是 parseArgsLenient 的结果。
			output, err := a.formula.ExecuteFiber(callCtx, call.Function.Name, call.Function.Arguments)
			if err != nil {
				return err
			}
			results[i] = output
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}

	for i, call := range calls {
		a.emit(Step{
			Iteration: iteration,
			Type:      StepObservation,
			Tool:      call.Function.Name,
			Content:   results[i],
		})
		a.history = append(a.history, entity.Message{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    results[i],
		})
	}
	return nil
}

// parseArgsLenient 复现 agent.py:436-446：
// 模型偶尔生成非法 JSON（如尾随逗号），此时按空对象继续，不中断 ReAct 循环。
// 返回值只用于生成可读轨迹；发给 Formula 的永远是原始字符串。
func parseArgsLenient(raw string) map[string]any {
	if raw == "" {
		raw = "{}"
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return map[string]any{}
	}
	if parsed == nil {
		return map[string]any{}
	}
	return parsed
}
