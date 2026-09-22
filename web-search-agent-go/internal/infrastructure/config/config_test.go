package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate 固定全部 provider 环境变量，既隔离开发者本机凭据，
// 也让 .env 加载无从覆盖（Load 只填充尚未设置的键）。
func isolate(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MOONSHOT_API_KEY", "KIMI_API_KEY", "KIMI_BASE_URL", "SEARCH_TIMEOUT",
		"OPENROUTER_API_KEY", "OPENROUTER_BASE_URL", "OPENROUTER_MODEL",
	} {
		t.Setenv(key, "")
	}
}

func TestAPIKeyPrefersExplicitThenMoonshotThenLegacy(t *testing.T) {
	isolate(t)
	t.Setenv("KIMI_API_KEY", "legacy")
	if got := APIKey(""); got != "legacy" {
		t.Fatalf("应回退到 KIMI_API_KEY，实际 %q", got)
	}

	t.Setenv("MOONSHOT_API_KEY", "primary")
	if got := APIKey(""); got != "primary" {
		t.Fatalf("MOONSHOT_API_KEY 应优先，实际 %q", got)
	}
	if got := APIKey("explicit"); got != "explicit" {
		t.Fatalf("显式传入应优先，实际 %q", got)
	}
}

func TestCredentialSourceEnvNamesTheVariableInUse(t *testing.T) {
	isolate(t)
	t.Setenv("KIMI_API_KEY", "legacy")
	if got := CredentialSourceEnv(); got != "KIMI_API_KEY" {
		t.Fatalf("实际 %q", got)
	}
	t.Setenv("MOONSHOT_API_KEY", "primary")
	if got := CredentialSourceEnv(); got != "MOONSHOT_API_KEY" {
		t.Fatalf("实际 %q", got)
	}
}

func TestBaseURLTreatsBlankOverrideAsUnset(t *testing.T) {
	isolate(t)
	if got := BaseURL("  "); got != DefaultBaseURL {
		t.Fatalf("空白覆盖值应被忽略，实际 %q", got)
	}
	t.Setenv("KIMI_BASE_URL", "   ")
	if got := BaseURL(""); got != DefaultBaseURL {
		t.Fatalf("留空的 KIMI_BASE_URL 不应把端点清空，实际 %q", got)
	}
	t.Setenv("KIMI_BASE_URL", "https://proxy.example/v1")
	if got := BaseURL(""); got != "https://proxy.example/v1" {
		t.Fatalf("实际 %q", got)
	}
	if got := BaseURL("https://cli.example/v1"); got != "https://cli.example/v1" {
		t.Fatalf("命令行覆盖优先，实际 %q", got)
	}
}

func TestSearchTimeoutFallsBackOnInvalidValues(t *testing.T) {
	isolate(t)
	if got := SearchTimeout(); got != DefaultSearchTimeout {
		t.Fatalf("默认应为 180s，实际 %v", got)
	}
	t.Setenv("SEARCH_TIMEOUT", "12.5")
	if got := SearchTimeout(); got != 12500*time.Millisecond {
		t.Fatalf("实际 %v", got)
	}
	for _, bad := range []string{"abc", "-1", "0"} {
		t.Setenv("SEARCH_TIMEOUT", bad)
		if got := SearchTimeout(); got != DefaultSearchTimeout {
			t.Fatalf("非法值 %q 应退回默认，实际 %v", bad, got)
		}
	}
}

func TestResolveUsesMoonshotWhenCredentialPresent(t *testing.T) {
	isolate(t)
	t.Setenv("MOONSHOT_API_KEY", "primary")

	backend, err := Resolve("", "", "", false)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if backend.Provider != "moonshot" || backend.UsingOpenRouter() {
		t.Fatalf("应直连 Moonshot: %+v", backend)
	}
	if backend.Model != DefaultModel || backend.BaseURL != DefaultBaseURL {
		t.Fatalf("默认模型 / 端点不对: %+v", backend)
	}
}

// P1-3：缺失 Moonshot 凭据时，Python 版静默改走 OpenRouter，
// 把「搜索 Agent」降级为凭记忆答题。这里必须拒绝启动。
func TestResolveRefusesSilentNoSearchFallback(t *testing.T) {
	isolate(t)
	t.Setenv("OPENROUTER_API_KEY", "fallback")

	_, err := Resolve("", "", "", false)
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("应拒绝启动，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "-allow-no-search") {
		t.Fatalf("错误必须指出怎么显式放行: %v", err)
	}
}

func TestResolveAllowsExplicitNoSearchFallback(t *testing.T) {
	isolate(t)
	t.Setenv("OPENROUTER_API_KEY", "fallback")

	backend, err := Resolve("", "", "kimi-k3", true)
	if err != nil {
		t.Fatalf("显式放行后应成功: %v", err)
	}
	if !backend.UsingOpenRouter() {
		t.Fatalf("应走兜底通道: %+v", backend)
	}
	if backend.Model != "moonshotai/kimi-k2.6" {
		t.Fatalf("kimi-k3 未在 OpenRouter 托管，应映射: %q", backend.Model)
	}
	if backend.BaseURL != defaultOpenRouterBaseURL {
		t.Fatalf("端点不对: %q", backend.BaseURL)
	}
}

func TestResolveFailsWithoutAnyCredential(t *testing.T) {
	isolate(t)
	if _, err := Resolve("", "", "", true); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("实际 %v", err)
	}
}

func TestParseDotEnvLine(t *testing.T) {
	cases := []struct {
		line      string
		key       string
		value     string
		supported bool
	}{
		{"MOONSHOT_API_KEY=abc", "MOONSHOT_API_KEY", "abc", true},
		{"  export KIMI_BASE_URL=https://x/v1  ", "KIMI_BASE_URL", "https://x/v1", true},
		{`QUOTED="quoted value"`, "QUOTED", "quoted value", true},
		{"# comment", "", "", false},
		{"", "", "", false},
		{"no-equals-sign", "", "", false},
	}
	for _, tc := range cases {
		key, value, ok := parseDotEnvLine(tc.line)
		if ok != tc.supported || key != tc.key || value != tc.value {
			t.Fatalf("%q 解析为 (%q,%q,%v)", tc.line, key, value, ok)
		}
	}
}

func TestApplyDotEnvNeverOverridesExistingEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("MOONSHOT_API_KEY", "from-environment")
	// 真正未设置的键才会被 .env 填充；isolate 把它设成了空串，这里撤销。
	os.Unsetenv("SEARCH_TIMEOUT")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	contents := "MOONSHOT_API_KEY=from-dotenv\nSEARCH_TIMEOUT=42\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("写入 .env 失败: %v", err)
	}

	applyDotEnv(path)

	if got := os.Getenv("MOONSHOT_API_KEY"); got != "from-environment" {
		t.Fatalf("已存在的环境变量不应被覆盖，实际 %q", got)
	}
	if got := os.Getenv("SEARCH_TIMEOUT"); got != "42" {
		t.Fatalf("缺失的键应由 .env 填充，实际 %q", got)
	}
}
