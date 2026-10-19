package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

func TestLoadUsesDefaultsWhenFileMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("配置文件不存在时不应报错: %v", err)
	}
	if cfg.MaxIterations != 20 || cfg.Server.Port != 8000 {
		t.Errorf("默认值不对: %+v", cfg)
	}
}

func TestLoadReadsYAMLAndNormalizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(
		"provider: moonshot\nmax_iterations: 0\nserver:\n  port: 9100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "kimi" {
		t.Errorf("provider = %q，moonshot 应规范化为 kimi", cfg.Provider)
	}
	if cfg.MaxIterations != 20 {
		t.Errorf("非法的 max_iterations 应回落默认值，实际 %d", cfg.MaxIterations)
	}
	if cfg.Server.Port != 9100 {
		t.Errorf("端口 = %d", cfg.Server.Port)
	}
}

// hints 用指针区分「没配」与「显式 false」，没配时沿用默认开启。
func TestHintConfigDistinguishesUnsetFromFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(
		"hints:\n  enable_timestamps: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	hints := cfg.HintConfig()
	if hints.EnableTimestamps {
		t.Error("显式 false 应生效")
	}
	if !hints.EnableTodoList {
		t.Error("未配置的开关应保持默认开启")
	}
}

// 环境变量格式错误时回落默认值，不能让服务起不来。
func TestEnvIntToleratesGarbage(t *testing.T) {
	t.Setenv("AGENT_PORT", "不是数字")
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 8000 {
		t.Errorf("端口 = %d，格式错误时应回落 8000", cfg.Server.Port)
	}
}

func TestNormalizeProviderAliases(t *testing.T) {
	cases := map[string]string{
		"qwen": "dashscope", "bailian": "dashscope", "moonshot": "kimi",
		"": "kimi", "KIMI": "kimi", "openrouter": "openrouter",
	}
	for input, want := range cases {
		if got := NormalizeProvider(input); got != want {
			t.Errorf("NormalizeProvider(%q) = %q，期望 %q", input, got, want)
		}
	}
}

func TestResolveUsesProviderDefaults(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("KIMI_API_KEY", "kimi-key")

	cfg := defaults()
	if err := cfg.Resolve(); err != nil {
		t.Fatal(err)
	}
	if cfg.ResolvedProvider != "kimi" || cfg.APIKey != "kimi-key" {
		t.Errorf("解析结果 = %+v", cfg)
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		t.Error("应填上 provider 默认端点与模型")
	}
}

// 首选 provider 没有 Key 但有 OPENROUTER_API_KEY 时整体回落；
// 此时直连模型名在 OpenRouter 上不可用，必须置空改用其默认模型。
func TestResolveFallsBackToOpenRouter(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "router-key")

	cfg := defaults()
	cfg.Model = "kimi-k3"
	if err := cfg.Resolve(); err != nil {
		t.Fatal(err)
	}
	if cfg.ResolvedProvider != "openrouter" || !cfg.FellBackToRouter {
		t.Fatalf("应回落到 openrouter，实际 = %+v", cfg)
	}
	if cfg.Model == "kimi-k3" {
		t.Error("不含斜杠的直连模型名应被置空改用 OpenRouter 默认模型")
	}
}

// 已经是 OpenRouter 形式的模型名（含斜杠）要保留。
func TestResolveKeepsSlashedModelOnFallback(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "router-key")

	cfg := defaults()
	cfg.Model = "google/gemini-3.5-flash"
	if err := cfg.Resolve(); err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "google/gemini-3.5-flash" {
		t.Errorf("模型名 = %q，含斜杠时应保留", cfg.Model)
	}
}

func TestResolveFailsWithoutAnyKey(t *testing.T) {
	clearProviderEnv(t)

	cfg := defaults()
	if err := cfg.Resolve(); !errors.Is(err, derr.ErrNoAPIKey) {
		t.Fatalf("错误 = %v，期望 ErrNoAPIKey", err)
	}
}

// clearProviderEnv 清空所有可能影响解析的凭据环境变量，
// 避免开发机上真实的 .env 把测试结果带偏。
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"DASHSCOPE_API_KEY", "SILICONFLOW_API_KEY", "DOUBAO_API_KEY",
		"KIMI_API_KEY", "OPENROUTER_API_KEY", "DASHSCOPE_BASE_URL",
	} {
		t.Setenv(name, "")
	}
}
