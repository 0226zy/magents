package config

import (
	"os"
	"strings"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

// ProviderSpec 是一个模型提供商的默认端点与模型。
type ProviderSpec struct {
	KeyEnv  string
	BaseURL string
	Model   string
}

// providerSpecs 与 Python 版 agent.py 的 provider 分支一一对应。
var providerSpecs = map[string]ProviderSpec{
	"dashscope": {
		KeyEnv:  "DASHSCOPE_API_KEY",
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "qwen3.7-plus",
	},
	"siliconflow": {
		KeyEnv:  "SILICONFLOW_API_KEY",
		BaseURL: "https://api.siliconflow.cn/v1",
		Model:   "Qwen/Qwen3-235B-A22B-Thinking-2507",
	},
	"doubao": {
		KeyEnv:  "DOUBAO_API_KEY",
		BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		Model:   "doubao-seed-1-6-thinking-250715",
	},
	"kimi": {
		KeyEnv:  "KIMI_API_KEY",
		BaseURL: "https://api.moonshot.cn/v1",
		Model:   "kimi-k3",
	},
	"openrouter": {
		KeyEnv:  "OPENROUTER_API_KEY",
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   "google/gemini-3.5-flash",
	},
}

// SupportedProviders 返回全部可选 provider（含别名），供 CLI 的 choices 提示。
func SupportedProviders() []string {
	return []string{"dashscope", "qwen", "bailian", "siliconflow", "doubao", "kimi", "moonshot", "openrouter"}
}

// NormalizeProvider 统一别名：qwen / bailian 都是 dashscope，moonshot 就是 kimi。
func NormalizeProvider(provider string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(provider)); normalized {
	case "", "moonshot":
		return "kimi"
	case "qwen", "bailian":
		return "dashscope"
	default:
		return normalized
	}
}

// Resolve 确定最终使用的 provider、API Key、端点与模型。
//
// 沿用 Python 的通用兜底：首选 provider 没有 Key 但设置了 OPENROUTER_API_KEY 时，
// 整体回落到 openrouter。此时若模型名不含 "/"，说明它是直连 provider 的模型名，
// 在 OpenRouter 上不可用，置空改用 OpenRouter 默认模型。
func (c *Config) Resolve() error {
	LoadDotEnv()

	provider := NormalizeProvider(c.Provider)
	spec, known := providerSpecs[provider]
	if !known {
		return derr.ErrNoAPIKey
	}

	apiKey := c.APIKey
	if apiKey == "" {
		apiKey = os.Getenv(spec.KeyEnv)
	}

	if apiKey == "" {
		fallbackKey := os.Getenv("OPENROUTER_API_KEY")
		if fallbackKey == "" {
			return derr.ErrNoAPIKey
		}
		provider = "openrouter"
		spec = providerSpecs[provider]
		apiKey = fallbackKey
		c.FellBackToRouter = true
		if !strings.Contains(c.Model, "/") {
			c.Model = ""
		}
	}

	c.ResolvedProvider = provider
	c.APIKey = apiKey
	if c.BaseURL == "" {
		c.BaseURL = spec.BaseURL
	}
	if c.Model == "" {
		c.Model = spec.Model
	}
	if base := os.Getenv("DASHSCOPE_BASE_URL"); provider == "dashscope" && base != "" {
		c.BaseURL = base
	}
	return nil
}
