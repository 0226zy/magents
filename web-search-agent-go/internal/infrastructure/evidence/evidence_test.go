package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sample struct {
	Zulu  string `json:"zulu"`
	Alpha string `json:"alpha"`
	Link  string `json:"link"`
}

func isolateCredentials(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.HasSuffix(name, "_API_KEY") {
			t.Setenv(name, "")
		}
	}
	for _, name := range credentialEnvNames {
		t.Setenv(name, "")
	}
}

// 证据里全是带 & 的 URL，默认的 HTML 转义会把它们写成 \u0026。
func TestMarshalKeepsURLsReadableAndFieldOrder(t *testing.T) {
	payload, err := Marshal(sample{Zulu: "z", Alpha: "a", Link: "https://x.test/s?q=1&r=2<3>"})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	text := string(payload)
	if strings.Contains(text, `\u0026`) || strings.Contains(text, `\u003c`) {
		t.Fatalf("不应转义 HTML 字符: %s", text)
	}
	if !strings.Contains(text, "q=1&r=2<3>") {
		t.Fatalf("URL 被改写了: %s", text)
	}
	// struct 的字段顺序即键顺序，不会像 map 那样被重排。
	if strings.Index(text, `"zulu"`) > strings.Index(text, `"alpha"`) {
		t.Fatalf("键顺序应随字段声明顺序: %s", text)
	}
	if !strings.HasSuffix(text, "}\n") {
		t.Fatalf("应以换行结尾（对齐 Python 的 + \"\\n\"）: %q", text)
	}
	if !strings.Contains(text, "\n  \"zulu\"") {
		t.Fatalf("应使用两空格缩进: %s", text)
	}
}

func TestMarshalKeepsUnicodeUnescaped(t *testing.T) {
	payload, _ := Marshal(map[string]string{"answer": "东盟有 11 个成员国"})

	if !strings.Contains(string(payload), "东盟有 11 个成员国") {
		t.Fatalf("中文不应被转义: %s", payload)
	}
}

func TestWriteWithChecksumIsSelfConsistent(t *testing.T) {
	isolateCredentials(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "evidence.json")

	digest, err := WriteWithChecksum(path, sample{Alpha: "a"})
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	sum := sha256.Sum256(written)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("摘要与文件内容不符")
	}

	checksum, err := os.ReadFile(filepath.Join(directory, "evidence.sha256"))
	if err != nil {
		t.Fatalf("读取校验和失败: %v", err)
	}
	if string(checksum) != digest+"  evidence.json\n" {
		t.Fatalf("校验和文件格式不对: %q", checksum)
	}
}

// 含假 Key 的载荷必须被拒绝，且目标文件不被创建。
func TestWriteRefusesPayloadCarryingAConfiguredKey(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("MOONSHOT_API_KEY", "sk-fake-credential-value")
	directory := t.TempDir()
	path := filepath.Join(directory, "evidence.json")

	_, err := WriteWithChecksum(path, sample{Alpha: "泄露了 sk-fake-credential-value 这个值"})

	if !errors.Is(err, ErrCredentialLeak) {
		t.Fatalf("应拒绝写入，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "MOONSHOT_API_KEY") {
		t.Fatalf("错误应指出是哪个变量: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("拒绝写入时不应创建文件")
	}
}

func TestAssertCredentialFreeRejectsAuthorizationHeaders(t *testing.T) {
	isolateCredentials(t)

	err := AssertCredentialFree(`{"request":{"headers":{"Authorization":"Bearer xxx"}}}`)

	if !errors.Is(err, ErrCredentialLeak) {
		t.Fatalf("应拒绝，实际 %v", err)
	}
}

func TestAssertCredentialFreeAcceptsCleanEvidence(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("MOONSHOT_API_KEY", "sk-fake-credential-value")

	clean := `{"request":{"method":"GET","url":"https://api.moonshot.cn/v1/formulas"},"http_status":200}`
	if err := AssertCredentialFree(clean); err != nil {
		t.Fatalf("干净证据不应被拒: %v", err)
	}
	if CredentialValueRecorded(clean) {
		t.Fatal("干净证据的 credential_value_recorded 应为 false")
	}
}

// 太短的密钥值不参与扫描：在正文里撞车的概率远高于真正泄露的概率。
func TestShortCredentialsAreNotScanned(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("MOONSHOT_API_KEY", "abc")

	if err := AssertCredentialFree("这段话里有 abc 三个字母"); err != nil {
		t.Fatalf("不应因过短的值误报: %v", err)
	}
}

func TestCredentialScanCoversAnyApiKeyVariable(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("SOME_VENDOR_API_KEY", "sk-another-fake-value")

	err := AssertCredentialFree("payload 里混进了 sk-another-fake-value")

	if !errors.Is(err, ErrCredentialLeak) {
		t.Fatalf("应扫描所有 *_API_KEY 变量，实际 %v", err)
	}
}

func TestAppendJSONLKeepsEveryRecord(t *testing.T) {
	isolateCredentials(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")

	for _, answer := range []string{"第一问", "第二问"} {
		if err := AppendJSONL(path, sample{Alpha: answer}); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}

	contents, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("交互模式每问都应留痕，实际 %d 行", len(lines))
	}
	if !strings.Contains(lines[0], "第一问") || !strings.Contains(lines[1], "第二问") {
		t.Fatalf("记录被覆盖了: %v", lines)
	}
}

func TestAppendJSONLRefusesLeakyRecord(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("MOONSHOT_API_KEY", "sk-fake-credential-value")
	path := filepath.Join(t.TempDir(), "session.jsonl")

	err := AppendJSONL(path, sample{Alpha: "sk-fake-credential-value"})

	if !errors.Is(err, ErrCredentialLeak) {
		t.Fatalf("应拒绝，实际 %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("拒绝时不应创建文件")
	}
}

func TestTimestampMatchesPythonStrftime(t *testing.T) {
	at := time.Date(2026, 7, 30, 15, 44, 42, 0, time.UTC)

	if got := Timestamp(at); got != "20260730T154442Z" {
		t.Fatalf("时间戳格式不对: %s", got)
	}
}

func TestCurrentHostReportsGoRuntime(t *testing.T) {
	host := CurrentHost()

	if host.Platform == "" || host.Go == "" || host.Machine == "" {
		t.Fatalf("主机信息不完整: %+v", host)
	}
	if !strings.HasPrefix(host.Go, "go") {
		t.Fatalf("应记录 Go 版本: %s", host.Go)
	}
}
