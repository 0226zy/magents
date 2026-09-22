// Package entity 存放领域层的协议 DTO：消息与 chat completion 响应。
// 只依赖标准库，application 与 infrastructure 都依赖这里，方向不允许反过来。
package entity

// Message 是 chat completion 上下文里的一条消息（role + content）。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 对应 POST {base_url}/chat/completions 的请求体。
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// ChatResponse 是大模型一次响应中我们关心的部分，其余字段原样忽略。
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
}
