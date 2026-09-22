package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenPath 指向一次归档的真实运行：21 次 HTTP 调用、5 轮迭代、15 个 fiber。
const goldenPath = "../../testdata/golden/latest.json"

// goldenEvidence 只解码回放需要的两部分：运行载荷，以及 Python 版当时算出的结论。
type goldenEvidence struct {
	Run        Payload `json:"run"`
	Acceptance struct {
		Checks   map[string]bool `json:"checks"`
		Passed   bool            `json:"passed"`
		FiberIDs []string        `json:"fiber_ids"`
		Usage    UsageTotals     `json:"usage"`
	} `json:"acceptance"`
}

func loadGolden(t *testing.T) goldenEvidence {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatalf("读取黄金数据失败: %v", err)
	}
	var evidence goldenEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatalf("解析黄金数据失败: %v", err)
	}
	return evidence
}

// TestGoldenReplay 是跨语言主验收门：把 Python 版留下的真实运行数据喂给 Go 版
// 校验器，18 项 check 的布尔值必须与 Python 版完全相同。
//
// 不存在「合理差异」：任何一项不一致都说明 Go 版的解析或判定有 bug。
// P1-4 收紧了域名判定，但这次归档运行同时引用了 asean.org 与 peraturan.bpk.go.id，
// 按域名后缀匹配依然成立，因此结论不变。
func TestGoldenReplay(t *testing.T) {
	evidence := loadGolden(t)

	result := Validate(evidence.Run, Options{})

	if len(evidence.Acceptance.Checks) != 18 {
		t.Fatalf("归档里应有 18 项 check，实际 %d", len(evidence.Acceptance.Checks))
	}
	for _, entry := range result.Checks.Entries() {
		want, ok := evidence.Acceptance.Checks[entry.Name]
		if !ok {
			t.Fatalf("归档里没有断言 %s", entry.Name)
		}
		if entry.Passed != want {
			t.Fatalf("%s: Python 记录 %v，Go 算出 %v", entry.Name, want, entry.Passed)
		}
	}
	if !result.Passed || !evidence.Acceptance.Passed {
		t.Fatalf("两侧都应判定通过：Go %v / Python %v（未通过项 %v）",
			result.Passed, evidence.Acceptance.Passed, result.FailedChecks())
	}
}

func TestGoldenReplayReproducesFiberReceipts(t *testing.T) {
	evidence := loadGolden(t)

	result := Validate(evidence.Run, Options{})

	if len(result.FiberIDs) != 15 {
		t.Fatalf("应有 15 个 fiber 凭据，实际 %d", len(result.FiberIDs))
	}
	if distinct(result.FiberIDs) != 15 {
		t.Fatalf("fiber id 必须互不相同: %v", result.FiberIDs)
	}
	if len(evidence.Acceptance.FiberIDs) != len(result.FiberIDs) {
		t.Fatalf("与 Python 记录的数量不一致: %d vs %d",
			len(evidence.Acceptance.FiberIDs), len(result.FiberIDs))
	}
	for i, id := range evidence.Acceptance.FiberIDs {
		if result.FiberIDs[i] != id {
			t.Fatalf("第 %d 个 fiber id 不一致: Python %s / Go %s", i, id, result.FiberIDs[i])
		}
	}
}

func TestGoldenReplayReproducesUsageTotals(t *testing.T) {
	evidence := loadGolden(t)

	usage := Validate(evidence.Run, Options{}).Usage

	want := UsageTotals{
		PromptTokens:       54021,
		CompletionTokens:   4102,
		TotalTokens:        58123,
		CachedPromptTokens: 29952,
		ReasoningTokens:    1698,
	}
	if usage != want {
		t.Fatalf("usage 汇总不一致\n期望: %+v\n实际: %+v", want, usage)
	}
	if usage != evidence.Acceptance.Usage {
		t.Fatalf("与 Python 记录不一致\nPython: %+v\nGo: %+v", evidence.Acceptance.Usage, usage)
	}
}

// 归档轨迹是 Python 版「action/observation 交替」的排布。把每轮重排成
// 「先全部 action 再全部 observation」后，18 项结论必须一字不差。
func TestGoldenReplayIsIndifferentToParallelTraceGrouping(t *testing.T) {
	evidence := loadGolden(t)

	original := Validate(evidence.Run, Options{})

	regrouped := evidence.Run
	regrouped.Trace = groupByIteration(evidence.Run.Trace)
	grouped := Validate(regrouped, Options{})

	for i, entry := range original.Checks.Entries() {
		if other := grouped.Checks.Entries()[i]; other.Passed != entry.Passed {
			t.Fatalf("%s 因轨迹重排而改变结论", entry.Name)
		}
	}
	if !grouped.Passed {
		t.Fatalf("重排后仍应通过，未通过项: %v", grouped.FailedChecks())
	}
}

// groupByIteration 保持迭代顺序，但把每轮内的步骤重排成
// thought → 全部 action → 全部 observation → answer。
func groupByIteration(steps []Step) []Step {
	var iterations []int
	byIteration := map[int][]Step{}
	for _, step := range steps {
		if _, seen := byIteration[step.Iteration]; !seen {
			iterations = append(iterations, step.Iteration)
		}
		byIteration[step.Iteration] = append(byIteration[step.Iteration], step)
	}

	regrouped := make([]Step, 0, len(steps))
	for _, iteration := range iterations {
		for _, stepType := range []string{"thought", "action", "observation", "answer"} {
			for _, step := range byIteration[iteration] {
				if step.Type == stepType {
					regrouped = append(regrouped, step)
				}
			}
		}
	}
	return regrouped
}
