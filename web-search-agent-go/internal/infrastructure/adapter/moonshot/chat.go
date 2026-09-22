package moonshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTemperature = 0.6
	// Kimi K3 是推理模型，会先产出较长的 reasoning_content，需要给最终回答留足
	// 输出预算（Moonshot 要求 max_tokens>=2048），否则答案可能被截断为空。
	defaultMaxTokens = 32768
)

// ChatClient 负责 POST {baseURL}/chat/completions。
type ChatClient struct {
	transport
	baseURL     string
	model       string
	temperature float64
	maxTokens   int
	turns       *Recorder
}

// NewChatClient 构造 chat 协议客户端。recorder 与 FormulaClient 共享，
// 这样一个问题的三类凭据落在同一条 api_turns 上。
func NewChatClient(apiKey, baseURL, model string, recorder *Recorder, httpClient *http.Client) *ChatClient {
	return &ChatClient{
		transport:   newTransport(apiKey, httpClient),
		baseURL:     strings.TrimRight(baseURL, "/"),
		model:       model,
		temperature: defaultTemperature,
		maxTokens:   defaultMaxTokens,
		turns:       recorder,
	}
}

// Model 返回实际使用的模型 id。
func (c *ChatClient) Model() string { return c.model }

// BaseURL 返回已去掉尾部斜杠的端点。
func (c *ChatClient) BaseURL() string { return c.baseURL }

// SetTemperature 设置温度，超出 [0,2] 的值被忽略（对齐 agent.set_temperature）。
func (c *ChatClient) SetTemperature(temperature float64) {
	if temperature >= 0 && temperature <= 2 {
		c.temperature = temperature
	}
}

// Chat 发起一次 chat completion。
//
// tools 原样透传：它是从 Formula 端点拉来的权威声明，验收断言
// formula_tool_declared_each_chat_turn 要求每次请求都带着完整声明。
func (c *ChatClient) Chat(ctx context.Context, messages []Message, tools []json.RawMessage) (*ChatResponse, error) {
	request := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: ReasoningSafeTemperature(c.model, c.temperature),
		MaxTokens:   c.maxTokens,
		Tools:       tools,
	}
	if strings.EqualFold(c.model, "kimi-k3") {
		request.ReasoningEffort = "max"
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("编码 chat 请求失败: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	started := time.Now()
	status, payload, err := c.do(ctx, http.MethodPost, url, body)

	turn := APITurn{
		Kind:       "chat_completion",
		Request:    body,
		HTTPStatus: status,
	}
	record := func(failure error, response []byte) {
		turn.Elapsed = elapsedSeconds(started)
		turn.Response = response
		if failure != nil {
			turn.Error = newTurnError(failure, payload)
		}
		c.turns.Append(turn)
	}

	if err != nil {
		record(err, nil)
		return nil, err
	}

	var response ChatResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		wrapped := fmt.Errorf("解析 chat 响应失败: %w", err)
		record(wrapped, nil)
		return nil, wrapped
	}
	if len(response.Choices) == 0 {
		wrapped := errors.New("chat 响应没有 choices")
		record(wrapped, payload)
		return nil, wrapped
	}

	response.Raw = payload
	record(nil, payload)
	return &response, nil
}
