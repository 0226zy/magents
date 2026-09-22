package react

import (
	"fmt"
	"time"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/contract"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/derr"
)

// 失败兜底文案，定义在 internal/contract，与验收层共享同一套判定。
const (
	SearchErrorPrefix    = contract.SearchErrorPrefix
	MaxIterationsMessage = contract.MaxIterationsMessage
	NoInfoMessage        = contract.NoInfoMessage
	truncationNote       = contract.TruncationNote
)

// FailureKind 显式表达失败成因。
//
// Python 版只返回错误字符串，靠 is_failure_answer 前缀匹配来判定，既可能误判
// （正常答案恰好以「搜索过程中出现错误」开头），也容易被调用方忘记判定而把失败
// 当成功。Go 版把成因提升为类型，文案仍逐字保留以维持兼容。
type FailureKind int

// 失败成因。
const (
	FailureNone FailureKind = iota
	FailureProvider
	FailureMaxIterations
	FailureNoInfo
	FailureTruncated
)

// Result 是一次 ReAct 运行的结果。Run 永不返回 error，失败以兜底文案 + FailureKind 表达。
type Result struct {
	Answer  string
	Failure FailureKind
	Trace   []Step
}

// IsFailure 报告本次运行是否未能正常作答。
//
// FailureTruncated 不算失败：截断的答案仍是「有最终答案」，
// Python 的 is_failure_answer 同样不匹配截断文案。
func (r Result) IsFailure() bool {
	return r.Failure != FailureNone && r.Failure != FailureTruncated
}

// IsFailureAnswer 判断一段答案文本是否为失败兜底。
// 保留给只拿得到字符串的调用方。
func IsFailureAnswer(answer string) bool {
	return contract.IsFailureAnswer(answer)
}

// errorHint 给 provider 故障补一句可操作的提示。
//
// 交互模式下最常见的两种失败是速率限制和请求超时，而且两者常常连在一起：
// 429 会触发重试，重试把超时预算耗完之后，用户只看到一句「请求超时」，
// 很容易误判成网络问题或模型能力问题。
func errorHint(err error, timeout time.Duration) string {
	switch {
	case derr.IsRateLimit(err):
		return "（触发了服务商的速率限制，请降低请求频率或稍后重试）"
	case derr.IsTimeout(err):
		budget := "超时未返回"
		if timeout > 0 {
			budget = fmt.Sprintf("超过 %.0f 秒未返回", timeout.Seconds())
		}
		return fmt.Sprintf(
			"（请求%s。kimi-k3 以 reasoning_effort=max 运行，"+
				"单次调用可能需要一到数分钟；若持续超时，可调大环境变量 "+
				"SEARCH_TIMEOUT，并检查日志中是否有 429 导致的反复重试）", budget)
	}
	return ""
}
