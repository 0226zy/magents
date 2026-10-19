package mailbox

import (
	"fmt"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

// APIError 是 provider 返回的非预期状态码。
//
// 它定义在领域层而不是适配器里：实验的终态判定要区分 401（凭据无效，实验没跑起来，
// 判 blocked）与其它错误（实验跑了但失败），这条规则属于领域知识。
type APIError struct {
	Method string
	Path   string
	Status int
	Type   string
	Title  string
}

func (e *APIError) Error() string {
	detail := e.Type
	if detail == "" {
		detail = e.Title
	}
	if detail == "" {
		detail = "API error"
	}
	return fmt.Sprintf("%s %s returned %d: %s", e.Method, e.Path, e.Status, detail)
}

// Unwrap 让 errors.Is(err, derr.ErrMailboxAPI) 成立。
func (e *APIError) Unwrap() error { return derr.ErrMailboxAPI }

// ErrOffline 表示没有可用的 provider 凭据，实验无法真正跑起来。
var ErrOffline = fmt.Errorf("%w: 缺少 UNIPILE_DSN / UNIPILE_ACCESS_TOKEN", derr.ErrMissingCredentials)
