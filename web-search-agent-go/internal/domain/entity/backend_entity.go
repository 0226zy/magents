package entity

// Backend 是一次运行最终使用的凭据、端点与模型。
// 由 infrastructure/config 解析得出，application 层只读使用。
type Backend struct {
	APIKey   string
	BaseURL  string
	Model    string
	Provider string // moonshot / openrouter
}

// UsingOpenRouter 表示本次运行走的是兜底通道，此时 Formula web_search 不可用。
func (b Backend) UsingOpenRouter() bool { return b.Provider == "openrouter" }
