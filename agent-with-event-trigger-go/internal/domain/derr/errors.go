// Package derr 存放领域哨兵错误：跨层判断用 errors.Is，不用字符串比较。
package derr

import "errors"

var (
	// ErrUnknownEventType 表示事件类型字符串不在 EventType 枚举里，映射为 HTTP 400。
	ErrUnknownEventType = errors.New("未知的事件类型")
	// ErrInvalidTodoStatus 表示 TODO 状态字符串非法；整批更新不生效。
	ErrInvalidTodoStatus = errors.New("非法的 TODO 状态")
	// ErrUnknownTool 表示模型请求了一个没有注册的工具名。
	ErrUnknownTool = errors.New("未注册的工具")
	// ErrInvalidToolArguments 表示模型生成的工具参数不是合法 JSON。
	ErrInvalidToolArguments = errors.New("工具参数不是合法 JSON")
	// ErrMCPLoading 表示 MCP 工具正在加载中，映射为 HTTP 409。
	ErrMCPLoading = errors.New("MCP 工具正在加载")
	// ErrAgentNotReady 表示 Agent 尚未初始化，映射为 HTTP 500。
	ErrAgentNotReady = errors.New("Agent 尚未初始化")
	// ErrMaxIterations 表示达到循环上限仍未得到最终回答；不算请求失败。
	ErrMaxIterations = errors.New("达到 max_iterations 上限，模型仍在请求工具调用")
	// ErrAmbiguousClassification 表示邮件同时命中或未命中任何场景规则。
	ErrAmbiguousClassification = errors.New("邮件分类结果不唯一")
	// ErrInvalidMeetingInterval 表示会议邮件缺少合法的 START_UTC/END_UTC 区间。
	ErrInvalidMeetingInterval = errors.New("会议邮件缺少合法的 START_UTC/END_UTC 区间")
	// ErrCredentialLeak 表示待落盘的证据里出现了凭据痕迹，拒绝写入。
	ErrCredentialLeak = errors.New("证据内容疑似包含凭据")
	// ErrMissingCredentials 表示实验 6-1 缺少 Unipile 凭据，判定为 blocked。
	ErrMissingCredentials = errors.New("缺少 UNIPILE_DSN / UNIPILE_ACCESS_TOKEN")
	// ErrNoAPIKey 表示所选 provider 与 OpenRouter 兜底都没有可用的 API Key。
	ErrNoAPIKey = errors.New("未找到可用的模型 API Key")
	// ErrProviderAPI 表示模型 provider 返回了非 200 响应。
	ErrProviderAPI = errors.New("模型 API 返回错误")
	// ErrMailboxAPI 表示邮箱 provider 返回了非预期状态码。
	ErrMailboxAPI = errors.New("邮箱 API 返回错误")
)
