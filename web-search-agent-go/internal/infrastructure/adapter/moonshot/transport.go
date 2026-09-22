package moonshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// NewHTTPClient 返回协议层用的 HTTP 客户端。
//
// Timeout 刻意为零：实测单次 chat 最慢 41 秒、全流程 230 秒，超时预算由调用方
// 用 context.WithTimeout 给每次调用单独设置，这样 chat 与 Formula 两类调用可以
// 各自计时，而不是共享一个客户端级别的挂钟。
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 0}
}

// transport 是三个端点共用的带鉴权请求器。
// 凭据只存在这里，永远不进入 APITurn。
type transport struct {
	apiKey string
	http   *http.Client
}

func newTransport(apiKey string, httpClient *http.Client) transport {
	if httpClient == nil {
		httpClient = NewHTTPClient()
	}
	return transport{apiKey: apiKey, http: httpClient}
}

// do 发一次请求并把 body 完整读回。
//
// 读 body 与判状态码的顺序刻意与 agent.py:188-189、251-252 一致：先拿到 body，
// 再决定是否报错，这样 4xx 响应的内容也能进入证据。
func (t transport) do(ctx context.Context, method, url string, body []byte) (*int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, nil, normalizeTransportError(err)
	}
	defer resp.Body.Close()

	payload, readErr := io.ReadAll(resp.Body)
	status := statusPtr(resp.StatusCode)
	if readErr != nil {
		return status, nil, normalizeTransportError(readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return status, payload, &StatusError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(payload)),
		}
	}
	return status, payload, nil
}

// normalizeTransportError 把传输层超时归一成 context.DeadlineExceeded，
// 让上层的 IsTimeout 与错误提示文案有统一的判别依据。
func normalizeTransportError(err error) error {
	if IsTimeout(err) && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
	}
	return err
}

// newTurnError 构造失败凭据，尽量把 provider 的原始 body 一起留下。
func newTurnError(err error, payload []byte) *TurnError {
	turnError := &TurnError{
		Class:   errorClass(err),
		Message: err.Error(),
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return turnError
	}
	if json.Valid(payload) {
		turnError.Response = append(json.RawMessage(nil), payload...)
	} else {
		turnError.ResponseText = string(payload)
	}
	return turnError
}

// errorClass 给证据一个粗粒度的错误分类，对应 Python 的 type(exc).__name__。
func errorClass(err error) string {
	switch {
	case IsRateLimit(err):
		return "RateLimitError"
	case IsTimeout(err):
		return "APITimeoutError"
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return "APIStatusError"
	}
	return "RuntimeError"
}
