// Package entity 存放领域层共用的协议 DTO：消息、工具调用、chat 请求/响应。
// 它们只依赖标准库，application 与 infrastructure 都依赖这里，方向不允许反过来。
package entity

import "encoding/json"

// Message 是发给 /chat/completions 的一条消息。
//
// 字段集合刻意只覆盖 Moonshot 接受的部分：assistant 消息回传时若带上
// reasoning_content / refusal，Moonshot 会返回 400 tokenization failed。
// 这里不声明那些字段，因此该错误在结构上无法发生。
type Message struct {
	Role       string     `json:"role"` // system / user / assistant / tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 是模型请求的一次工具调用。
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // 恒为 "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 是工具调用的函数名与参数。
type FunctionCall struct {
	Name string `json:"name"`
	// Arguments 是模型生成的原始 JSON 字符串。
	// 转发给 Formula Fiber 时必须用它的原文，不能解析后重新序列化。
	Arguments string `json:"arguments"`
}

// ChatRequest 的 Tools 字段刻意是 []json.RawMessage 而不是类型化的 Tool。
//
// 声明是从 GET /formulas/{uri}/tools 拉来的权威 schema，客户端对其结构零假设。
// 若解码进 struct 再重新编码，provider schema 里任何未声明的字段都会被静默丢弃，
// 而验收断言 formula_tool_declared_each_chat_turn 要求每次 chat 请求都带着完整声明。
// 透传是正确性要求，不是优化。
type ChatRequest struct {
	Model           string            `json:"model"`
	Messages        []Message         `json:"messages"`
	Temperature     float64           `json:"temperature"`
	MaxTokens       int               `json:"max_tokens"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	Tools           []json.RawMessage `json:"tools,omitempty"`
}

// ChatResponse 是 /chat/completions 的响应。
type ChatResponse struct {
	ID      string          `json:"id"`
	Choices []Choice        `json:"choices"`
	Usage   Usage           `json:"usage"`
	Raw     json.RawMessage `json:"-"` // 证据落盘用原文，避免未声明字段丢失
}

// Choice 是响应里的一个候选。
type Choice struct {
	FinishReason string        `json:"finish_reason"`
	Message      ChoiceMessage `json:"message"`
}

// ChoiceMessage 是响应侧的消息，字段比请求侧的 Message 多。
// 刻意用不同的类型：ReasoningContent 只能读、不能回传。
type ChoiceMessage struct {
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content"`
	ToolCalls        []ToolCall `json:"tool_calls"`
}

// ToAssistantMessage 把模型响应转成可回传的历史消息。
//
// 对应 agent.py:413-430 —— 那段 Python 注释解释了为什么不能直接复用 SDK 对象。
// Go 里这个转换是类型系统强制的：ReasoningContent 没有对应字段可落。
func (m ChoiceMessage) ToAssistantMessage() Message {
	return Message{
		Role:      "assistant",
		Content:   m.Content,
		ToolCalls: m.ToolCalls,
	}
}

// Usage 是一次 chat completion 的 token 统计。
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
