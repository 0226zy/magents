// Package react 实现第一章的 ReAct 循环：想（thought）→ 做（action）→
// 看（observation）→ 最终答案（answer）。
package react

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// StepType 是 ReAct 轨迹的步骤类型。
type StepType string

// 轨迹的四种步骤。
const (
	StepThought     StepType = "thought"
	StepAction      StepType = "action"
	StepObservation StepType = "observation"
	StepAnswer      StepType = "answer"
)

// Step 的 JSON 标签必须与 Python 版一致：轨迹会写入证据文件，
// 并被 acceptance 校验器按 type / iteration / tool 字段读取。
type Step struct {
	Iteration int            `json:"iteration"`
	Type      StepType       `json:"type"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	Content   string         `json:"content,omitempty"`
}

// stepLabels 是轨迹步骤的展示图标与中文标签。
var stepLabels = map[StepType][2]string{
	StepThought:     {"💭", "思考"},
	StepAction:      {"🔧", "行动"},
	StepObservation: {"👀", "观察"},
	StepAnswer:      {"✅", "最终答案"},
}

// DefaultTraceMaxLen 是渲染一条轨迹时展示的最大字数。
const DefaultTraceMaxLen = 500

// FormatTraceStep 把一条 ReAct 轨迹步骤渲染成一行可读文本。
//
// 这正是本章强调的「轨迹（trajectory）」——用户消息、模型思考、工具调用、
// 工具结果都被清晰地区分开来，让 ReAct 循环「想→做→看」一目了然。
func FormatTraceStep(step Step, maxLen int) string {
	label, ok := stepLabels[step.Type]
	if !ok {
		label = [2]string{"•", string(step.Type)}
	}
	prefix := fmt.Sprintf("%s [%d] %s", label[0], step.Iteration, label[1])

	if step.Type == StepAction {
		return fmt.Sprintf("%s: 调用工具 %s  参数=%s", prefix, step.Tool, encodeArgs(step.Args))
	}

	content := []rune(strings.TrimSpace(step.Content))
	if maxLen > 0 && len(content) > maxLen {
		return fmt.Sprintf("%s: %s…（省略 %d 字）", prefix, string(content[:maxLen]), len(content)-maxLen)
	}
	return fmt.Sprintf("%s: %s", prefix, string(content))
}

// encodeArgs 渲染工具参数。
//
// 关掉 HTML 转义，否则搜索词里的 & < > 会变成 \u0026 这类看不懂的东西。
// 与 Python 的 json.dumps 相比这里没有键值之间的空格，纯展示差异。
func encodeArgs(args map[string]any) string {
	if args == nil {
		args = map[string]any{}
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(args); err != nil {
		return "{}"
	}
	return strings.TrimRight(buf.String(), "\n")
}
