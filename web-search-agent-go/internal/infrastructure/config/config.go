// Package config 解析运行 web-search-agent 所需的凭据与端点。
//
// 对应 Python 版的 config.py 与 agentbook.providers 的解析逻辑。
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
)

const (
	// DefaultModel 是实验 1-2 要求的精确模型。
	DefaultModel = "kimi-k3"
	// DefaultBaseURL 是 Moonshot 官方端点。
	DefaultBaseURL = "https://api.moonshot.cn/v1"
	// DefaultMaxIterations 与 agent 的默认值保持一致。
	DefaultMaxIterations = 5
	// DefaultSearchTimeout 同时作用于 chat completion 与 Formula 调用。
	// kimi-k3 以 reasoning_effort=max 运行，单次 completion 常需 1-3 分钟。
	DefaultSearchTimeout = 180 * time.Second

	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	defaultOpenRouterModel   = "moonshotai/kimi-k2.6"
)

// ErrNoCredential 表示既没有 Moonshot 凭据也没有 OpenRouter 兜底凭据。
var ErrNoCredential = errors.New("未设置 MOONSHOT_API_KEY / KIMI_API_KEY，也没有 OPENROUTER_API_KEY 作为兜底")

var loadOnce sync.Once

// Load 读取最近的 .env（从工作目录向上查找）。
// Python 版在 config.py 里重复导入并重复调用了 load_dotenv（缺陷 P2-8），
// 这里用 sync.Once 保证只加载一次，且已存在的环境变量优先。
func Load() {
	loadOnce.Do(func() {
		path, err := findDotEnv()
		if err != nil {
			return
		}
		applyDotEnv(path)
	})
}

func findDotEnv() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func applyDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, value)
	}
}

func parseDotEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}

// APIKey 返回 Moonshot 凭据：显式传入优先，其次 MOONSHOT_API_KEY，最后是旧名 KIMI_API_KEY。
func APIKey(override string) string {
	Load()
	if override != "" {
		return override
	}
	if key := os.Getenv("MOONSHOT_API_KEY"); key != "" {
		return key
	}
	return os.Getenv("KIMI_API_KEY")
}

// CredentialSourceEnv 返回凭据实际来自哪个环境变量，用于证据文件。
func CredentialSourceEnv() string {
	Load()
	if os.Getenv("MOONSHOT_API_KEY") != "" {
		return "MOONSHOT_API_KEY"
	}
	return "KIMI_API_KEY"
}

// BaseURL 返回 chat 与 Formula 共用的端点。
// 空值视为「未配置」，避免 .env 里留空的 KIMI_BASE_URL 把端点清空。
func BaseURL(override string) string {
	Load()
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("KIMI_BASE_URL")); v != "" {
		return v
	}
	return DefaultBaseURL
}

// SearchTimeout 读取 SEARCH_TIMEOUT（秒）。非法值退回默认值。
func SearchTimeout() time.Duration {
	Load()
	raw := strings.TrimSpace(os.Getenv("SEARCH_TIMEOUT"))
	if raw == "" {
		return DefaultSearchTimeout
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		return DefaultSearchTimeout
	}
	return time.Duration(seconds * float64(time.Second))
}

// Resolve 解析后端。对应 Python 的 resolve_llm_backend，但收紧了兜底行为：
// 缺失 Moonshot 凭据时 Python 会静默改走 OpenRouter，把「搜索 Agent」降级为
// 凭记忆答题（缺陷 P1-3）。这里要求调用方用 allowNoSearch 显式承认这一点。
// Backend 的领域定义在 domain/entity，这里只负责解析并构造。
func Resolve(apiKey, baseURL, model string, allowNoSearch bool) (entity.Backend, error) {
	Load()
	resolvedKey := APIKey(apiKey)
	if model == "" {
		model = DefaultModel
	}
	if resolvedKey != "" {
		return entity.Backend{
			APIKey:   resolvedKey,
			BaseURL:  BaseURL(baseURL),
			Model:    model,
			Provider: "moonshot",
		}, nil
	}

	fallbackKey := os.Getenv("OPENROUTER_API_KEY")
	if fallbackKey == "" {
		return entity.Backend{}, ErrNoCredential
	}
	if !allowNoSearch {
		return entity.Backend{}, fmt.Errorf(
			"%w：只找到 OPENROUTER_API_KEY，而 Moonshot Formula web_search 在 OpenRouter 上不可用；"+
				"若确实要让模型凭记忆作答，请显式传 -allow-no-search", ErrNoCredential)
	}

	fallbackURL := strings.TrimSpace(os.Getenv("OPENROUTER_BASE_URL"))
	if fallbackURL == "" {
		fallbackURL = defaultOpenRouterBaseURL
	}
	return entity.Backend{
		APIKey:   fallbackKey,
		BaseURL:  fallbackURL,
		Model:    mapModelToOpenRouter(model),
		Provider: "openrouter",
	}, nil
}

// mapModelToOpenRouter 把裸模型 id 映射成 OpenRouter 的等价 id。
// kimi-k3 未在 OpenRouter 上托管，映射为 kimi-k2.6。
func mapModelToOpenRouter(model string) string {
	if strings.Contains(model, "/") {
		return model
	}
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "kimi-"), strings.HasPrefix(lower, "moonshot"):
		return defaultOpenRouterModel
	case strings.HasPrefix(lower, "gpt-"), strings.HasPrefix(lower, "o1"), strings.HasPrefix(lower, "o3"):
		return "openai/" + model
	case strings.HasPrefix(lower, "claude-"):
		return "anthropic/" + model
	case strings.HasPrefix(lower, "gemini"):
		return "google/" + model
	case strings.HasPrefix(lower, "deepseek-"):
		return "deepseek/" + model
	}
	if override := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")); override != "" {
		return override
	}
	return model
}
