// Package contract 存放「运行时产出」与「验收校验」之间共享的判定规则：
// 工具声明长什么样算合格，答案文本什么情况下算失败兜底。
//
// 它刻意是一个不含 HTTP 的叶子包。协议层（internal/moonshot）与 ReAct 循环
// （internal/react）在跑的时候用它，验收层（internal/acceptance）在读归档证据
// 时用同一套判定，两边不会因为各自实现而产生分歧；验收层也因此不必依赖任何
// 网络代码，得以保持 Python 版 validate() 那样的纯函数性质。
package contract

import (
	"bytes"
	"encoding/json"
)

// view 仅用于校验声明内容，不用于重新编码。
// 权威 schema 一律以 json.RawMessage 原样透传，见 moonshot.ChatRequest.Tools。
type view struct {
	Type     string `json:"type"`
	Function struct {
		Name       string          `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// HasWebSearch 判断声明里是否含名为 web_search 的 function 工具。
//
// 对应 run_experiment_1_2.py:61-68：Python 版要求 parameters 是 dict
// （不是缺失、不是 null），这里对应「解码后非空且首个非空白字节为 '{'」。
func HasWebSearch(tools []json.RawMessage) bool {
	for _, raw := range tools {
		var v view
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if v.Type != "function" || v.Function.Name != "web_search" {
			continue
		}
		if isJSONObject(v.Function.Parameters) {
			return true
		}
	}
	return false
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}
