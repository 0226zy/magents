package entity

import (
	"encoding/json"
	"sync"
)

// APITurn 是一次 provider 调用的凭据：三类调用（工具声明拉取、chat completion、
// Formula Fiber 执行）成功与失败都要留下一条。
//
// Request 里绝对不能包含 Authorization 头：这里的请求记录在结构上只容纳
// {method, url, body} 与 chat 的请求体，凭据没有落点。
type APITurn struct {
	Kind       string          `json:"kind"` // formula_tools / chat_completion / formula_fiber
	FormulaURI string          `json:"formula_uri,omitempty"`
	Request    json.RawMessage `json:"request"`
	HTTPStatus *int            `json:"http_status"`
	Response   json.RawMessage `json:"response,omitempty"`
	Elapsed    float64         `json:"elapsed_seconds"`
	Error      *TurnError      `json:"error,omitempty"`
}

// TurnError 记录失败调用的成因，并尽量带上 provider 返回的原始 body。
type TurnError struct {
	Class        string          `json:"class"`
	Message      string          `json:"message"`
	Response     json.RawMessage `json:"response,omitempty"`
	ResponseText string          `json:"response_text,omitempty"`
}

// HTTPRequestRecord 是 Formula 两个端点的请求记录形状，对齐 agent.py。
type HTTPRequestRecord struct {
	Method string          `json:"method"`
	URL    string          `json:"url"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// FiberRequest 是 POST /fibers 的请求体。
// Arguments 必须是模型生成的原始字符串。
type FiberRequest struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Recorder 收集一次问题内的全部 API 凭据。
//
// 它是并发安全的：同一轮里的多个 Formula Fiber 会在各自的 goroutine 里执行，
// 因此 api_turns 会被并发追加。追加顺序随之不再严格反映发起顺序，验收断言
// 只按 kind 分类计数，不依赖顺序。
type Recorder struct {
	mu    sync.Mutex
	turns []APITurn
}

// NewRecorder 构造一个空的凭据收集器。
func NewRecorder() *Recorder { return &Recorder{} }

// Append 追加一条凭据。
func (r *Recorder) Append(turn APITurn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns = append(r.turns, turn)
}

// Turns 返回凭据的副本。
func (r *Recorder) Turns() []APITurn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]APITurn(nil), r.turns...)
}

// Reset 清空凭据，供新的问题使用。
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns = nil
}
