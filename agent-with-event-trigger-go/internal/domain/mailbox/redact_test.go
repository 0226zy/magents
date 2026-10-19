package mailbox

import (
	"strings"
	"testing"
)

func TestRedactHidesSecretsButKeepsShape(t *testing.T) {
	redacted := Redact(map[string]any{
		"access_token": "unit-secret",
		"subject":      "Meeting invitation",
		"nested":       map[string]any{"api_key": "another-secret"},
	})

	encoded := CanonicalJSONString(redacted)
	if strings.Contains(encoded, "unit-secret") || strings.Contains(encoded, "another-secret") {
		t.Fatalf("脱敏结果仍含明文密钥: %s", encoded)
	}
	if !strings.Contains(encoded, "Meeting invitation") {
		t.Error("非敏感字段不应被脱敏")
	}
}

// Python 的 str(1) 是 "1" 而不是 "1.0"。JSON 解出来的整数在 Go 里是 float64，
// 若按浮点渲染，两种实现对同一份数据会算出不同的哈希。
func TestRedactRendersIntegralFloatsWithoutDecimalPoint(t *testing.T) {
	encoded := CanonicalJSONString(Redact(map[string]any{"api_key": float64(1)}))
	if strings.Contains(encoded, "1.0") {
		t.Errorf("整数被渲染成浮点: %s", encoded)
	}
}

func TestCanonicalJSONDoesNotEscapeHTML(t *testing.T) {
	// 默认的 Go encoder 会把 & 转成 \u0026，和 Python 的 ensure_ascii=False 对不上。
	if got := CanonicalJSONString(map[string]any{"a": "x&y"}); !strings.Contains(got, "x&y") {
		t.Errorf("CanonicalJSON = %s，不应转义 HTML 字符", got)
	}
}

func TestContainsCredentialShapeCatchesTokens(t *testing.T) {
	if !ContainsCredentialShape(`{"access_token":"sk-abcdefghijklmnopqrstuvwxyz0123"}`) {
		t.Error("应识别出凭据形状")
	}
	if ContainsCredentialShape(`{"subject":"hello"}`) {
		t.Error("普通文本被误判为凭据")
	}
}
