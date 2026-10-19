// Package session 实现会话管理（dsh State/Control 层在应用侧的编排）：
// 创建、resume（启动时从日志恢复）、fork、replay（事件回放）、search，
// 以及回合生命周期管理（一个会话同一时刻只跑一个回合）。
//
// 会话历史不是单独存储的，而是从 append-only 事件日志派生（与 dsh 同一条纪律：
// 日志是唯一事实源，Messages() 是它的一个投影）。
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0226zy/magents/dsh-go/internal/application/agent"
	"github.com/0226zy/magents/dsh-go/internal/domain/derr"
	"github.com/0226zy/magents/dsh-go/internal/domain/entity"
	"github.com/0226zy/magents/dsh-go/internal/domain/port"
)

// Manager 管理全部会话运行时。
type Manager struct {
	store           port.SessionStorePort
	loop            *agent.Loop
	cwdOf           func(sessionID string) string // bash 感知的 cwd（组合根注入）
	spillDir        string
	approvalTimeout time.Duration
	// cwdGuard 校验新会话工作区（组合根注入，如 security.allowed_cwd_roots 白名单）；nil = 不限制。
	cwdGuard func(cwd string) error
	// compression 是上下文投影压缩策略（组合根注入；at=0 = 禁用）。
	compression compressionConfig

	mu       sync.Mutex
	runtimes map[string]*Runtime
}

// NewManager 构造会话管理器。
func NewManager(store port.SessionStorePort, loop *agent.Loop, cwdOf func(sessionID string) string, spillDir string, approvalTimeout time.Duration) *Manager {
	return &Manager{store: store, loop: loop, cwdOf: cwdOf, spillDir: spillDir, approvalTimeout: approvalTimeout, runtimes: map[string]*Runtime{}}
}

// SetCwdGuard 注入工作区白名单校验（组合根启动期调用）。
func (m *Manager) SetCwdGuard(guard func(cwd string) error) { m.cwdGuard = guard }

// ── 会话生命周期 ─────────────────────────────────────────────────

// Create 创建新会话（cwd 为项目工作区根，mode 为审批模式，model 为 LLMInfoRepo 中的模型名，
// 空串表示使用缺省模型——解析时机在回合开始，此处不校验存在性）。
func (m *Manager) Create(cwd, mode, title, model string) (*Runtime, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	abs, err := filepath.Abs(cwd)
	if err == nil {
		cwd = abs
	}
	if m.cwdGuard != nil {
		if err := m.cwdGuard(cwd); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		return nil, fmt.Errorf("工作区目录不可创建: %w", err)
	}
	header := entity.SessionHeader{
		ID:    newID("s"),
		Title: title,
		Cwd:   cwd,
		Mode:  mode,
		Model: model,
	}
	if err := m.store.Create(header); err != nil {
		return nil, err
	}
	return m.load(header.ID)
}

// Get 取会话运行时；内存中没有则从磁盘日志恢复（resume）。
func (m *Manager) Get(id string) (*Runtime, error) {
	return m.load(id)
}

// List 返回全部会话 header（按创建时间倒序）。
func (m *Manager) List() ([]entity.SessionHeader, error) {
	return m.store.List()
}

// Fork 在 atSeq 切点派生新会话（atSeq<0 表示当前末尾）。
func (m *Manager) Fork(id string, atSeq int) (*Runtime, error) {
	header, err := m.store.Fork(id, newID("s"), atSeq)
	if err != nil {
		return nil, err
	}
	return m.load(header.ID)
}

// Events 返回 fromSeq 起的事件（replay：UI/审计从日志重建任意时刻的视图）。
func (m *Manager) Events(id string, fromSeq int) ([]entity.Event, error) {
	rt, err := m.load(id)
	if err != nil {
		return nil, err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if fromSeq < 0 {
		fromSeq = 0
	}
	if fromSeq >= len(rt.events) {
		return nil, nil
	}
	return append([]entity.Event(nil), rt.events[fromSeq:]...), nil
}

// Search 在会话日志中检索（对齐 dsh 会话日志的 search 能力）：
// 对消息正文、工具调用参数与提示文本做大小写不敏感子串匹配。
func (m *Manager) Search(id, query string) ([]entity.Event, error) {
	rt, err := m.load(id)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	var hits []entity.Event
	for _, ev := range rt.events {
		if ev.Message != nil {
			if strings.Contains(strings.ToLower(ev.Message.Content), q) {
				hits = append(hits, ev)
				continue
			}
			matched := false
			for _, tc := range ev.Message.ToolCalls {
				if strings.Contains(strings.ToLower(tc.Function.Name+" "+tc.Function.Arguments), q) {
					matched = true
					break
				}
			}
			if matched {
				hits = append(hits, ev)
			}
		} else if strings.Contains(strings.ToLower(ev.Note), q) {
			hits = append(hits, ev)
		}
	}
	return hits, nil
}

// SetMode 切换会话审批模式（suggest / auto-edit / full-auto），并持久化到 header。
func (m *Manager) SetMode(id, mode string) error {
	switch mode {
	case "suggest", "auto-edit", "full-auto":
	default:
		return fmt.Errorf("非法审批模式 %q（可选: suggest / auto-edit / full-auto）", mode)
	}
	rt, err := m.load(id)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	rt.header.Mode = mode
	header := rt.header
	rt.mu.Unlock()
	return m.store.UpdateHeader(header)
}

// SetModel 切换会话选用的模型（空 = 缺省），并持久化到 header。
// 下一个回合生效——编排器用它给 fork 出的会话换角色模型（如编码会话用快模型）。
func (m *Manager) SetModel(id, model string) error {
	rt, err := m.load(id)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	rt.header.Model = model
	header := rt.header
	rt.mu.Unlock()
	return m.store.UpdateHeader(header)
}

// ModeOf 返回会话当前审批模式（供审批钩子查询）。
func (m *Manager) ModeOf(id string) string {
	m.mu.Lock()
	rt, ok := m.runtimes[id]
	m.mu.Unlock()
	if !ok {
		return ""
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.header.Mode
}

// HeaderOf 返回会话 header 副本。
func (m *Manager) HeaderOf(id string) (entity.SessionHeader, error) {
	rt, err := m.load(id)
	if err != nil {
		return entity.SessionHeader{}, err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.header, nil
}

func (m *Manager) load(id string) (*Runtime, error) {
	m.mu.Lock()
	if rt, ok := m.runtimes[id]; ok {
		m.mu.Unlock()
		return rt, nil
	}
	m.mu.Unlock()

	header, events, err := m.store.Load(id)
	if err != nil {
		return nil, err
	}
	rt := &Runtime{
		header:        header,
		events:        events,
		manager:       m,
		subs:          map[chan entity.Event]struct{}{},
		pending:       map[string]chan entity.ApprovalDecision{},
		compressCache: newCompressCache(),
	}
	m.mu.Lock()
	m.runtimes[id] = rt
	m.mu.Unlock()
	return rt, nil
}

// ── 回合控制 ─────────────────────────────────────────────────────

// RunTurn 异步启动一个回合。会话正忙时返回 ErrSessionBusy。
// 回合结束（正常或失败）会追加 turn_done / turn_failed marker 事件，
// SSE 订阅者据此感知回合结束。
func (m *Manager) RunTurn(id, userText string) error {
	rt, err := m.load(id)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	if rt.running {
		rt.mu.Unlock()
		return derr.ErrSessionBusy
	}
	rt.running = true
	ctx, cancel := context.WithCancel(context.Background())
	rt.cancel = cancel
	rt.mu.Unlock()

	go func() {
		defer func() {
			rt.mu.Lock()
			rt.running = false
			rt.cancel = nil
			rt.mu.Unlock()
		}()
		if _, err := m.loop.RunTurn(ctx, rt, userText); err != nil {
			_, _ = rt.AppendEvent(entity.Event{Kind: entity.EventMarker, Marker: &entity.Marker{
				Type: "turn_failed", Detail: err.Error(),
			}})
			return
		}
		_, _ = rt.AppendEvent(entity.Event{Kind: entity.EventMarker, Marker: &entity.Marker{Type: "turn_done"}})
	}()
	return nil
}

// Cancel 取消正在运行的回合。
func (m *Manager) Cancel(id string) {
	rt, err := m.load(id)
	if err != nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cancel != nil {
		rt.cancel()
	}
}

// ResolveApproval 裁决一个待决审批。
func (m *Manager) ResolveApproval(id, approvalID string, approved bool) error {
	rt, err := m.load(id)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	ch, ok := rt.pending[approvalID]
	rt.mu.Unlock()
	if !ok {
		return derr.ErrNoPendingApproval
	}
	ch <- entity.ApprovalDecision{Approved: approved, By: "human"}
	return nil
}

// Subscribe 订阅会话实时事件（SSE）。返回的 channel 带缓冲，
// 消费缓慢时丢弃而非阻塞会话（活性优先：订阅者不应拖垮主链路）。
func (m *Manager) Subscribe(id string) (<-chan entity.Event, func(), error) {
	rt, err := m.load(id)
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan entity.Event, 256)
	rt.mu.Lock()
	rt.subs[ch] = struct{}{}
	rt.mu.Unlock()
	unsub := func() {
		rt.mu.Lock()
		delete(rt.subs, ch)
		close(ch)
		rt.mu.Unlock()
	}
	return ch, unsub, nil
}

// ── Runtime：port.SessionRuntime 的实现 ─────────────────────────

// Runtime 是一个会话的内存运行时：事件日志的内存镜像 + 回合状态 + 订阅者 + 待决审批。
type Runtime struct {
	header  entity.SessionHeader
	events  []entity.Event
	manager *Manager

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	tokens  int
	subs    map[chan entity.Event]struct{}
	pending map[string]chan entity.ApprovalDecision
	// compressCache 是投影压缩替身缓存（事件 seq → 压缩后文本），保证重复投影幂等；
	// compressNoticed 标记压缩说明 notice 是否已注入轨迹（只注一次）。
	compressCache   map[int]string
	compressNoticed bool
}

// ID 实现 port.SessionRuntime。
func (r *Runtime) ID() string { return r.header.ID }

// Model 实现 port.SessionRuntime：会话选用的模型名（空 = 缺省）。
func (r *Runtime) Model() string { return r.header.Model }

// Cwd 实现 port.SessionRuntime：bash 会话漂移后的最新目录。
func (r *Runtime) Cwd() string {
	if r.manager.cwdOf != nil {
		if cwd := r.manager.cwdOf(r.header.ID); cwd != "" {
			return cwd
		}
	}
	return r.header.Cwd
}

// Running 报告会话是否有运行中的回合。
func (r *Runtime) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Header 返回 header 副本。
func (r *Runtime) Header() entity.SessionHeader {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.header
}

// AppendEvent 实现 port.SessionRuntime：持久化 → 内存镜像 → 广播订阅者。
func (r *Runtime) AppendEvent(ev entity.Event) (entity.Event, error) {
	ev, err := r.manager.store.Append(r.header.ID, ev)
	if err != nil {
		return ev, err
	}
	r.mu.Lock()
	r.events = append(r.events, ev)
	for ch := range r.subs {
		select {
		case ch <- ev:
		default: // 订阅者消费缓慢：丢弃，不阻塞主链路
		}
	}
	r.mu.Unlock()
	return ev, nil
}

// Messages 实现 port.SessionRuntime：从事件日志派生消息序列，
// 并修复轨迹完整性（指引 §4.2：缺配对的 tool result 以合成消息补齐）。
// 若压缩策略激活（累计 token 超阈值），较早的长消息在投影层被替换为
// 「摘录 + spill 全文路径」（见 compress.go）；事件日志本体不变。
func (r *Runtime) Messages() []entity.Message {
	r.mu.Lock()
	events := append([]entity.Event(nil), r.events...)
	r.mu.Unlock()

	var indexed []indexedMessage
	for _, ev := range events {
		switch {
		case ev.Message != nil:
			indexed = append(indexed, indexedMessage{seq: ev.Seq, msg: *ev.Message})
		case ev.Kind == entity.EventNotice:
			// Harness 提示以普通 system 文本身份进入上下文（中立格式，无厂商私有结构）
			indexed = append(indexed, indexedMessage{seq: ev.Seq, msg: entity.Message{Role: "system", Content: ev.Note}})
		}
	}

	// 投影压缩（评审 P1）：只改投影，不改日志；幂等（按 seq 缓存替身）
	if r.compressActive() {
		r.compressIndexed(indexed)
	}

	raw := make([]entity.Message, len(indexed))
	for i := range indexed {
		raw[i] = indexed[i].msg
	}

	var out []entity.Message
	for i := 0; i < len(raw); i++ {
		m := raw[i]
		out = append(out, m)
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		// 收集本 assistant 消息全部 tool_call，逐一配对其后的 tool 消息
		pending := map[string]string{}
		for _, tc := range m.ToolCalls {
			pending[tc.ID] = tc.Function.Name
		}
		j := i + 1
		for j < len(raw) && raw[j].Role == "tool" {
			out = append(out, raw[j])
			delete(pending, raw[j].ToolCallID)
			j++
		}
		for id, name := range pending {
			out = append(out, entity.Message{
				Role:       "tool",
				ToolCallID: id,
				Name:       name,
				Content:    "（该工具调用未产生结果：回合在此中断，或会话从更早的事件点 fork。请勿假设其执行成功。）",
			})
		}
		i = j - 1
	}
	return out
}

// AddTokens 实现 port.SessionRuntime。
func (r *Runtime) AddTokens(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens += n
	return r.tokens
}

// Approve 实现 port.SessionRuntime：登记待决审批、落盘审批事件、阻塞等待裁决。
// 超时按拒绝处理（指引 §4.5：每条路径必须有明确终止）。
func (r *Runtime) Approve(ctx context.Context, toolName, summary, detail string) entity.ApprovalDecision {
	id := newID("appr")
	ch := make(chan entity.ApprovalDecision, 1)
	r.mu.Lock()
	r.pending[id] = ch
	r.mu.Unlock()

	_, _ = r.AppendEvent(entity.Event{Kind: entity.EventApproval, Approval: &entity.ApprovalRecord{
		ID: id, ToolName: toolName, Summary: summary, Detail: detail, By: "pending",
	}})

	finish := func(d entity.ApprovalDecision) entity.ApprovalDecision {
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
		_, _ = r.AppendEvent(entity.Event{Kind: entity.EventApproval, Approval: &entity.ApprovalRecord{
			ID: id, ToolName: toolName, Summary: summary, Approved: d.Approved, By: d.By,
		}})
		return d
	}

	select {
	case d := <-ch:
		return finish(d)
	case <-time.After(r.manager.approvalTimeout):
		return finish(entity.ApprovalDecision{Approved: false, By: "timeout", Reason: "审批超时（默认拒绝）"})
	case <-ctx.Done():
		return finish(entity.ApprovalDecision{Approved: false, By: "cancelled", Reason: "回合已取消"})
	}
}

// Spill 实现 port.SessionRuntime：超长输出全文落盘。
func (r *Runtime) Spill(toolName, content string) string {
	dir := filepath.Join(r.manager.spillDir, r.header.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "(spill 落盘失败: " + err.Error() + ")"
	}
	p := filepath.Join(dir, fmt.Sprintf("%s-%d.log", toolName, time.Now().UnixNano()))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return "(spill 落盘失败: " + err.Error() + ")"
	}
	return p
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s_%s%s", prefix, time.Now().Format("20060102"), hex.EncodeToString(buf))
}
