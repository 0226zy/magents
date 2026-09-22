// Package moonshot 是唯一接触 HTTP 适配器：chat completion 与 Formula 两组端点。
// 协议 DTO 已上移到 domain/entity，本包用别名引用（见 aliases.go）。
package moonshot

import (
	"encoding/json"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/contract"
)

// HasWebSearchDeclaration 判断声明里是否含名为 web_search 的 function 工具。
func HasWebSearchDeclaration(tools []json.RawMessage) bool {
	return contract.HasWebSearch(tools)
}

// ReasoningSafeTemperature 复刻 agent.py:20-25。
// 推理模型（Kimi K3、GPT-5 系）只接受 temperature=1；其余模型按调用方请求的值。
// Python 版此处返回 int 1 而 requested 是 float（缺陷 P2-9），Go 统一为 float64。
func ReasoningSafeTemperature(model string, requested float64) float64 {
	normalized := normalizeModel(model)
	if containsAny(normalized, "kimi-k3", "gpt-5") {
		return 1
	}
	return requested
}
