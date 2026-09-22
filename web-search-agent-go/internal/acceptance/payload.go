// Package acceptance 是实验 1-2 的验收校验器：18 项断言。
//
// 它是纯函数，只读入已归档的运行载荷，不发任何网络请求，也不依赖协议层的
// HTTP 代码。这样同一套判定既能校验 Go 版的实时运行，也能回放 Python 版留下
// 的历史证据。
package acceptance

import (
	"bytes"
	"encoding/json"
)

// FormulaURI 是实验要求的 Formula 标识。
const FormulaURI = "moonshot/web-search:latest"

// Payload 是一次独立运行的完整记录，字段与 Python 版 run_once 的返回一致。
type Payload struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Trace    []Step `json:"trace"`
	APITurns []Turn `json:"api_turns"`
}

// Step 是轨迹里的一步。验收只关心 type / iteration / tool 三个字段。
type Step struct {
	Iteration int    `json:"iteration"`
	Type      string `json:"type"`
	Tool      string `json:"tool,omitempty"`
}

// Turn 是一次 provider 调用的凭据。
type Turn struct {
	Kind       string          `json:"kind"`
	FormulaURI string          `json:"formula_uri,omitempty"`
	Request    TurnRequest     `json:"request"`
	HTTPStatus *int            `json:"http_status"`
	Response   TurnResponse    `json:"response"`
	Error      json.RawMessage `json:"error,omitempty"`
}

// TurnRequest 同时覆盖三类请求的形状：chat 请求带 tools，
// Formula Fiber 请求带 body。
type TurnRequest struct {
	Tools []json.RawMessage `json:"tools,omitempty"`
	Body  *FiberBody        `json:"body,omitempty"`
}

// FiberBody 是 Formula Fiber 的请求体。
//
// Arguments 刻意是 json.RawMessage：验收要确认它是模型生成的原始 JSON
// **字符串**，而不是被解析成对象后重新塞回去的副本。
type FiberBody struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// TurnResponse 覆盖三类响应里验收会读的字段。
type TurnResponse struct {
	ID     string            `json:"id,omitempty"`
	Status string            `json:"status,omitempty"`
	Tools  []json.RawMessage `json:"tools,omitempty"`
	Usage  *Usage            `json:"usage,omitempty"`
}

// Usage 是 token 统计。
type Usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// UsageTotals 是一次运行的 token 汇总，字段名与 Python 版一致。
type UsageTotals struct {
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	TotalTokens        int `json:"total_tokens"`
	CachedPromptTokens int `json:"cached_prompt_tokens"`
	ReasoningTokens    int `json:"reasoning_tokens"`
}

// hasError 报告这条凭据是否记录了失败。
func (t Turn) hasError() bool {
	return len(bytes.TrimSpace(t.Error)) > 0 && !bytes.Equal(bytes.TrimSpace(t.Error), []byte("null"))
}

func (t Turn) statusIs(code int) bool {
	return t.HTTPStatus != nil && *t.HTTPStatus == code
}

// isJSONString 判断原始参数是否为 JSON 字符串，对应 Python 的 isinstance(..., str)。
func isJSONString(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '"'
}

func turnsOfKind(turns []Turn, kind string) []Turn {
	var selected []Turn
	for _, turn := range turns {
		if turn.Kind == kind {
			selected = append(selected, turn)
		}
	}
	return selected
}

// ResponseIDs 返回全部 chat completion 的响应 id。
func ResponseIDs(turns []Turn) []string {
	var ids []string
	for _, turn := range turns {
		if turn.Kind == "chat_completion" && turn.Response.ID != "" {
			ids = append(ids, turn.Response.ID)
		}
	}
	return ids
}

// FiberIDs 只返回真实且成功的 Formula Fiber 凭据。
//
// 三个条件缺一不可：HTTP 200、status == "succeeded"、id 非空。
// 这是验收「不可伪造」的基础：id 由 provider 生成，客户端无从编造。
func FiberIDs(turns []Turn) []string {
	ids := []string{}
	for _, turn := range turns {
		if turn.Kind != "formula_fiber" {
			continue
		}
		if turn.statusIs(200) && turn.Response.Status == "succeeded" && turn.Response.ID != "" {
			ids = append(ids, turn.Response.ID)
		}
	}
	return ids
}

// SummarizeUsage 汇总全部 chat completion 的 token 用量。
func SummarizeUsage(turns []Turn) UsageTotals {
	var totals UsageTotals
	for _, turn := range turns {
		if turn.Kind != "chat_completion" || turn.Response.Usage == nil {
			continue
		}
		usage := turn.Response.Usage
		totals.PromptTokens += usage.PromptTokens
		totals.CompletionTokens += usage.CompletionTokens
		totals.CachedPromptTokens += usage.PromptTokensDetails.CachedTokens
		totals.ReasoningTokens += usage.CompletionTokensDetails.ReasoningTokens
	}
	totals.TotalTokens = totals.PromptTokens + totals.CompletionTokens
	return totals
}
