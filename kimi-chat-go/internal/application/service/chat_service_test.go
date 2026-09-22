package service

import (
	"context"
	"errors"
	"testing"

	"github.com/0226zy/magents/kimi-chat-go/internal/domain/derr"
	"github.com/0226zy/magents/kimi-chat-go/internal/domain/entity"
)

type fakeChat struct {
	gotMessages []entity.Message
	resp        *entity.ChatResponse
	err         error
}

func (f *fakeChat) Chat(_ context.Context, messages []entity.Message) (*entity.ChatResponse, error) {
	f.gotMessages = messages
	return f.resp, f.err
}

func TestExecuteSendsSystemAndUserMessages(t *testing.T) {
	resp := &entity.ChatResponse{}
	resp.Choices = make([]struct {
		Message      entity.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	}, 1)
	resp.Choices[0].Message = entity.Message{Role: "assistant", Content: "我是 Kimi。"}

	chat := &fakeChat{resp: resp}
	svc := NewChatService(chat)

	final, err := svc.Execute(context.Background(), "system-prompt", "你好")
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if final.Choices[0].Message.Content != "我是 Kimi。" {
		t.Errorf("回答不符合预期: %q", final.Choices[0].Message.Content)
	}
	if len(chat.gotMessages) != 2 ||
		chat.gotMessages[0].Role != "system" || chat.gotMessages[0].Content != "system-prompt" ||
		chat.gotMessages[1].Role != "user" || chat.gotMessages[1].Content != "你好" {
		t.Errorf("发送的消息不符合预期: %+v", chat.gotMessages)
	}
}

func TestExecuteReturnsSentinelWhenNoChoices(t *testing.T) {
	svc := NewChatService(&fakeChat{resp: &entity.ChatResponse{}})
	_, err := svc.Execute(context.Background(), "s", "u")
	if !errors.Is(err, derr.ErrNoChoices) {
		t.Errorf("err = %v，期望 derr.ErrNoChoices", err)
	}
}
