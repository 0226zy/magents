package contract

import "strings"

// 失败兜底文案。ReAct 循环用它们作答，验收断言 final_answer_observed 用它们
// 判断「是否真的给出了最终答案」，因此逐字保留 Python 版的措辞。
const (
	SearchErrorPrefix    = "搜索过程中出现错误"
	MaxIterationsMessage = "抱歉，搜索过程超过了最大迭代次数，请稍后重试。"
	NoInfoMessage        = "抱歉，我无法获取足够的信息来回答您的问题。"
	// TruncationNote 标注回答被 max_tokens 截断。它刻意不算失败：
	// 截断的答案仍然是一个最终答案。
	TruncationNote = "（注意：回答因达到 max_tokens 上限被截断，请增大 max_tokens 后重试。）"
)

// IsFailureAnswer 判断一段答案文本是否为失败兜底（未能正常作答）。
func IsFailureAnswer(answer string) bool {
	return strings.HasPrefix(answer, SearchErrorPrefix) ||
		answer == MaxIterationsMessage ||
		answer == NoInfoMessage
}
