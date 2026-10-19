package adapter

import (
	"sync"
	"time"
)

// SystemClock 是真实时钟，Advance 是空操作。
type SystemClock struct{}

// NewSystemClock 构造真实时钟。
func NewSystemClock() SystemClock { return SystemClock{} }

// Now 返回当前时间。
func (SystemClock) Now() time.Time { return time.Now() }

// Advance 对真实时钟无意义，保留签名以满足端口。
func (SystemClock) Advance(time.Duration) {}

// SimulatedClock 是可推进的虚拟时钟，对应 Python 的 simulate_time_delay：
// 每轮推进固定时长，让离线演示的时间戳看起来像真的在流逝。测试也用它驱动超时逻辑。
type SimulatedClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewSimulatedClock 以给定时刻为起点构造虚拟时钟。
func NewSimulatedClock(start time.Time) *SimulatedClock {
	if start.IsZero() {
		start = time.Now()
	}
	return &SimulatedClock{now: start}
}

// Now 返回虚拟当前时间。
func (c *SimulatedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance 推进虚拟时间。
func (c *SimulatedClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
