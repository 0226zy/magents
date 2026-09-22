// Package adapter 提供基础设施适配器：Moonshot API 客户端。
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/0226zy/magents/kimi-chat-go/internal/domain/entity"
)

// MoonshotConfig 是模型 API 连接配置（由 infrastructure/config 读入后传入）。
type MoonshotConfig struct {
	APIKey      string
	BaseURL     string
	Model       string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
}

// MoonshotClient 调用 Moonshot 的 OpenAI 兼容 chat completions 接口，
// 实现 domain/port.ChatModelPort。
type MoonshotClient struct {
	cfg  MoonshotConfig
	http *http.Client
}

// NewMoonshotClient 创建客户端。APIKey / BaseURL / Model 必填。
func NewMoonshotClient(cfg MoonshotConfig) (*MoonshotClient, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("api_key 为空：请在 config.yaml 中填写，或设置环境变量 MOONSHOT_API_KEY / KIMI_API_KEY")
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, errors.New("base_url / model 不能为空")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &MoonshotClient{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

// APIError 是 Moonshot 返回的错误载荷。
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (e APIError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("%s: %s", e.Type, e.Message)
	}
	return e.Message
}

// errorPayload 对应 {"error": {...}} 的响应外壳。
type errorPayload struct {
	Err APIError `json:"error"`
}

// Chat 携带完整消息列表调用一次模型。
func (c *MoonshotClient) Chat(ctx context.Context, messages []entity.Message) (*entity.ChatResponse, error) {
	payload, err := json.Marshal(entity.ChatRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		Temperature: c.cfg.Temperature,
		MaxTokens:   c.cfg.MaxTokens,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var ep errorPayload
		if json.Unmarshal(body, &ep) == nil && ep.Err.Message != "" {
			return nil, fmt.Errorf("API 返回 %d: %s", resp.StatusCode, ep.Err.Error())
		}
		return nil, fmt.Errorf("API 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp entity.ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return &chatResp, nil
}
