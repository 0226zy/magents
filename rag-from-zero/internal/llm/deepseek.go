// Package llm 是大模型客户端。阶段一使用 DeepSeek API（OpenAI 兼容格式），密钥从环境变量读取。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"
)

// LLM 是聊天补全客户端接口。
type LLM interface {
	Chat(ctx context.Context, messages []Message) (string, error)
}

// Message 是 OpenAI 格式的对话消息。
type Message struct {
	Role    string `json:"role"` // system / user / assistant
	Content string `json:"content"`
}

// Client 是 DeepSeek 客户端。
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	client     *http.Client
	maxRetries int
}

type Option func(*Client)

func WithBaseURL(url string) Option         { return func(c *Client) { c.baseURL = url } }
func WithModel(model string) Option         { return func(c *Client) { c.model = model } }
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.client = hc } }
func WithMaxRetries(n int) Option           { return func(c *Client) { c.maxRetries = n } }

const (
	defaultBaseURL = "https://api.deepseek.com"
	defaultModel   = "deepseek-chat"
)

// NewClient 构造客户端；apiKey 为空时返回错误。
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("llm: apiKey 不能为空")
	}
	c := &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		model:      defaultModel,
		client:     &http.Client{Timeout: 120 * time.Second},
		maxRetries: 3,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// NewFromEnv 从环境变量构造客户端：
//   - DEEPSEEK_API_KEY（必填）
//   - DEEPSEEK_BASE_URL（可选，默认 https://api.deepseek.com）
//   - DEEPSEEK_MODEL（可选，默认 deepseek-chat）
func NewFromEnv() (*Client, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil, errors.New("llm: 环境变量 DEEPSEEK_API_KEY 未设置")
	}
	return NewClient(apiKey,
		WithBaseURL(getenvDefault("DEEPSEEK_BASE_URL", defaultBaseURL)),
		WithModel(getenvDefault("DEEPSEEK_MODEL", defaultModel)),
	)
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ---------- OpenAI 兼容请求/响应结构 ----------

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// apiError 区分可重试（429/5xx）与不可重试（其他 4xx）错误。
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("llm: DeepSeek 返回 %d: %s", e.status, e.msg)
}

func (e *apiError) retryable() bool {
	return e.status == http.StatusTooManyRequests || e.status >= 500
}

// Chat 调用 POST /chat/completions（OpenAI 兼容）。带超时、区分 4xx/5xx、429/5xx 指数退避重试。
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}
		answer, err := c.chatOnce(ctx, messages)
		if err == nil {
			return answer, nil
		}
		lastErr = err
		var ae *apiError
		if !errors.As(err, &ae) || !ae.retryable() {
			return "", err // 4xx（除 429）不重试，直接失败
		}
	}
	return "", fmt.Errorf("llm: 重试 %d 次后仍失败: %w", c.maxRetries, lastErr)
}

func (c *Client) chatOnce(ctx context.Context, messages []Message) (string, error) {
	body, err := json.Marshal(chatRequest{Model: c.model, Messages: messages})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		var cr chatResponse
		if json.Unmarshal(data, &cr) == nil && cr.Error != nil {
			msg = cr.Error.Message
		}
		return "", &apiError{status: resp.StatusCode, msg: msg}
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", fmt.Errorf("llm: 解析响应: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", errors.New("llm: 响应中没有 choices")
	}
	return cr.Choices[0].Message.Content, nil
}
