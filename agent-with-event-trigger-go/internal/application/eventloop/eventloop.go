// Package eventloop 把多个触发器汇流成一个统一事件队列，由单个 goroutine 分发。
// 这是书中「把所有输入统一建模为事件流」的最小实现。
package eventloop

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// defaultQueueSize 是事件队列容量。触发器的推送不应被慢速的 Agent 阻塞，
// 但也不能无限堆积——缓冲满时触发器自然背压。
const defaultQueueSize = 64

// drainTimeout 是停止后排空剩余事件的预算。
const drainTimeout = 2 * time.Second

// EventLoop 是事件队列与分发循环。
type EventLoop struct {
	queue     chan entity.Event
	dispatch  port.EventDispatcher
	log       port.Logger
	triggers  []port.TriggerSource
	processed atomic.Int64
}

// New 构造事件循环。
func New(dispatch port.EventDispatcher, log port.Logger) *EventLoop {
	return &EventLoop{
		queue:    make(chan entity.Event, defaultQueueSize),
		dispatch: dispatch,
		log:      log,
	}
}

// AddTrigger 注册一个触发器。必须在 Run 之前调用。
func (l *EventLoop) AddTrigger(trigger port.TriggerSource) {
	l.triggers = append(l.triggers, trigger)
}

// Triggers 返回已注册的触发器数量。
func (l *EventLoop) Triggers() int { return len(l.triggers) }

// Processed 返回已处理的事件数。
func (l *EventLoop) Processed() int64 { return l.processed.Load() }

// Run 启动全部触发器并分发事件，阻塞到 ctx 取消且队列排空。
func (l *EventLoop) Run(ctx context.Context) {
	triggerCtx, stopTriggers := context.WithCancel(ctx)
	defer stopTriggers()

	var wg sync.WaitGroup
	for _, trigger := range l.triggers {
		wg.Add(1)
		go func(t port.TriggerSource) {
			defer wg.Done()
			t.Run(triggerCtx, l.emit)
		}(trigger)
	}

	l.log.Info("事件循环启动，已注册 %d 个触发器，等待事件唤醒 Agent", len(l.triggers))

	for {
		select {
		case <-ctx.Done():
			stopTriggers()
			wg.Wait()
			l.drain(ctx)
			l.log.Info("事件循环结束，共处理 %d 个事件", l.processed.Load())
			return
		case event := <-l.queue:
			l.handle(ctx, event)
		}
	}
}

// emit 把事件推进队列。ctx 取消后触发器可能还在跑最后一轮，这里不阻塞。
func (l *EventLoop) emit(event entity.Event) {
	select {
	case l.queue <- event:
	default:
		l.log.Warn("事件队列已满，丢弃事件 %s (%s)", event.ID, event.Type)
	}
}

// drain 在停止后尽量把队列里剩下的事件处理完，避免最后几个事件无声消失。
func (l *EventLoop) drain(parent context.Context) {
	if len(l.queue) == 0 {
		return
	}
	// parent 已经取消，分发要用一个独立的、带预算的上下文。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), drainTimeout)
	defer cancel()

	for {
		select {
		case event := <-l.queue:
			l.handle(ctx, event)
		default:
			return
		}
	}
}

// handle 分发单个事件。单个事件失败只记日志：一个事件出错不该让整个系统停摆。
func (l *EventLoop) handle(ctx context.Context, event entity.Event) {
	count := l.processed.Add(1)
	l.log.Info("取出第 %d 个事件 -> 唤醒 Agent：%s", count, event.ToUserMessage())
	if err := l.dispatch.Dispatch(ctx, event); err != nil {
		l.log.Error("处理事件 %s 失败: %v", event.ID, err)
	}
}
