package entity

import (
	"encoding/json"
	"strings"
)

// FinalAnswerMarker 是模型声明「任务已完成」的标记，见系统提示词末段。
const FinalAnswerMarker = "FINAL ANSWER:"

// Message 是发给 /chat/completions 的一条消息。
//
// 字段集合刻意只覆盖 provider 接受的部分：assistant 消息回传时若带上
// reasoning_content，Moonshot 会返回 400 tokenization failed。这里不声明那个
// 字段，因此该错误在结构上无法发生。
type Message struct {
	Role       string     `json:"role"` // system / user / assistant / tool
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 是 assistant 消息中的一次工具调用请求。
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // 恒为 "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 是被调用函数的名字与参数。
type FunctionCall struct {
	Name string `json:"name"`
	// Arguments 是模型生成的原始 JSON 字符串。记录与回传都用原文，
	// 只在执行前解析成 map，避免重新序列化丢字段。
	Arguments string `json:"arguments"`
}

// ToolDefinition 是提供给模型的工具声明。
type ToolDefinition struct {
	Type     string       `json:"type"` // 恒为 "function"
	Function FunctionSpec `json:"function"`
}

// FunctionSpec 描述一个可调用函数。Parameters 是 JSON Schema；
// MCP 工具的 schema 由 provider 给出，原样透传，不做结构假设。
type FunctionSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ChoiceMessage 是响应侧的消息，比请求侧多一个 ReasoningContent。
// 刻意用不同的类型：ReasoningContent 只能读，不能回传。
type ChoiceMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content"`
	ToolCalls        []ToolCall `json:"tool_calls"`
}

// ToAssistantMessage 把模型响应转成可回传的历史消息。
// 转换是类型系统强制的：ReasoningContent 没有对应字段可落。
func (m ChoiceMessage) ToAssistantMessage() Message {
	return Message{
		Role:      "assistant",
		Content:   m.Content,
		ToolCalls: m.ToolCalls,
	}
}

// Choice 是响应里的一个候选。
type Choice struct {
	Message      ChoiceMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// Usage 是一次 chat completion 的 token 统计。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse 是 /chat/completions 的响应。
type ChatResponse struct {
	Choices []Choice        `json:"choices"`
	Usage   Usage           `json:"usage"`
	Raw     json.RawMessage `json:"-"` // 原始报文，供调试与证据使用
}

// ChatOptions 是单次请求的采样参数。
type ChatOptions struct {
	Temperature float64
	MaxTokens   int
	ToolChoice  string // 通常是 "auto"
}

// reasoningModelMarkers 列出只接受 temperature=1 的推理模型特征。
var reasoningModelMarkers = []string{"kimi-k3", "gpt-5"}

// SafeTemperature 为推理模型返回 1，其余模型返回请求值。
// 模型名先小写、再把 "/" 换成 "-"，让 openai/gpt-5 这类 OpenRouter 模型名也能命中。
func SafeTemperature(model string, requested float64) float64 {
	normalized := strings.ReplaceAll(strings.ToLower(model), "/", "-")
	for _, marker := range reasoningModelMarkers {
		if strings.Contains(normalized, marker) {
			return 1
		}
	}
	return requested
}

// ExtractFinalAnswer 提取模型的最终回答。
// 带 FINAL ANSWER: 标记时取标记之后的内容，marked 为 true；
// 不带标记时返回原文，marked 为 false——无工具调用的纯文本回复同样是终止信号。
func ExtractFinalAnswer(content string) (answer string, marked bool) {
	if _, after, found := strings.Cut(content, FinalAnswerMarker); found {
		return strings.TrimSpace(after), true
	}
	return strings.TrimSpace(content), false
}
