package react

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/port"
)

const goldenPath = "../../../testdata/golden/latest.json"

type goldenRun struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Trace    []struct {
		Iteration int    `json:"iteration"`
		Type      string `json:"type"`
		Tool      string `json:"tool"`
	} `json:"trace"`
	APITurns []struct {
		Kind     string          `json:"kind"`
		Response json.RawMessage `json:"response"`
	} `json:"api_turns"`
}

func loadGoldenRun(t *testing.T) goldenRun {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatalf("读取黄金数据失败: %v", err)
	}
	var evidence struct {
		Run goldenRun `json:"run"`
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatalf("解析黄金数据失败: %v", err)
	}
	return evidence.Run
}

// replayGolden 用归档里的 chat 响应序列驱动 Go 的 ReAct 循环，fiber 调用打桩。
func replayGolden(t *testing.T, run goldenRun) (Result, *fakeChat) {
	t.Helper()
	chat := goldenChat(t, run)
	agent := New(chat, &fakeFormula{output: "（归档的加密搜索载荷）"}, Options{})
	return agent.Run(context.Background(), run.Question, 8), chat
}

// replayGoldenWith 同上，但允许换一个 fiber 打桩实现（例如回放真实耗时）。
func replayGoldenWith(t *testing.T, run goldenRun, formula port.FormulaRunner) Result {
	t.Helper()
	agent := New(goldenChat(t, run), formula, Options{})
	return agent.Run(context.Background(), run.Question, 8)
}

func goldenChat(t *testing.T, run goldenRun) *fakeChat {
	t.Helper()
	var responses []*entity.ChatResponse
	for _, turn := range run.APITurns {
		if turn.Kind != "chat_completion" {
			continue
		}
		var response entity.ChatResponse
		if err := json.Unmarshal(turn.Response, &response); err != nil {
			t.Fatalf("解析归档 chat 响应失败: %v", err)
		}
		responses = append(responses, &response)
	}
	if len(responses) != 5 {
		t.Fatalf("归档应有 5 次 chat 调用，实际 %d", len(responses))
	}
	return &fakeChat{responses: responses}
}

func countByIteration(trace []Step, stepType StepType) map[int]int {
	counts := map[int]int{}
	for _, step := range trace {
		if step.Type == stepType {
			counts[step.Iteration]++
		}
	}
	return counts
}

// TestGoldenTraceReplay 断言轨迹按迭代分组后的搜索次数与归档一致。
//
// action/observation 的交错顺序会因并行化而不同，所以只比对分组与计数。
func TestGoldenTraceReplay(t *testing.T) {
	run := loadGoldenRun(t)

	result, chat := replayGolden(t, run)

	if result.Answer != run.Answer {
		t.Fatalf("最终答案与归档不一致\n归档: %.80s...\n实际: %.80s...", run.Answer, result.Answer)
	}
	if chat.calls != 5 {
		t.Fatalf("chat 应调用 5 次，实际 %d", chat.calls)
	}
	if result.IsFailure() {
		t.Fatal("归档运行是成功的，回放不应判为失败")
	}

	searches := countByIteration(result.Trace, StepAction)
	want := []int{2, 4, 5, 4, 0}
	total := 0
	for i, count := range want {
		iteration := i + 1
		if searches[iteration] != count {
			t.Fatalf("第 %d 轮搜索次数应为 %d，实际 %d（完整分布 %v）",
				iteration, count, searches[iteration], searches)
		}
		total += count
	}
	if total != 15 {
		t.Fatalf("归档共 15 次搜索，实际 %d", total)
	}

	last := result.Trace[len(result.Trace)-1]
	if last.Type != StepAnswer || last.Iteration != 5 {
		t.Fatalf("最后一步应是第 5 轮的 answer，实际 %+v", last)
	}
}

// 每轮的 action / observation / thought 数量都必须与归档逐轮相等，
// 变的只是轮内的先后排布。
func TestGoldenTraceReplayMatchesArchivedStepCountsPerIteration(t *testing.T) {
	run := loadGoldenRun(t)

	result, _ := replayGolden(t, run)

	archived := map[StepType]map[int]int{
		StepThought:     {},
		StepAction:      {},
		StepObservation: {},
		StepAnswer:      {},
	}
	for _, step := range run.Trace {
		if counts, ok := archived[StepType(step.Type)]; ok {
			counts[step.Iteration]++
		}
	}

	for stepType, wantCounts := range archived {
		gotCounts := countByIteration(result.Trace, stepType)
		if len(gotCounts) != len(wantCounts) {
			t.Fatalf("%s 的迭代分布不一致\n归档: %v\n实际: %v", stepType, wantCounts, gotCounts)
		}
		for iteration, want := range wantCounts {
			if gotCounts[iteration] != want {
				t.Fatalf("第 %d 轮的 %s 数量应为 %d，实际 %d", iteration, stepType, want, gotCounts[iteration])
			}
		}
	}
}
