package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// stubAgent 用固定的交互时刻与进程表驱动监控逻辑，不依赖真实 Agent。
type stubAgent struct {
	mu                  sync.Mutex
	lastUserInteraction time.Time
	processes           []entity.BackgroundProcess
	reminded            []string
}

func (a *stubAgent) LastUserInteraction() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastUserInteraction
}

func (a *stubAgent) OverdueProcesses(now time.Time, threshold time.Duration) []entity.BackgroundProcess {
	a.mu.Lock()
	defer a.mu.Unlock()

	var overdue []entity.BackgroundProcess
	for _, process := range a.processes {
		if process.Reminded || now.Sub(process.StartTime) < threshold {
			continue
		}
		overdue = append(overdue, process)
	}
	return overdue
}

func (a *stubAgent) MarkProcessReminded(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reminded = append(a.reminded, id)
	for i := range a.processes {
		if a.processes[i].ID == id {
			a.processes[i].Reminded = true
		}
	}
}

type recordingDispatcher struct {
	mu     sync.Mutex
	events []entity.Event
}

func (d *recordingDispatcher) Dispatch(_ context.Context, event entity.Event) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, event)
	return nil
}

func (d *recordingDispatcher) typesSeen() []entity.EventType {
	types := make([]entity.EventType, 0)
	for _, event := range d.seen() {
		types = append(types, event.Type)
	}
	return types
}

func (d *recordingDispatcher) seen() []entity.Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]entity.Event, len(d.events))
	copy(out, d.events)
	return out
}

type stubClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stubClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stubClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type silentLogger struct{}

func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}

func newTestMonitor(agent *stubAgent, clock *stubClock) (*Monitor, *recordingDispatcher) {
	dispatcher := &recordingDispatcher{}
	return New(agent, dispatcher, clock, silentLogger{}, Config{
		Interval:       time.Second,
		UserTimeout:    60 * time.Second,
		ProcessTimeout: 30 * time.Second,
	}), dispatcher
}

// 用户空闲达到阈值时产生 user_timeout —— Python 定义了这个事件类型却从不产生它。
func TestUserTimeoutIsEmittedOncePerIdlePeriod(t *testing.T) {
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	agent := &stubAgent{lastUserInteraction: start}
	clock := &stubClock{now: start}
	monitor, dispatcher := newTestMonitor(agent, clock)
	ctx := context.Background()

	clock.Advance(30 * time.Second)
	monitor.Tick(ctx)
	if len(dispatcher.typesSeen()) != 0 {
		t.Fatal("未到阈值不应产生事件")
	}

	clock.Advance(40 * time.Second)
	monitor.Tick(ctx)
	monitor.Tick(ctx)
	if got := dispatcher.typesSeen(); len(got) != 1 || got[0] != entity.EventUserTimeout {
		t.Fatalf("事件 = %v，期望恰好一个 user_timeout（去重后不应每个 tick 都刷）", got)
	}
}

// 用户重新说话后要复位，下一个空闲期还能再提醒；只提醒一次会让后续空闲全被忽略。
func TestUserTimeoutResetsAfterInteraction(t *testing.T) {
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	agent := &stubAgent{lastUserInteraction: start}
	clock := &stubClock{now: start}
	monitor, dispatcher := newTestMonitor(agent, clock)
	ctx := context.Background()

	clock.Advance(90 * time.Second)
	monitor.Tick(ctx)

	// 用户回来了。
	agent.mu.Lock()
	agent.lastUserInteraction = clock.Now()
	agent.mu.Unlock()
	monitor.Tick(ctx)

	clock.Advance(90 * time.Second)
	monitor.Tick(ctx)

	if got := dispatcher.typesSeen(); len(got) != 2 {
		t.Fatalf("事件数 = %d，期望两个空闲期各提醒一次", len(got))
	}
}

func TestProcessTimeoutIsEmittedOncePerProcess(t *testing.T) {
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	agent := &stubAgent{
		lastUserInteraction: start,
		processes: []entity.BackgroundProcess{
			{ID: "proc_ml", Name: "ML Model Training", StartTime: start},
		},
	}
	clock := &stubClock{now: start}
	monitor, dispatcher := newTestMonitor(agent, clock)
	ctx := context.Background()

	clock.Advance(40 * time.Second)
	monitor.Tick(ctx)
	monitor.Tick(ctx)

	var timeouts []entity.Event
	for _, event := range dispatcher.seen() {
		if event.Type == entity.EventProcessTimeout {
			timeouts = append(timeouts, event)
		}
	}
	if len(timeouts) != 1 {
		t.Fatalf("process_timeout 事件数 = %d，期望 1", len(timeouts))
	}
	if timeouts[0].Metadata["process_name"] != "ML Model Training" {
		t.Errorf("事件元数据缺少进程名: %v", timeouts[0].Metadata)
	}
	if len(agent.reminded) != 1 || agent.reminded[0] != "proc_ml" {
		t.Errorf("提醒标记 = %v", agent.reminded)
	}
}

func TestStartAndStopAreIdempotent(t *testing.T) {
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	monitor, _ := newTestMonitor(&stubAgent{lastUserInteraction: start}, &stubClock{now: start})

	monitor.Start(context.Background())
	monitor.Start(context.Background())
	if !monitor.Running() {
		t.Fatal("Start 后应处于运行状态")
	}

	monitor.Stop()
	monitor.Stop()
	if monitor.Running() {
		t.Fatal("Stop 后不应仍在运行")
	}
}

func TestReconfigureOnlyOverridesPositiveValues(t *testing.T) {
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	monitor, _ := newTestMonitor(&stubAgent{lastUserInteraction: start}, &stubClock{now: start})

	monitor.Reconfigure(Config{UserTimeout: 5 * time.Second})
	cfg := monitor.Config()
	if cfg.UserTimeout != 5*time.Second {
		t.Errorf("用户阈值 = %s，期望 5s", cfg.UserTimeout)
	}
	if cfg.ProcessTimeout != 30*time.Second {
		t.Errorf("未指定的阈值被改动: %s", cfg.ProcessTimeout)
	}
}
