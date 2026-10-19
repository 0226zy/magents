// Package agent 实现 Agent 核心循环（dsh Control 层的等价物，指引 §2.1）。
//
// 关键不变量：
//   - 上下文 = 静态前缀（系统提示词 + 工具定义，逐字节稳定）+ 状态栏（追加在末尾）+ 轨迹；
//   - 每个 tool_call 必有配对的 tool result 消息（含错误形态）；
//   - 循环唯一正常终止条件是模型不再发起工具调用；
//     轮数 / 预算 / 活性 / 熔断均为 Harness 保护性终止；
//   - 工具层错误不终止会话，作为普通工具结果回注（指引 §4.3）。
package agent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/0226zy/magents/dsh-go/internal/domain/derr"
	"github.com/0226zy/magents/dsh-go/internal/domain/entity"
	"github.com/0226zy/magents/dsh-go/internal/domain/port"
)

// Limits 是循环的保护性终止与恢复阈值，全部来自配置（指引 §4.5：
// 每条恢复路径必须有明确熔断上限）。
type Limits struct {
	MaxIterations          int
	TokenBudget            int
	LivelockRepeat         int
	MaxRetries             int
	CircuitBreakerFailures int
}

// ErrorClassifier 判别模型调用错误是否可重试，并给出服务端建议等待时长。
// 由组合根注入（openai 适配器的 Retryable/RetryAfterHint），
// 使 application 层不依赖具体厂商（指引 §8 纪律三：保留中立层）。
type ErrorClassifier func(err error) (retryable bool, retryAfter time.Duration)

// Loop 是 Agent 循环驱动器。本身无状态，可并发服务多个会话。
// 模型按会话解析（llmFor）：不同会话可选用不同模型（LLMInfoRepo 资源）。
type Loop struct {
	llmFor       port.LLMResolver
	tools        port.ToolRegistryPort
	hooks        []port.Hook
	statusBar    port.StatusBarPort
	systemPrompt string
	limits       Limits
	classify     ErrorClassifier
}

// NewLoop 构造循环驱动器。
func NewLoop(llmFor port.LLMResolver, tools port.ToolRegistryPort, hooks []port.Hook, statusBar port.StatusBarPort, systemPrompt string, limits Limits, classify ErrorClassifier) *Loop {
	return &Loop{llmFor: llmFor, tools: tools, hooks: hooks, statusBar: statusBar, systemPrompt: systemPrompt, limits: limits, classify: classify}
}

// RunTurn 执行一个回合：把 userText 追加进轨迹后运行 ReAct 循环，
// 直到模型不再调用工具（返回最终回答）或触发保护性终止（返回错误）。
// 所有中间事件（assistant 消息、工具结果、Harness 提示）实时追加进会话日志。
func (l *Loop) RunTurn(ctx context.Context, sess port.SessionRuntime, userText string) (string, error) {
	if _, err := sess.AppendEvent(entity.Event{
		Kind:    entity.EventMessage,
		Message: &entity.Message{Role: "user", Content: userText},
	}); err != nil {
		return "", err
	}

	// 回合开始时解析一次模型：回合内不切换（切换应开新会话或 fork）
	llm, err := l.llmFor(sess.Model())
	if err != nil {
		l.notice(sess, fmt.Sprintf("模型解析失败: %v", err))
		return "", err
	}

	var lastFP string
	consecutiveFP := 0
	consecutiveModelFailures := 0

	for iter := 1; ; iter++ {
		if iter > l.limits.MaxIterations {
			l.notice(sess, fmt.Sprintf("已达最大迭代轮数 %d，回合被保护性终止。如需继续，请发送新消息让 Agent 接着做。", l.limits.MaxIterations))
			return "", derr.ErrMaxIterations
		}

		// 1. 组装上下文：静态前缀 + 状态栏 + 轨迹（指引 §2.1）
		messages := l.buildContext(ctx, sess)

		// 2. 模型决策：可重试错误走静默分级恢复（指引 §4.3）
		resp, err := l.chatWithRecovery(ctx, sess, llm, messages, &consecutiveModelFailures)
		if err != nil {
			l.notice(sess, fmt.Sprintf("模型调用失败（已尝试自动恢复）: %v", err))
			return "", err
		}
		consecutiveModelFailures = 0
		assistant := resp.Choices[0].Message

		// 模型回复（无论文本还是工具调用）都追加进轨迹
		if _, err := sess.AppendEvent(entity.Event{Kind: entity.EventMessage, Message: &assistant}); err != nil {
			return "", err
		}

		// 预算终止：token 累计超上限
		total := sess.AddTokens(resp.Usage.TotalTokens)
		// 进度 marker：不进入模型上下文（零 token 成本），仅供 SSE/UI 展示（评审 P1）。
		l.progress(sess, fmt.Sprintf("第 %d 轮 · 模型 %s · 本轮 %d tokens · 累计 %d", iter, llm.Model(), resp.Usage.TotalTokens, total))
		if total > l.limits.TokenBudget {
			l.notice(sess, fmt.Sprintf("会话 token 累计 %d，超出预算 %d，回合被保护性终止。", total, l.limits.TokenBudget))
			return "", derr.ErrBudgetExhausted
		}

		// 3. 唯一正常终止：模型不再调用工具
		if len(assistant.ToolCalls) == 0 {
			return assistant.Content, nil
		}

		// 4. 校验（PreToolUse 钩子：审批/拦截）并执行工具
		results := l.executeCalls(ctx, sess, assistant.ToolCalls)

		// 5. 结果回注 + 无进展检测（重复调用指纹，指引 §4.2）
		for i, tc := range assistant.ToolCalls {
			if _, err := sess.AppendEvent(entity.Event{
				Kind:    entity.EventMessage,
				Message: &entity.Message{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: results[i].Content},
			}); err != nil {
				return "", err
			}

			fp := fingerprint(tc)
			if fp == lastFP {
				consecutiveFP++
			} else {
				consecutiveFP = 1
				lastFP = fp
			}
		}
		if consecutiveFP == l.limits.LivelockRepeat {
			// 第一次触阈：把警告作为模型输入，给它自我纠正的机会
			l.notice(sess, fmt.Sprintf(
				"[Harness 警告] 检测到相同工具调用已连续出现 %d 次，疑似无进展循环。请改变策略：换工具、换参数，或先总结已知信息再决定下一步。",
				consecutiveFP))
		}
		if consecutiveFP > l.limits.LivelockRepeat+1 {
			l.notice(sess, "无进展循环未自行纠正，回合被强制打断。")
			return "", derr.ErrLivelock
		}
	}
}

// buildContext 组装上下文：静态前缀（系统提示词）+ 状态栏 + 轨迹。
// 状态栏作为最后一条 system 消息追加，不进入事件日志、不改写静态前缀。
func (l *Loop) buildContext(ctx context.Context, sess port.SessionRuntime) []entity.Message {
	messages := make([]entity.Message, 0, 64)
	messages = append(messages, entity.Message{Role: "system", Content: l.systemPrompt})
	if l.statusBar != nil {
		messages = append(messages, entity.Message{Role: "system", Content: l.statusBar.Snapshot(ctx, sess.Cwd())})
	}
	return append(messages, sess.Messages()...)
}

// chatWithRecovery 是模型调用的恢复路径（指引 §4.3 分级恢复第一级：静默重试）。
// 指数退避 + 随机抖动，尊重服务端 Retry-After；恢复期间不向用户暴露任何错误
// （核心边界原则）；连续失败达到熔断阈值即放弃（指引 §4.5）。
func (l *Loop) chatWithRecovery(ctx context.Context, sess port.SessionRuntime, llm port.LLMPort, messages []entity.Message, consecutiveFailures *int) (*entity.ChatResponse, error) {
	base := time.Second
	for attempt := 0; ; attempt++ {
		resp, err := llm.Chat(ctx, messages, l.tools.Definitions())
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		retryable, hint := l.classify(err)
		if !retryable {
			return nil, fmt.Errorf("%w: %v", derr.ErrNotRetryable, err)
		}
		*consecutiveFailures++
		if *consecutiveFailures >= l.limits.CircuitBreakerFailures {
			return nil, fmt.Errorf("%w（模型调用连续失败 %d 次）: %v", derr.ErrCircuitOpen, *consecutiveFailures, err)
		}
		if attempt >= l.limits.MaxRetries {
			return nil, fmt.Errorf("静默重试 %d 次后仍失败: %w", attempt, err)
		}

		wait := base << uint(attempt)
		if wait > 30*time.Second {
			wait = 30 * time.Second
		}
		wait += time.Duration(rand.Int63n(int64(wait/4 + 1))) // 抖动
		if hint > wait {
			wait = hint // 尊重服务端等待时长提示
		}
		l.notice(sess, fmt.Sprintf("模型调用遇到可重试错误，%s 后自动重试（第 %d/%d 次）……", wait.Round(time.Second), attempt+1, l.limits.MaxRetries))

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// executeCalls 执行一批工具调用：全部声明并发安全时并行执行（指引 §2.4），
// 否则串行。每个调用先过 PreToolUse 钩子（审批/拦截），执行后过 PostToolUse。
func (l *Loop) executeCalls(ctx context.Context, sess port.SessionRuntime, calls []entity.ToolCall) []entity.ToolResult {
	results := make([]entity.ToolResult, len(calls))

	parallel := len(calls) > 1
	for _, tc := range calls {
		tool, ok := l.tools.Lookup(tc.Function.Name)
		if !ok || !tool.ConcurrencySafe() {
			parallel = false
			break
		}
	}

	var wg sync.WaitGroup
	for i, tc := range calls {
		if parallel {
			wg.Add(1)
			go func(i int, tc entity.ToolCall) {
				defer wg.Done()
				results[i] = l.executeOne(ctx, sess, tc)
			}(i, tc)
		} else {
			results[i] = l.executeOne(ctx, sess, tc)
		}
	}
	wg.Wait()
	return results
}

// executeOne 执行单个工具调用：幻觉调用、参数畸形、钩子拦截、执行异常
// 全部转化为普通工具结果（指引 §4.3：错误是模型的输入，不是会话的终点）。
func (l *Loop) executeOne(ctx context.Context, sess port.SessionRuntime, tc entity.ToolCall) entity.ToolResult {
	tctx := &port.ToolContext{
		SessionID: sess.ID(),
		Cwd:       sess.Cwd,
		Model:     sess.Model(),
		Approve: func(ctx context.Context, toolName, summary, detail string) entity.ApprovalDecision {
			return sess.Approve(ctx, toolName, summary, detail)
		},
		Spill: func(toolName, content string) string {
			return sess.Spill(toolName, content)
		},
		Emit: func(ev entity.Event) {
			_, _ = sess.AppendEvent(ev)
		},
	}

	tool, ok := l.tools.Lookup(tc.Function.Name)
	if !ok {
		// 幻觉调用：告知可用工具清单，模型下一轮自行纠正
		return entity.ToolResult{IsError: true, Content: fmt.Sprintf(
			"工具 %q 不存在。可用工具：%v。请改用上述工具之一。", tc.Function.Name, l.tools.Names())}
	}

	for _, h := range l.hooks {
		if reason := h.PreToolUse(ctx, tc.Function.Name, tc.Function.Arguments, tctx); reason != "" {
			return entity.ToolResult{IsError: true, Content: reason}
		}
		if ctx.Err() != nil {
			return entity.ToolResult{IsError: true, Content: "回合已被取消"}
		}
	}

	start := time.Now()
	result := tool.Execute(ctx, tc.Function.Arguments, tctx)
	// 工具耗时 marker（含审批等待）：不进入模型上下文，供 SSE/UI 展示（评审 P1）。
	l.progress(sess, fmt.Sprintf("工具 %s · 耗时 %s", tc.Function.Name, time.Since(start).Round(time.Millisecond)))
	for _, h := range l.hooks {
		h.PostToolUse(ctx, tc.Function.Name, tc.Function.Arguments, tctx, &result)
	}
	return result
}

// fingerprint 计算「工具名 + 参数」指纹，用于无进展循环检测（指引 §4.2）。
func fingerprint(tc entity.ToolCall) string {
	sum := sha1.Sum([]byte(tc.Function.Name + "\x00" + tc.Function.Arguments))
	return hex.EncodeToString(sum[:8])
}

// notice 把 Harness 提示追加进轨迹（以普通文本身份进入模型上下文）。
func (l *Loop) notice(sess port.SessionRuntime, text string) {
	_, _ = sess.AppendEvent(entity.Event{Kind: entity.EventNotice, Note: text})
}

// progress 追加一条进度 marker：不进入模型上下文投影（零 token 成本），
// 仅供 SSE/UI 实时展示回合进度与工具耗时。
func (l *Loop) progress(sess port.SessionRuntime, text string) {
	_, _ = sess.AppendEvent(entity.Event{Kind: entity.EventMarker, Marker: &entity.Marker{Type: "progress", Detail: text}})
}
