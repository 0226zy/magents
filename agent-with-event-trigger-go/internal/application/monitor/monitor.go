// Package monitor 把「长时间没人说话」「后台进程跑太久」变成事件推给 Agent。
//
// Python 版定义了 user_timeout / process_timeout 两种事件类型，但全仓库没有任何
// 代码产生它们，客户端调用的 /monitoring/start 端点也不存在。这个包补上这条链路。
package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// AgentPort 是监控器需要从 Agent 读取的状态。
type AgentPort interface {
	LastUserInteraction() time.Time
	OverdueProcesses(now time.Time, threshold time.Duration) []entity.BackgroundProcess
	MarkProcessReminded(id string)
}

// Config 是监控阈值。
type Config struct {
	Interval       time.Duration
	UserTimeout    time.Duration
	ProcessTimeout time.Duration
}

// Monitor 周期性检查超时并派发提醒事件。
type Monitor struct {
	agent    AgentPort
	dispatch port.EventDispatcher
	clock    port.Clock
	log      port.Logger

	// cfg 也由 mu 保护：/monitoring/start 可以带上新的阈值，
	// 而此时监控循环可能正在另一个 goroutine 里读它。
	mu           sync.Mutex
	cfg          Config
	running      bool
	cancel       context.CancelFunc
	userReminded bool
}

// New 构造监控器，缺省阈值与 Python 注释里描述的一致（用户 60 秒、进程 30 秒）。
func New(agent AgentPort, dispatch port.EventDispatcher, clock port.Clock,
	log port.Logger, cfg Config) *Monitor {

	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.UserTimeout <= 0 {
		cfg.UserTimeout = 60 * time.Second
	}
	if cfg.ProcessTimeout <= 0 {
		cfg.ProcessTimeout = 30 * time.Second
	}
	return &Monitor{agent: agent, dispatch: dispatch, clock: clock, log: log, cfg: cfg}
}

// Config 返回当前阈值。
func (m *Monitor) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Reconfigure 覆盖阈值，零值字段保持原样。正在运行时需要重启才会生效，
// 由调用方（Stop 后再 Start）决定。
func (m *Monitor) Reconfigure(override Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if override.Interval > 0 {
		m.cfg.Interval = override.Interval
	}
	if override.UserTimeout > 0 {
		m.cfg.UserTimeout = override.UserTimeout
	}
	if override.ProcessTimeout > 0 {
		m.cfg.ProcessTimeout = override.ProcessTimeout
	}
}

// Running 报告监控是否在运行。
func (m *Monitor) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Start 在后台启动监控，重复调用无副作用。
func (m *Monitor) Start(parent context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	m.running = true
	m.cancel = cancel
	// 用户重新开始交互后才算新的空闲期，启动时先清掉上一轮的去重标记。
	m.userReminded = false
	cfg := m.cfg
	m.mu.Unlock()

	go m.loop(ctx, cfg.Interval)
	m.log.Info("超时监控已启动：用户 %s / 进程 %s，每 %s 检查一次",
		cfg.UserTimeout, cfg.ProcessTimeout, cfg.Interval)
}

// Stop 停止监控，重复调用无副作用。
func (m *Monitor) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.running = false
	m.cancel = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
		m.log.Info("超时监控已停止")
	}
}

func (m *Monitor) loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Tick(ctx)
		}
	}
}

// Tick 执行一次检查。单独暴露是为了让测试用受控时钟直接驱动，不依赖真实 sleep。
func (m *Monitor) Tick(ctx context.Context) {
	now := m.clock.Now()
	m.checkUserTimeout(ctx, now)
	m.checkProcessTimeout(ctx, now)
}

// checkUserTimeout 检查用户空闲。
//
// 每个空闲期只提醒一次：不去重会每个 tick 刷一条，去重后不复位则整个进程只提醒一次。
// 用户再次交互（LastUserInteraction 前进）时复位，下一个空闲期才会重新提醒。
func (m *Monitor) checkUserTimeout(ctx context.Context, now time.Time) {
	idle := now.Sub(m.agent.LastUserInteraction())

	m.mu.Lock()
	if idle < m.cfg.UserTimeout {
		m.userReminded = false
		m.mu.Unlock()
		return
	}
	if m.userReminded {
		m.mu.Unlock()
		return
	}
	m.userReminded = true
	m.mu.Unlock()

	m.emit(ctx, entity.NewEvent(entity.EventUserTimeout,
		"Please check whether the user still needs help, or summarize current progress.",
		map[string]any{"duration": formatDuration(idle)}, now))
}

// checkProcessTimeout 检查后台进程，每个进程只提醒一次。
func (m *Monitor) checkProcessTimeout(ctx context.Context, now time.Time) {
	m.mu.Lock()
	threshold := m.cfg.ProcessTimeout
	m.mu.Unlock()

	for _, process := range m.agent.OverdueProcesses(now, threshold) {
		m.agent.MarkProcessReminded(process.ID)
		m.emit(ctx, entity.NewEvent(entity.EventProcessTimeout,
			"Please check the process status and decide whether to keep waiting or intervene.",
			map[string]any{
				"process_id":   process.ID,
				"process_name": process.Name,
				"duration":     formatDuration(now.Sub(process.StartTime)),
			}, now))
	}
}

func (m *Monitor) emit(ctx context.Context, event entity.Event) {
	m.log.Info("监控触发事件 %s: %s", event.Type, event.Content)
	if err := m.dispatch.Dispatch(ctx, event); err != nil {
		m.log.Error("派发监控事件 %s 失败: %v", event.Type, err)
	}
}

// formatDuration 渲染成 "1m30s" 这类人类可读形式，写进事件的 duration 字段。
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}
