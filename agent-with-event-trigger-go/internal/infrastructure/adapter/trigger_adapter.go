package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// OneShotTimer 在延迟到期后触发一次定时事件。
// 对应书中「下周一上午 10:00 致电 DMV」这类有明确时间点的任务。
type OneShotTimer struct {
	TimerID string
	Delay   time.Duration
	Content string
	Log     port.Logger
}

// Name 返回触发器名字。
func (t *OneShotTimer) Name() string { return fmt.Sprintf("OneShotTimer(%s)", t.TimerID) }

// Run 等待延迟后触发；ctx 取消时提前返回且不触发。
func (t *OneShotTimer) Run(ctx context.Context, emit func(entity.Event)) {
	t.logf("已注册：%s 后触发", t.Delay)
	timer := time.NewTimer(t.Delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		emit(entity.NewEvent(entity.EventTimerTrigger, t.Content, map[string]any{
			"timer_id":                t.TimerID,
			"kind":                    "one_shot",
			"scheduled_delay_seconds": t.Delay.Seconds(),
		}, time.Now()))
	}
}

func (t *OneShotTimer) logf(format string, args ...any) {
	if t.Log != nil {
		t.Log.Info("[%s] "+format, append([]any{t.Name()}, args...)...)
	}
}

// RecurringTimer 每隔固定间隔触发一次。
// 对应「每小时检查服务器健康状况」这类周期性任务。
type RecurringTimer struct {
	TimerID  string
	Interval time.Duration
	Content  string
	Log      port.Logger
}

// Name 返回触发器名字。
func (t *RecurringTimer) Name() string { return fmt.Sprintf("RecurringTimer(%s)", t.TimerID) }

// Run 循环触发直到 ctx 取消。
func (t *RecurringTimer) Run(ctx context.Context, emit func(entity.Event)) {
	if t.Log != nil {
		t.Log.Info("[%s] 已注册：每 %s 触发一次", t.Name(), t.Interval)
	}
	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()

	tick := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick++
			emit(entity.NewEvent(entity.EventTimerTrigger,
				fmt.Sprintf("%s（第 %d 次）", t.Content, tick),
				map[string]any{
					"timer_id":         t.TimerID,
					"kind":             "recurring",
					"interval_seconds": t.Interval.Seconds(),
					"tick":             tick,
				}, time.Now()))
		}
	}
}

// FileWatcher 轮询目录，发现新增或修改的文件时触发事件。
// 用轮询而非 fsnotify：不引第三方依赖，跨平台行为一致，演示场景够用。
type FileWatcher struct {
	Dir      string
	Interval time.Duration
	Log      port.Logger

	snapshot map[string]time.Time
}

// Name 返回触发器名字。
func (w *FileWatcher) Name() string { return fmt.Sprintf("FileWatch(%s)", w.Dir) }

// Run 轮询目录直到 ctx 取消。
func (w *FileWatcher) Run(ctx context.Context, emit func(entity.Event)) {
	if err := os.MkdirAll(w.Dir, 0o755); err != nil && w.Log != nil {
		w.Log.Warn("[%s] 创建监听目录失败: %v", w.Name(), err)
	}
	// 启动时先拍一张快照，否则已有文件会在第一轮被当成新增全部触发一遍。
	w.snapshot = w.scan()
	if w.Log != nil {
		w.Log.Info("[%s] 已注册：轮询间隔 %s（当前已有 %d 个文件）",
			w.Name(), w.Interval, len(w.snapshot))
	}

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := w.scan()
			for name, modified := range current {
				previous, existed := w.snapshot[name]
				switch {
				case !existed:
					emit(w.event(name, "created"))
				case !modified.Equal(previous):
					emit(w.event(name, "modified"))
				}
			}
			w.snapshot = current
		}
	}
}

func (w *FileWatcher) event(name, change string) entity.Event {
	action := "修改"
	if change == "created" {
		action = "新增"
	}
	return entity.NewEvent(entity.EventFileChange,
		fmt.Sprintf("检测到文件%s，请查看其内容并给出简要处理建议。", action),
		map[string]any{"path": filepath.Join(w.Dir, name), "change": change},
		time.Now())
}

func (w *FileWatcher) scan() map[string]time.Time {
	snapshot := map[string]time.Time{}
	entries, err := os.ReadDir(w.Dir)
	if err != nil {
		return snapshot
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		snapshot[entry.Name()] = info.ModTime()
	}
	return snapshot
}
