package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
)

// UnipileConfig 是 Unipile 客户端配置。
type UnipileConfig struct {
	DSN         string
	AccessToken string
	Timeout     time.Duration
	// Transport 供测试注入假服务，生产留空。
	Transport http.RoundTripper
}

// UnipileClient 是实验 6-1 的邮件/日历适配器，实现 port.MailboxPort。
// 每次调用都留下一条脱敏回执——证据要能证明调用确实发生过。
type UnipileClient struct {
	baseURL string
	token   string
	http    *http.Client

	mu    sync.Mutex
	calls []mailbox.CallReceipt
}

// NewUnipileClient 构造客户端。DSN 不带协议时补 https。
func NewUnipileClient(cfg UnipileConfig) (*UnipileClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, derr.ErrMissingCredentials
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.DSN), "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 45 * time.Second
	}
	return &UnipileClient{
		baseURL: base,
		token:   strings.TrimSpace(cfg.AccessToken),
		http:    &http.Client{Timeout: cfg.Timeout, Transport: cfg.Transport},
	}, nil
}

// BaseURL 返回实际使用的端点。
func (c *UnipileClient) BaseURL() string { return c.baseURL }

// Calls 返回全部调用回执的副本。
func (c *UnipileClient) Calls() []mailbox.CallReceipt {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]mailbox.CallReceipt, len(c.calls))
	copy(out, c.calls)
	return out
}

// Close 释放连接。
func (c *UnipileClient) Close() { c.http.CloseIdleConnections() }

type requestSpec struct {
	method    string
	path      string
	query     url.Values
	jsonBody  any
	multipart map[string]string
	expected  int
}

// do 执行一次请求并记录回执。
func (c *UnipileClient) do(ctx context.Context, spec requestSpec) (any, error) {
	if spec.expected == 0 {
		spec.expected = http.StatusOK
	}
	started := time.Now()

	body, contentType, err := encodeBody(spec)
	if err != nil {
		return nil, err
	}

	endpoint := c.baseURL + spec.path
	if len(spec.query) > 0 {
		endpoint += "?" + spec.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, spec.method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", c.token)
	req.Header.Set("User-Agent", "magents-event-trigger-go/1.0")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.record(mailbox.CallReceipt{
			Method:           spec.method,
			Path:             spec.path,
			Request:          redactedRequest(spec),
			CredentialScheme: "X-API-KEY",
			Success:          false,
			LatencySeconds:   roundSeconds(time.Since(started)),
			ErrorType:        fmt.Sprintf("%T", err),
			ErrorTitle:       truncateText(err.Error(), 300),
		})
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		payload = map[string]any{
			"non_json_sha256": mailbox.SHA256Hex(raw),
			"bytes":           len(raw),
		}
	}

	status := resp.StatusCode
	receipt := mailbox.CallReceipt{
		Method:           spec.method,
		Path:             spec.path,
		Request:          redactedRequest(spec),
		CredentialScheme: "X-API-KEY",
		Status:           &status,
		Success:          status == spec.expected,
		LatencySeconds:   roundSeconds(time.Since(started)),
		ResponseSHA256:   mailbox.SHA256Hex(raw),
		ResponseBytes:    len(raw),
		ResponseShape:    responseShape(payload),
	}
	apiErr := &mailbox.APIError{Method: spec.method, Path: spec.path, Status: status}
	if object, ok := payload.(map[string]any); ok {
		apiErr.Type = stringOrEmpty(object["type"])
		apiErr.Title = stringOrEmpty(object["title"])
		receipt.ErrorType = apiErr.Type
		receipt.ErrorTitle = apiErr.Title
	}
	c.record(receipt)

	if status != spec.expected {
		return nil, apiErr
	}
	return payload, nil
}

func encodeBody(spec requestSpec) (io.Reader, string, error) {
	switch {
	case spec.multipart != nil:
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		keys := make([]string, 0, len(spec.multipart))
		for key := range spec.multipart {
			keys = append(keys, key)
		}
		sort.Strings(keys) // 字段顺序确定，便于复现与比对
		for _, key := range keys {
			if err := writer.WriteField(key, spec.multipart[key]); err != nil {
				return nil, "", err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return &buffer, writer.FormDataContentType(), nil
	case spec.jsonBody != nil:
		encoded, err := json.Marshal(spec.jsonBody)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(encoded), "application/json", nil
	default:
		return nil, "", nil
	}
}

// redactedRequest 把请求参数脱敏后写进回执。
func redactedRequest(spec requestSpec) any {
	params := map[string]any{}
	for key, values := range spec.query {
		if len(values) == 1 {
			params[key] = values[0]
		} else {
			params[key] = values
		}
	}
	multipartCopy := map[string]any{}
	for key, value := range spec.multipart {
		multipartCopy[key] = value
	}
	return mailbox.Redact(map[string]any{
		"params":    params,
		"json":      spec.jsonBody,
		"multipart": multipartCopy,
	})
}

func (c *UnipileClient) record(receipt mailbox.CallReceipt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, receipt)
}

// ListAccounts 列出全部账号。
func (c *UnipileClient) ListAccounts(ctx context.Context) ([]mailbox.ProviderObject, error) {
	payload, err := c.do(ctx, requestSpec{
		method: http.MethodGet, path: "/api/v1/accounts",
		query: url.Values{"limit": {"100"}},
	})
	if err != nil {
		return nil, err
	}
	return objectList(payload, "items")
}

// ListFolders 列出账号下的邮件文件夹。
func (c *UnipileClient) ListFolders(ctx context.Context, accountID string) ([]mailbox.ProviderObject, error) {
	payload, err := c.do(ctx, requestSpec{
		method: http.MethodGet, path: "/api/v1/folders",
		query: url.Values{"account_id": {accountID}},
	})
	if err != nil {
		return nil, err
	}
	return objectList(payload, "items")
}

// ListEmails 轮询收件箱。
func (c *UnipileClient) ListEmails(ctx context.Context, accountID string, after time.Time,
	folder string, limit int) ([]mailbox.ProviderObject, error) {

	query := url.Values{
		"account_id": {accountID},
		"after":      {mailbox.IsoMillis(after)},
		"limit":      {strconv.Itoa(clamp(limit, 1, 250))},
		"meta_only":  {"false"},
	}
	if folder != "" {
		query.Set("folder", folder)
	}
	payload, err := c.do(ctx, requestSpec{
		method: http.MethodGet, path: "/api/v1/emails", query: query,
	})
	if err != nil {
		return nil, err
	}
	return objectList(payload, "items")
}

// GetEmail 读取单封邮件。
func (c *UnipileClient) GetEmail(ctx context.Context, emailID string) (mailbox.ProviderObject, error) {
	payload, err := c.do(ctx, requestSpec{
		method: http.MethodGet, path: "/api/v1/emails/" + emailID,
	})
	if err != nil {
		return nil, err
	}
	return singleObject(payload)
}

// UpdateEmailFolders 移动邮件到指定文件夹。
func (c *UnipileClient) UpdateEmailFolders(ctx context.Context, emailID string,
	folders []string) (mailbox.ProviderObject, error) {

	payload, err := c.do(ctx, requestSpec{
		method: http.MethodPut, path: "/api/v1/emails/" + emailID,
		jsonBody: map[string]any{"folders": folders},
	})
	if err != nil {
		return nil, err
	}
	return singleObject(payload)
}

// SendEmail 发送一封邮件。Unipile 的发信端点返回 201。
func (c *UnipileClient) SendEmail(ctx context.Context, accountID, recipient, subject,
	body string) (mailbox.ProviderObject, error) {

	to, err := json.Marshal([]map[string]string{
		{"display_name": "Experiment 6-1 mailbox", "identifier": recipient},
	})
	if err != nil {
		return nil, err
	}
	payload, err := c.do(ctx, requestSpec{
		method: http.MethodPost, path: "/api/v1/emails", expected: http.StatusCreated,
		multipart: map[string]string{
			"account_id": accountID,
			"to":         string(to),
			"subject":    subject,
			"body":       body,
		},
	})
	if err != nil {
		return nil, err
	}
	return singleObject(payload)
}

// ListCalendars 列出账号下的日历。
func (c *UnipileClient) ListCalendars(ctx context.Context, accountID string) ([]mailbox.ProviderObject, error) {
	payload, err := c.do(ctx, requestSpec{
		method: http.MethodGet, path: "/api/v1/calendars",
		query: url.Values{"account_id": {accountID}, "limit": {"100"}},
	})
	if err != nil {
		return nil, err
	}
	return objectList(payload, "data")
}

// ListCalendarEvents 查询日历事件。查询窗口前后各放宽一天，
// 以覆盖跨时区与跨日的事件——漏掉它们会把冲突误判成空闲。
func (c *UnipileClient) ListCalendarEvents(ctx context.Context, accountID, calendarID string,
	start, end time.Time) ([]mailbox.ProviderObject, error) {

	payload, err := c.do(ctx, requestSpec{
		method: http.MethodGet,
		path:   "/api/v1/calendars/" + calendarID + "/events",
		query: url.Values{
			"account_id":       {accountID},
			"start":            {mailbox.IsoMillis(start.Add(-24 * time.Hour))},
			"end":              {mailbox.IsoMillis(end.Add(24 * time.Hour))},
			"expand_recurring": {"true"},
			"limit":            {"250"},
		},
	})
	if err != nil {
		return nil, err
	}
	return objectList(payload, "data")
}

func objectList(payload any, key string) ([]mailbox.ProviderObject, error) {
	object, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: 响应不是对象", derr.ErrMailboxAPI)
	}
	raw, ok := object[key].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: 响应缺少 %s 列表", derr.ErrMailboxAPI, key)
	}
	out := make([]mailbox.ProviderObject, 0, len(raw))
	for _, item := range raw {
		if typed, ok := item.(map[string]any); ok {
			out = append(out, typed)
		}
	}
	return out, nil
}

func singleObject(payload any) (mailbox.ProviderObject, error) {
	object, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: 响应不是对象", derr.ErrMailboxAPI)
	}
	return object, nil
}

// responseShape 记录响应的形状而非内容：对象记键名，其它记类型。
func responseShape(payload any) any {
	object, ok := payload.(map[string]any)
	if !ok {
		return fmt.Sprintf("%T", payload)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringOrEmpty(value any) string {
	text, _ := value.(string)
	return text
}

func roundSeconds(d time.Duration) float64 {
	return float64(int64(d.Seconds()*1000)) / 1000
}

func clamp(value, low, high int) int {
	return min(max(value, low), high)
}

func truncateText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
