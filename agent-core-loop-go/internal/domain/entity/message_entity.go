// Package entity 存放领域层的协议 DTO：消息、工具调用、工具定义与大模型响应。
// 只依赖标准库，application 与 infrastructure 都依赖这里，方向不允许反过来。
package entity

import "encoding/json"

// Message 是 chat completion 上下文里的一条消息。
// 三种典型形态：system/user（仅 Content）、assistant（Content 和/或 ToolCalls）、
// tool（ToolCallID + Content，即工具执行结果）。
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 是 assistant 消息中的一次工具调用请求。
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // 目前固定为 "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 是被调用函数的名字与 JSON 字符串形式的参数。
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串，需由执行器解析
}

// ToolDefinition 是提供给模型的工具声明（JSON Schema 参数）。
type ToolDefinition struct {
	Type     string       `json:"type"` // 固定为 "function"
	Function FunctionSpec `json:"function"`
}

// FunctionSpec 描述一个可调用的函数。
type FunctionSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema（object）
}

// ChatResponse 是大模型一次响应中我们关心的部分。
type ChatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`

	// Raw 是 API 返回的原始报文（未重新序列化），仅供轨迹 raw 文件记录，不参与 JSON 序列化。
	Raw json.RawMessage `json:"-"`
}
