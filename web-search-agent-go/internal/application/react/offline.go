package react

import (
	"fmt"
	"io"
	"os"
)

// DefaultOfflineQuestion 是离线演示的默认问题。
const DefaultOfflineQuestion = "Moonshot AI 的 Context Caching 是什么技术？"

// OfflineResult 是离线演示的产物。
type OfflineResult struct {
	Question string `json:"question"`
	Trace    []Step `json:"trace"`
	Answer   string `json:"answer"`
}

// RunOfflineDemo 离线演示 ReAct 循环——无需 API Key 或联网。
//
// 本函数不调用真实搜索，而是回放一段「示例轨迹」，用来直观展示本章讲的
// 「想→做→看→想→做→看」循环：模型先思考，再调用 web_search 行动，观察结果后
// 继续思考，最终综合出答案。轨迹内容仅为教学示例，不代表真实搜索返回。
func RunOfflineDemo(question string, verbose bool, out io.Writer) OfflineResult {
	if question == "" {
		question = DefaultOfflineQuestion
	}
	if out == nil {
		out = os.Stdout
	}

	trace := []Step{
		{Iteration: 1, Type: StepThought,
			Content: "用户想了解 Context Caching。这是 Moonshot 的特性，我需要先搜索官方说明，确认它的定义和作用。"},
		{Iteration: 1, Type: StepAction, Tool: "web_search",
			Args: map[string]any{"query": "Moonshot AI Context Caching 是什么"}},
		{Iteration: 1, Type: StepObservation, Tool: "web_search",
			Content: "（示例结果）Context Caching 是一种上下文缓存机制：把重复使用的前缀" +
				"（如长系统提示、文档）缓存在服务端，后续请求命中缓存即可复用，" +
				"从而降低重复计算与费用。"},
		{Iteration: 2, Type: StepThought,
			Content: "已知大致定义，但还缺少适用场景。再搜一次它的典型用途以便答得更完整。"},
		{Iteration: 2, Type: StepAction, Tool: "web_search",
			Args: map[string]any{"query": "Context Caching 适用场景 计费"}},
		{Iteration: 2, Type: StepObservation, Tool: "web_search",
			Content: "（示例结果）常见于多轮对话、长文档反复问答、固定系统提示等场景；" +
				"命中缓存的 token 通常按更低价格计费，并能显著降低首字延迟。"},
		{Iteration: 3, Type: StepAnswer,
			Content: "Context Caching（上下文缓存）是 Moonshot AI 提供的一种机制：将重复使用的" +
				"上下文前缀缓存在服务端，后续请求复用缓存内容，从而降低重复计算、减少费用、" +
				"并加快响应。它特别适合长系统提示、长文档反复问答、多轮对话等场景。" +
				"（本段来自离线示例轨迹，非真实搜索结果。）"},
	}

	if verbose {
		for _, step := range trace {
			fmt.Fprintln(out, FormatTraceStep(step, DefaultTraceMaxLen))
		}
	}

	answer := ""
	for _, step := range trace {
		if step.Type == StepAnswer {
			answer = step.Content
			break
		}
	}
	return OfflineResult{Question: question, Trace: trace, Answer: answer}
}
