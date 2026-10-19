package mailbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	emailAddressPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	// simulationPattern 用于验收门：控制面里出现这些词说明掺了模拟数据。
	simulationPattern = regexp.MustCompile(`(?i)\b(mock(?:ed)?|placeholder|synthetic|simulat(?:ed|ion))\b`)
	// credentialPattern 匹配常见的密钥形状，落盘前兜底扫描。
	credentialPattern = regexp.MustCompile(`\b(?:sk|gh[opusr])-[A-Za-z0-9_-]{12,}\b`)
)

// SHA256Hex 返回字节串的十六进制摘要。
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA256String 返回字符串的十六进制摘要。
func SHA256String(s string) string { return SHA256Hex([]byte(s)) }

func shortHash(s string) string { return SHA256String(s)[:20] }

// ContainsSimulationMarker 报告文本里是否出现模拟数据的痕迹。
func ContainsSimulationMarker(s string) bool { return simulationPattern.MatchString(s) }

// ContainsCredentialShape 报告文本里是否出现密钥形状的字符串。
func ContainsCredentialShape(s string) bool { return credentialPattern.MatchString(s) }

// Redact 以空 key 为起点递归脱敏。
func Redact(value any) any { return RedactKeyed(value, "") }

// RedactKeyed 递归脱敏，保留结构形状但抹去身份与正文。
//
// 规则按固定顺序判断，顺序本身是语义的一部分：邮箱地址无论挂在哪个 key 下都要先哈希，
// 而 subject 必须在身份规则之前原样留下（实验主题不含个人信息，却是证明场景保真的唯一凭据）。
func RedactKeyed(value any, key string) any {
	lower := strings.ToLower(key)

	// 1. 整体是邮箱地址的字符串
	if text, ok := value.(string); ok && emailAddressPattern.MatchString(text) {
		return map[string]any{
			"sha256":  shortHash(text),
			"present": true,
			"kind":    "email_address",
		}
	}

	// 2. 凭据类字段
	for _, token := range []string{"token", "authorization", "api_key", "credential"} {
		if strings.Contains(lower, token) {
			return "<redacted>"
		}
	}

	// 3. 正文类字段：只留摘要与长度
	switch lower {
	case "body", "body_plain", "text", "content":
		if text, ok := value.(string); ok {
			return map[string]any{
				"sha256": SHA256String(text),
				// Python 的 len() 数的是码点，用 len([]byte) 会让中文正文的字符数对不上。
				"characters": utf8.RuneCountInString(text),
			}
		}
	case "subject":
		return value
	}

	// 4. 身份类字段：截断哈希
	if strings.HasSuffix(lower, "_id") || isIdentityKey(lower) {
		switch typed := value.(type) {
		case string:
			return map[string]any{"sha256": shortHash(typed), "present": typed != ""}
		case int:
			return identityHashOfNumber(typed != 0, numberString(value))
		case int64:
			return identityHashOfNumber(typed != 0, numberString(value))
		case float64:
			// JSON 解出来的整数是 float64；Python 的 str(1) 是 "1" 而不是 "1.0"，
			// 两边摘要要能对上就必须按整数渲染。
			return identityHashOfNumber(typed != 0, numberString(value))
		}
	}

	// 5. 容器递归。slice 用父级 key 递归，与 Python 一致。
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, child := range typed {
			out[childKey] = RedactKeyed(child, childKey)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = RedactKeyed(child, key)
		}
		return out
	}

	return value
}

func identityHashOfNumber(present bool, text string) map[string]any {
	return map[string]any{"sha256": shortHash(text), "present": present}
}

// numberString 把数值渲染成 Python str() 的形式：整数不带小数点。
func numberString(value any) string {
	switch typed := value.(type) {
	case float64:
		if typed == math.Trunc(typed) && math.Abs(typed) < 1e15 {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func isIdentityKey(lower string) bool {
	switch lower {
	case "id", "identifier", "email", "from", "to":
		return true
	default:
		return false
	}
}

// CanonicalJSON 对应 Python 的
// json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))。
//
// Go 的 encoding/json 对 map 本来就按键排序，但默认会把 < > & 转义成 \u003c 之类，
// 必须关掉 HTML 转义，否则两种语言产出的字节不同——而「凭据未出现在证据中」这类
// 子串检查成立的前提，正是字节一致。
func CanonicalJSON(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return []byte("null")
	}
	return bytes.TrimRight(buffer.Bytes(), "\n")
}

// CanonicalJSONString 是 CanonicalJSON 的字符串形式。
func CanonicalJSONString(value any) string { return string(CanonicalJSON(value)) }
