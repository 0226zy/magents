package evidence

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrCredentialLeak 表示待落盘的内容里出现了凭据痕迹。
var ErrCredentialLeak = errors.New("证据内容疑似包含凭据")

// credentialEnvNames 是本项目会用到、或常与之共存于同一个 .env 的密钥变量。
// 除此之外，任何以 _API_KEY 结尾的环境变量也会被一并扫描。
var credentialEnvNames = []string{
	"MOONSHOT_API_KEY",
	"KIMI_API_KEY",
	"OPENROUTER_API_KEY",
	"DASHSCOPE_API_KEY",
	"ARK_API_KEY",
	"GEMINI_API_KEY",
	"OPENAI_API_KEY",
	"ANTHROPIC_API_KEY",
	"DEEPSEEK_API_KEY",
}

// minCredentialLength 是参与扫描的最短密钥长度。
// 太短的值（几个字符）在正文里撞车的概率远高于真正泄露的概率。
const minCredentialLength = 8

// AssertCredentialFree 在任何证据落盘前调用。
//
// 本项目单次运行写入的证据可达 500KB，里面有完整的 messages 数组与全部请求体。
// Python 版把 "credential_value_recorded": false 硬编码进证据，却没有任何代码去
// 核实这件事；一旦有人「顺手把 headers 也记上」就会静默泄露。这个函数补上那道闸。
//
// 三条规则：
//  1. 任一已配置密钥的值出现在内容中 → 拒绝
//  2. 内容同时（大小写不敏感）含 "authorization" 与 "bearer" → 拒绝
//  3. 其余放行
func AssertCredentialFree(payloads ...string) error {
	secrets := configuredCredentials()
	for _, payload := range payloads {
		for name, secret := range secrets {
			if strings.Contains(payload, secret) {
				return fmt.Errorf("%w: 环境变量 %s 的值出现在了证据里", ErrCredentialLeak, name)
			}
		}
		lowered := strings.ToLower(payload)
		if strings.Contains(lowered, "authorization") && strings.Contains(lowered, "bearer") {
			return fmt.Errorf("%w: 证据里出现了 Authorization / Bearer 请求头", ErrCredentialLeak)
		}
	}
	return nil
}

// CredentialValueRecorded 报告内容里是否残留凭据。
//
// 证据文件里的同名字段由它推导，而不是写死成 false。
func CredentialValueRecorded(payloads ...string) bool {
	return AssertCredentialFree(payloads...) != nil
}

// configuredCredentials 收集当前环境里所有非空的密钥值。
func configuredCredentials() map[string]string {
	secrets := map[string]string{}
	add := func(name string) {
		value := strings.TrimSpace(os.Getenv(name))
		if len(value) >= minCredentialLength {
			secrets[name] = value
		}
	}
	for _, name := range credentialEnvNames {
		add(name)
	}
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.HasSuffix(name, "_API_KEY") {
			add(name)
		}
	}
	return secrets
}
