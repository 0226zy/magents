package entity

import (
	"sort"
	"time"
)

// BackgroundProcess 是一个被登记监控的后台进程。
type BackgroundProcess struct {
	ID        string         `json:"-"`
	Name      string         `json:"name"`
	StartTime time.Time      `json:"start_time"`
	Metadata  map[string]any `json:"metadata"`
	Reminded  bool           `json:"reminded"`
}

// ProcessRegistry 是后台进程登记簿。非线程安全，由持有它的应用服务加锁。
type ProcessRegistry struct {
	items map[string]*BackgroundProcess
}

// Register 登记或覆盖一个进程。
func (r *ProcessRegistry) Register(p BackgroundProcess) {
	r.ensureInit()
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	copied := p
	r.items[p.ID] = &copied
}

// Unregister 注销进程，返回它此前是否存在。
func (r *ProcessRegistry) Unregister(id string) bool {
	if r.items == nil {
		return false
	}
	if _, ok := r.items[id]; !ok {
		return false
	}
	delete(r.items, id)
	return true
}

// Overdue 返回运行时长超过阈值、且尚未提醒过的进程。
// 按 ID 排序输出：Go 的 map 遍历顺序随机，不排序会让提醒事件的顺序不可复现。
func (r *ProcessRegistry) Overdue(now time.Time, threshold time.Duration) []BackgroundProcess {
	var out []BackgroundProcess
	for _, p := range r.items {
		if p.Reminded || now.Sub(p.StartTime) < threshold {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// MarkReminded 记录某进程已经提醒过，保证每个进程只提醒一次。
func (r *ProcessRegistry) MarkReminded(id string) {
	if p, ok := r.items[id]; ok {
		p.Reminded = true
	}
}

// Len 返回登记的进程数量。
func (r *ProcessRegistry) Len() int { return len(r.items) }

// Snapshot 返回登记簿的值副本，供轨迹与状态接口使用。
func (r *ProcessRegistry) Snapshot() map[string]BackgroundProcess {
	out := make(map[string]BackgroundProcess, len(r.items))
	for id, p := range r.items {
		out[id] = *p
	}
	return out
}

// Clear 清空登记簿。
func (r *ProcessRegistry) Clear() { r.items = map[string]*BackgroundProcess{} }

func (r *ProcessRegistry) ensureInit() {
	if r.items == nil {
		r.items = map[string]*BackgroundProcess{}
	}
}
