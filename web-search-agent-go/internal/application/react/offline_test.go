package react

import (
	"bytes"
	"strings"
	"testing"
)

func TestOfflineDemoReturnsCompleteDeterministicTrace(t *testing.T) {
	result := RunOfflineDemo("캐싱이 뭐야?", false, nil)

	if result.Question != "캐싱이 뭐야?" {
		t.Fatalf("问题不对: %q", result.Question)
	}
	assertStepTypes(t, result.Trace,
		StepThought, StepAction, StepObservation,
		StepThought, StepAction, StepObservation,
		StepAnswer)
	if result.Answer != result.Trace[len(result.Trace)-1].Content {
		t.Fatal("答案应取自轨迹的 answer 步骤")
	}
	if !strings.Contains(result.Answer, "离线示例轨迹") {
		t.Fatalf("必须说明这是示例而非真实搜索: %q", result.Answer)
	}
}

func TestOfflineDemoUsesDefaultQuestionWhenEmpty(t *testing.T) {
	if got := RunOfflineDemo("", false, nil).Question; got != DefaultOfflineQuestion {
		t.Fatalf("默认问题不对: %q", got)
	}
}

func TestOfflineDemoVerboseModePrintsEachTraceStep(t *testing.T) {
	var buffer bytes.Buffer

	result := RunOfflineDemo("question", true, &buffer)

	output := buffer.String()
	counts := map[string]int{"💭": 2, "🔧": 2, "👀": 2, "✅": 1}
	for icon, want := range counts {
		if got := strings.Count(output, icon); got != want {
			t.Fatalf("%s 期望出现 %d 次，实际 %d 次", icon, want, got)
		}
	}
	if !strings.Contains(output, result.Answer) {
		t.Fatal("verbose 输出应含最终答案")
	}
}

func TestOfflineDemoQuietModePrintsNothing(t *testing.T) {
	var buffer bytes.Buffer

	RunOfflineDemo("question", false, &buffer)

	if buffer.Len() != 0 {
		t.Fatalf("quiet 模式不应有输出: %s", buffer.String())
	}
}
