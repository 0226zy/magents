// Package derr 存放领域哨兵错误：application 与 infrastructure 用 errors.Is 判断，
// 不用字符串比较。
package derr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
)

// ErrRateLimit 对应 Python SDK 的 RateLimitError，由 HTTP 429 判别。
var ErrRateLimit = errors.New("provider 速率限制")

// StatusError 是非 2xx 响应。Body 保留 provider 的原始文本，便于排障。
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// Is 让 errors.Is(err, ErrRateLimit) 对 429 成立。
func (e *StatusError) Is(target error) bool {
	return target == ErrRateLimit && e.StatusCode == 429
}

// IsRateLimit 判断错误是否为 provider 的速率限制。
func IsRateLimit(err error) bool { return errors.Is(err, ErrRateLimit) }

// IsTimeout 判断错误是否为请求超时。
//
// 超时预算由调用方经 context.WithTimeout 给出（见 adapter.NewHTTPClient 的说明），
// 因此这里认 context.DeadlineExceeded，同时兼顾传输层自身报出的超时。
func IsTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
