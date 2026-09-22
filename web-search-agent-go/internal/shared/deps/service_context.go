// Package deps 是依赖注入容器（组合根）：
// 在这里把 infrastructure 的适配器实例化，注入 application 层。
// 这是全项目唯一允许同时 import application 与 infrastructure 的地方。
package deps

import (
	"github.com/0226zy/magents/web-search-agent-go/internal/application/session"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/adapter/moonshot"
)

// NewSession 按解析好的后端装配一个会话：Recorder → HTTP 客户端 →
// Chat/Formula 适配器 → ReAct Agent → Session。
func NewSession(backend entity.Backend, options session.Options) *session.Session {
	recorder := entity.NewRecorder()
	httpClient := moonshot.NewHTTPClient()
	chat := moonshot.NewChatClient(backend.APIKey, backend.BaseURL, backend.Model, recorder, httpClient)
	formula := moonshot.NewFormulaClient(backend.APIKey, backend.BaseURL, moonshot.DefaultFormulaURI, recorder, httpClient)
	return session.Open(backend, chat, formula, recorder, options)
}
