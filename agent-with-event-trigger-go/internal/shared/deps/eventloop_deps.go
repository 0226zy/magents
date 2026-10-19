package deps

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/eventloop"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/adapter"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/config"
)

// NewLogger 让接口层拿到日志实现而不必直接 import infrastructure。
func NewLogger(out io.Writer) port.Logger { return adapter.NewLoggerTo(out) }

// EventLoopSpec 描述一次事件循环演示要装配哪些触发器与处理者。
type EventLoopSpec struct {
	// Mock 为真时用离线模拟处理者，不需要任何 API Key。
	Mock bool
	// Timer / Recurring / FileWatch 控制启用哪些触发器。
	Timer     bool
	Recurring bool
	FileWatch bool

	Delay     time.Duration
	Interval  time.Duration
	WatchDir  string
	WatchPoll time.Duration

	ConfigPath    string
	Provider      string
	Model         string
	MaxIterations int
}

// BuildEventLoop 装配事件循环演示，返回循环本体与资源释放函数。
func BuildEventLoop(ctx context.Context, spec EventLoopSpec,
	log port.Logger) (*eventloop.EventLoop, func(), error) {

	dispatch, cleanup, err := buildDispatcher(ctx, spec, log)
	if err != nil {
		return nil, nil, err
	}

	loop := eventloop.New(dispatch, log)
	if spec.Timer {
		loop.AddTrigger(&adapter.OneShotTimer{
			TimerID: "daily_backup_check", Delay: spec.Delay, Log: log,
			Content: "一次性定时器到期：请检查每日备份是否已经完成。",
		})
	}
	if spec.Recurring {
		loop.AddTrigger(&adapter.RecurringTimer{
			TimerID: "health_check", Interval: spec.Interval, Log: log,
			Content: "循环定时器到期：请检查服务器健康状况。",
		})
	}
	if spec.FileWatch {
		loop.AddTrigger(&adapter.FileWatcher{
			Dir: spec.WatchDir, Interval: spec.WatchPoll, Log: log,
		})
	}
	return loop, cleanup, nil
}

// buildDispatcher 构造事件处理者：离线模式用模拟动作，真实模式装配完整 Agent。
func buildDispatcher(ctx context.Context, spec EventLoopSpec,
	log port.Logger) (port.EventDispatcher, func(), error) {

	if spec.Mock {
		return adapter.NewMockDispatcher(log), func() {}, nil
	}

	cfg, err := config.Load(spec.ConfigPath)
	if err != nil {
		return nil, nil, err
	}
	if spec.Provider != "" {
		cfg.Provider = spec.Provider
	}
	if spec.Model != "" {
		cfg.Model = spec.Model
	}
	if spec.MaxIterations > 0 {
		cfg.MaxIterations = spec.MaxIterations
	}

	runtime, err := Build(cfg, log)
	if err != nil {
		return nil, nil, fmt.Errorf("%w（离线演示请加 -mock）", err)
	}
	runtime.LoadMCPTools(ctx)
	return runtime.Agent, runtime.Close, nil
}
