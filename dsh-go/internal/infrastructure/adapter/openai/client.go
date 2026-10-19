// Package openai 是 OpenAI 兼容协议的 LLM 适配器插件（插件名 "openai-compat"）。
// DeepSeek / Moonshot / OpenAI 及一切兼容端点均可经本适配器接入，
// 与厂商的耦合被限制在本包内（指引 §8 纪律三：保留中立层）。
//
// 错误分类对应指引 §4.1 API 层故障：限流 / 过载 / 超时 / 断连为可重试错误，
// 认证失败 / 参数非法为不可重试错误，由 Retryable() 供 Harness 恢复路径判别。
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/0226zy/magents/dsh-go/internal/domain/entity"
)

// StatusError 是 HTTP 非 2xx 响应错误。
type StatusError struct {
	StatusCode int
	Body       string
	// RetryAfter 是服务端建议的等待时长（429/503 响应头），静默重试必须尊重。
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("模型 API 返回 %d: %s", e.StatusCode, truncate(e.Body, 300))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Retryable 按错误→恢复策略映射表判别（指引 §4.2：先分类，再计数）。
// 可重试：429 限流、5xx 过载、超时、断连等基础设施噪声；
// 不可重试：400 参数非法、401/403 认证权限、404 端点不存在。
func Retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		switch {
		case se.StatusCode == 429:
			return true
		case se.StatusCode >= 500:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true // 网络抖动 / 断连
	}
	return false
}

// RetryAfterHint 提取服务端建议的等待时长，无建议时返回 0。
func RetryAfterHint(err error) time.Duration {
	var se *StatusError
	if errors.As(err, &se) {
		return se.RetryAfter
	}
	return 0
}

// Client 负责 POST {baseURL}/chat/completions。
type Client struct {
	apiKey      string
	baseURL     string
	model       string
	temperature float64
	maxTokens   int
	timeout     time.Duration
	http        *http.Client
}

// New 构造适配器。timeout 是单次调用活性看门狗：超时即杀流，
// 由上层按可重试错误重试（指引 §4.2 活性看门狗条款）。
func New(apiKey, baseURL, model string, temperature float64, maxTokens int, timeout time.Duration) *Client {
	return &Client{
		apiKey:      apiKey,
		baseURL:     strings.TrimRight(baseURL, "/"),
		model:       model,
		temperature: temperature,
		maxTokens:   maxTokens,
		timeout:     timeout,
		// 客户端级超时为零：超时预算由每次调用的 context 单独控制。
		http: &http.Client{Timeout: 0},
	}
}

// Model 返回实际使用的模型 id。
func (c *Client) Model() string { return c.model }

type chatRequest struct {
	Model       string                  `json:"model"`
	Messages    []entity.Message        `json:"messages"`
	Tools       []entity.ToolDefinition `json:"tools,omitempty"`
	Temperature float64                 `json:"temperature"`
	MaxTokens   int                     `json:"max_tokens"`
}

// Chat 实现 port.LLMPort。一次调用一个超时预算，不做内部重试。
func (c *Client) Chat(ctx context.Context, messages []entity.Message, tools []entity.ToolDefinition) (*entity.ChatResponse, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: c.temperature,
		MaxTokens:   c.maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("编码 chat 请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// 传输层超时归一成 context.DeadlineExceeded，供 Retryable 统一判别。
		if isTimeout(err) && !errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
		}
		return nil, err
	}
	defer resp.Body.Close()

	// 先读 body 再判状态码：4xx/5xx 的响应内容也要进入错误，便于审计。
	payload, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(payload)),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	var out entity.ChatResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("解析 chat 响应失败: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, errors.New("chat 响应没有 choices")
	}
	out.Raw = payload
	return &out, nil
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}
