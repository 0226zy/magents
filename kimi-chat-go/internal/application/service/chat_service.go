// Package service 实现单次 chat completion 问答的用例编排：
// 组装 system + user 消息 → 调模型 → 取助手回答。
package service

import (
	"context"

	"github.com/0226zy/magents/kimi-chat-go/internal/domain/derr"
	"github.com/0226zy/magents/kimi-chat-go/internal/domain/entity"
	"github.com/0226zy/magents/kimi-chat-go/internal/domain/port"
)

// ChatService 是「问一个问题」的用例。
type ChatService struct {
	chat port.ChatModelPort
}

// NewChatService 构造应用服务。
func NewChatService(chat port.ChatModelPort) *ChatService {
	return &ChatService{chat: chat}
}

// Execute 携带 system + user 两条消息调用一次模型，返回助手回答。
func (s *ChatService) Execute(ctx context.Context, systemPrompt, userPrompt string) (*entity.ChatResponse, error) {
	resp, err := s.chat.Chat(ctx, []entity.Message{
		{Role: "system", Content: systemPrompt}, // ← Written by developer
		{Role: "user", Content: userPrompt},     // ← User input
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, derr.ErrNoChoices
	}
	return resp, nil
}
