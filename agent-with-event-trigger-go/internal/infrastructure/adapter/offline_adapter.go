package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// previewLimit 是文件内容预览的字符上限。
const previewLimit = 120

// MockDispatcher 是离线模拟处理器：不调用大模型，打印 Agent 被唤醒后的动作。
// 让「触发 → 唤醒 → 处理」的闭环在没有任何 API Key 的环境下也能跑通。
type MockDispatcher struct {
	log     port.Logger
	handled atomic.Int64
}

// NewMockDispatcher 构造离线处理器。
func NewMockDispatcher(log port.Logger) *MockDispatcher {
	return &MockDispatcher{log: log}
}

// Handled 返回已处理的事件数。
func (d *MockDispatcher) Handled() int64 { return d.handled.Load() }

// Dispatch 用确定性的模拟动作代替大模型与工具调用。
func (d *MockDispatcher) Dispatch(_ context.Context, event entity.Event) error {
	d.handled.Add(1)
	d.log.Info("Agent 被唤醒，收到消息: %s", event.ToUserMessage())
	d.log.Info("[模拟动作] %s", d.action(event))
	d.log.Info("Agent 处理完成: 已响应 %s 事件", event.Type)
	return nil
}

func (d *MockDispatcher) action(event entity.Event) string {
	switch event.Type {
	case entity.EventTimerTrigger:
		return "读取定时任务上下文 -> 执行例行检查 -> 汇报结果"
	case entity.EventFileChange:
		path, _ := event.Metadata["path"].(string)
		return "读取文件 " + filepath.Base(path) + " -> 内容预览: " + preview(path) + " -> 生成处理建议"
	default:
		return "解析事件 -> 调用相关工具 -> 生成处理结果"
	}
}

func preview(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(无法读取文件内容)"
	}
	text := strings.ReplaceAll(string(data), "\n", " ")
	runes := []rune(text)
	if len(runes) > previewLimit {
		text = string(runes[:previewLimit]) + "..."
	}
	return `"` + text + `"`
}
