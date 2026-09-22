package interfaces

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/config"
)

func isolateCredentials(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"MOONSHOT_API_KEY", "KIMI_API_KEY", "OPENROUTER_API_KEY",
		"KIMI_BASE_URL", "SEARCH_TIMEOUT",
	} {
		t.Setenv(name, "")
	}
}

// 离线演示是零 Key、零网络就能跑的入口。
func TestOfflineDemoRunsWithoutCredentials(t *testing.T) {
	isolateCredentials(t)
	var stdout, stderr bytes.Buffer
	output := filepath.Join(t.TempDir(), "demo.json")

	err := RunAgentCLI([]string{"-provider", "offline-demo", "-output", output},
		strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("离线演示不应失败: %v", err)
	}

	printed := stdout.String()
	for _, marker := range []string{"离线演示模式", "💭", "🔧", "👀", "✅", "结果已保存到"} {
		if !strings.Contains(printed, marker) {
			t.Fatalf("输出缺少 %q:\n%s", marker, printed)
		}
	}

	saved, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	var result struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Trace    []struct {
			Type string `json:"type"`
		} `json:"trace"`
	}
	if err := json.Unmarshal(saved, &result); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if len(result.Trace) != 7 {
		t.Fatalf("离线轨迹应有 7 步，实际 %d", len(result.Trace))
	}
	if result.Answer == "" || result.Question == "" {
		t.Fatalf("输出内容不完整: %s", saved)
	}
}

func TestOfflineDemoQuietModePrintsNoTrace(t *testing.T) {
	isolateCredentials(t)
	var stdout, stderr bytes.Buffer

	if err := RunAgentCLI([]string{"-provider", "offline-demo", "-quiet"},
		strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("运行失败: %v", err)
	}

	if strings.Contains(stdout.String(), "💭") {
		t.Fatalf("quiet 模式不应打印轨迹:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "📝 答案") {
		t.Fatal("quiet 模式仍应给出答案")
	}
}

func TestOfflineDemoAcceptsPositionalQuestion(t *testing.T) {
	isolateCredentials(t)
	var stdout, stderr bytes.Buffer

	if err := RunAgentCLI([]string{"-provider", "offline-demo", "-quiet", "什么是", "上下文缓存"},
		strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("运行失败: %v", err)
	}

	// 位置参数用空格拼接，对应 Python 的 " ".join(args.query)。
	if !strings.Contains(stdout.String(), "问题: 什么是 上下文缓存") {
		t.Fatalf("位置参数未正确拼接:\n%s", stdout.String())
	}
}

func TestProviderFlagRejectsUnknownBackend(t *testing.T) {
	isolateCredentials(t)
	var stdout, stderr bytes.Buffer

	err := RunAgentCLI([]string{"-provider", "bogus"}, strings.NewReader(""), &stdout, &stderr)

	if err == nil {
		t.Fatal("未知后端必须报错")
	}
	if !strings.Contains(stderr.String(), "offline-demo") {
		t.Fatalf("错误提示应说明可选值:\n%s", stderr.String())
	}
}

// P1-3：没有 Moonshot 凭据时默认拒绝启动，而不是静默降级为凭记忆答题。
func TestOnlineModeRefusesToStartWithoutSearchCapability(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("OPENROUTER_API_KEY", "fallback-key")
	var stdout, stderr bytes.Buffer

	err := RunAgentCLI([]string{"一个问题"}, strings.NewReader(""), &stdout, &stderr)

	if !errors.Is(err, config.ErrNoCredential) {
		t.Fatalf("应拒绝启动，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "-allow-no-search") {
		t.Fatalf("错误应指出如何显式放行: %v", err)
	}
}
