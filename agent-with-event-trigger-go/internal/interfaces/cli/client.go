// Package cli 提供三个命令行入口的参数解析与输出渲染。
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client 是事件服务的 HTTP 客户端。
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient 构造客户端。超时放到 10 分钟：一次事件处理可能包含多轮模型调用与工具执行，
// 用默认超时会在服务端还在干活时把连接掐掉。
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// Get 发起 GET 请求并返回解析后的 JSON。
func (c *Client) Get(ctx context.Context, path string) (map[string]any, error) {
	return c.request(ctx, http.MethodGet, path, nil)
}

// Post 发起 POST 请求并返回解析后的 JSON。
func (c *Client) Post(ctx context.Context, path string, body any) (map[string]any, error) {
	return c.request(ctx, http.MethodPost, path, body)
}

func (c *Client) request(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("%s 返回的不是 JSON: %s", path, truncate(string(raw), 200))
		}
	}
	if resp.StatusCode >= 400 {
		detail, _ := payload["detail"].(string)
		if detail == "" {
			detail = resp.Status
		}
		return payload, fmt.Errorf("%s %s 返回 %d: %s", method, path, resp.StatusCode, detail)
	}
	return payload, nil
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
