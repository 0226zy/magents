// Package adapter 提供基础设施适配器：模型客户端、内置工具、MCP 管理、
// 轨迹落盘、触发器、Unipile 客户端与证据写入。
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// LLMConfig 是模型客户端的连接配置。
type LLMConfig struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
	Timeout  time.Duration
}

// LLMClient 调用 OpenAI 兼容的 /chat/completions，实现 port.ChatModelPort。
// dashscope / siliconflow / doubao / kimi / openrouter 共用同一套协议，只是端点不同。
type LLMClient struct {
	cfg  LLMConfig
	http *http.Client
}

// NewLLMClient 创建模型客户端。
func NewLLMClient(cfg LLMConfig) (*LLMClient, error) {
	if cfg.APIKey == "" {
		return nil, derr.ErrNoAPIKey
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, fmt.Errorf("%w: base_url / model 不能为空", derr.ErrNoAPIKey)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 180 * time.Second
	}
	return &LLMClient{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

// Model 返回实际使用的模型名。
func (c *LLMClient) Model() string { return c.cfg.Model }

// Provider 返回实际使用的提供商。
func (c *LLMClient) Provider() string { return c.cfg.Provider }

type chatRequest struct {
	Model       string                  `json:"model"`
	Messages    []entity.Message        `json:"messages"`
	Tools       []entity.ToolDefinition `json:"tools,omitempty"`
	ToolChoice  string                  `json:"tool_choice,omitempty"`
	Temperature float64                 `json:"temperature"`
	MaxTokens   int                     `json:"max_tokens,omitempty"`
}

type apiErrorPayload struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Chat 携带完整消息列表与工具声明调用一次模型。
func (c *LLMClient) Chat(ctx context.Context, messages []entity.Message,
	tools []entity.ToolDefinition, opts entity.ChatOptions) (*entity.ChatResponse, error) {

	toolChoice := opts.ToolChoice
	if len(tools) == 0 {
		// 没有工具时带 tool_choice=auto 会被部分 provider 拒绝。
		toolChoice = ""
	}

	payload, err := json.Marshal(chatRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  toolChoice,
		Temperature: entity.SafeTemperature(c.cfg.Model, opts.Temperature),
		MaxTokens:   opts.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求模型失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("读取模型响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var payload apiErrorPayload
		if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
			return nil, fmt.Errorf("%w: HTTP %d %s: %s", derr.ErrProviderAPI,
				resp.StatusCode, payload.Error.Type, payload.Error.Message)
		}
		return nil, fmt.Errorf("%w: HTTP %d: %s", derr.ErrProviderAPI,
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp entity.ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("解析模型响应失败: %w", err)
	}
	chatResp.Raw = body
	return &chatResp, nil
}
