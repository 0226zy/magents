// Package service 实现 Agent 核心循环应用服务（书中 2.2.4 节）：
//
//	for {
//	    response  := chat.Chat(messages, tools)   // 带完整上下文调用模型
//	    messages   = append(messages, assistant)  // 模型回复（文本或工具调用）入列
//	    if 无工具调用 { 得到最终答案，退出 }
//	    for 每个工具调用 {
//	        result := tools.Execute(name, args)          // 执行工具
//	        messages = append(messages, tool 结果)        // 结果入列
//	    }
//	    // 回到循环顶部，带着更新的消息列表再调用模型
//	}
//
// 与书一样，循环带 maxIterations 上限，防止模型反复请求同一工具陷入死循环。
package service

import (
	"context"
	"fmt"

	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/port"
)

// AgentService 是「问一个问题」的用例：持有端口，运行核心循环。
type AgentService struct {
	chat          port.ChatModelPort
	tools         port.ToolPort
	trajectory    port.TrajectoryPort
	rawLog        port.RawLogPort // 可为 nil：不记录原始响应
	maxIterations int
}

// NewAgentService 构造应用服务。maxIterations <= 0 时取 8。
func NewAgentService(chat port.ChatModelPort, tools port.ToolPort, trajectory port.TrajectoryPort, rawLog port.RawLogPort, maxIterations int) *AgentService {
	if maxIterations <= 0 {
		maxIterations = 8
	}
	return &AgentService{chat: chat, tools: tools, trajectory: trajectory, rawLog: rawLog, maxIterations: maxIterations}
}

// Execute 从 system + user 消息出发运行核心循环，返回模型的最终回答。
func (s *AgentService) Execute(ctx context.Context, systemPrompt, userQuestion string) (string, error) {
	messages := []entity.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userQuestion},
	}
	s.trajectory.Event("=== 用户问题: %s", userQuestion)
	s.trajectory.EventJSON("发给模型的工具声明 (tools)", s.tools.Definitions())

	for iter := 1; iter <= s.maxIterations; iter++ {
		s.trajectory.Event("--- 第 %d/%d 轮: 调用模型（messages=%d）", iter, s.maxIterations, len(messages))
		// 记录本轮发给模型的完整上下文：system / user / 历史 assistant / tool 结果
		s.trajectory.EventJSON(fmt.Sprintf(">>> 第 %d 轮发给模型的完整 messages", iter), messages)

		resp, err := s.chat.Chat(ctx, messages, s.tools.Definitions())
		if err != nil {
			return "", fmt.Errorf("第 %d 轮调用模型失败: %w", iter, err)
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("第 %d 轮响应中没有 choices", iter)
		}
		assistant := resp.Choices[0].Message

		// 原始响应单独落盘（不进终端），便于事后核对协议细节
		if s.rawLog != nil && len(resp.Raw) > 0 {
			s.rawLog.LogResponse(iter, resp.Raw)
		}

		// 模型回复（无论文本还是工具调用）都追加进消息列表
		messages = append(messages, assistant)
		if assistant.Content != "" {
			s.trajectory.Event("<<< 模型回复: %s", assistant.Content)
		}

		// 没有工具调用 → 模型已给出最终回答，退出循环
		if len(assistant.ToolCalls) == 0 {
			s.trajectory.Event("=== 循环结束: finish_reason=%s, tokens(prompt=%d completion=%d total=%d)",
				resp.Choices[0].FinishReason,
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
			return assistant.Content, nil
		}

		// 逐个执行模型请求的工具调用，把结果追加进消息列表
		for _, tc := range assistant.ToolCalls {
			s.trajectory.Event(">>> 模型请求工具: %s(%s)", tc.Function.Name, tc.Function.Arguments)
			result, err := s.tools.Execute(tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				return "", fmt.Errorf("第 %d 轮执行工具 %s 失败: %w", iter, tc.Function.Name, err)
			}
			s.trajectory.Event("<<< 工具结果: %s", result)
			messages = append(messages, entity.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	return "", derr.ErrMaxIterations
}
