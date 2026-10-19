package eventloop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

type burstTrigger struct {
	name   string
	count  int
	prefix string
}

func (t *burstTrigger) Name() string { return t.name }

func (t *burstTrigger) Run(ctx context.Context, emit func(entity.Event)) {
	for i := range t.count {
		select {
		case <-ctx.Done():
			return
		default:
		}
		emit(entity.NewEvent(entity.EventTimerTrigger,
			t.prefix, map[string]any{"timer_id": t.name, "tick": i}, time.Now()))
	}
	<-ctx.Done()
}

type countingDispatcher struct {
	mu       sync.Mutex
	contents []string
	delay    time.Duration
}

func (d *countingDispatcher) Dispatch(_ context.Context, event entity.Event) error {
	if d.delay > 0 {
		time.Sleep(d.delay)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.contents = append(d.contents, event.Content)
	return nil
}

func (d *countingDispatcher) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.contents)
}

type silentLogger struct{}

func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}

// 多个触发器的事件汇到同一条队列，由单个 goroutine 串行分发。
func TestMultipleTriggersFunnelIntoOneQueue(t *testing.T) {
	dispatcher := &countingDispatcher{}
	loop := New(dispatcher, silentLogger{})
	loop.AddTrigger(&burstTrigger{name: "a", count: 3, prefix: "来自 a"})
	loop.AddTrigger(&burstTrigger{name: "b", count: 2, prefix: "来自 b"})

	if loop.Triggers() != 2 {
		t.Fatalf("触发器数 = %d，期望 2", loop.Triggers())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	loop.Run(ctx)

	if got := dispatcher.count(); got != 5 {
		t.Errorf("已分发事件 = %d，期望 5", got)
	}
	if loop.Processed() != 5 {
		t.Errorf("Processed = %d，期望 5", loop.Processed())
	}
}

// ctx 取消后要停下来，不能一直跑下去。
func TestRunStopsOnContextCancel(t *testing.T) {
	loop := New(&countingDispatcher{}, silentLogger{})
	loop.AddTrigger(&burstTrigger{name: "forever", count: 1, prefix: "x"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run 在 ctx 取消后没有返回")
	}
}

// 停止时队列里剩下的事件要排空，不能直接丢掉。
func TestRunDrainsQueuedEventsBeforeReturning(t *testing.T) {
	dispatcher := &countingDispatcher{delay: 20 * time.Millisecond}
	loop := New(dispatcher, silentLogger{})
	loop.AddTrigger(&burstTrigger{name: "burst", count: 5, prefix: "排空测试"})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	loop.Run(ctx)

	if got := dispatcher.count(); got != 5 {
		t.Errorf("排空后已分发 = %d，期望 5（剩余事件不应被丢弃）", got)
	}
}
