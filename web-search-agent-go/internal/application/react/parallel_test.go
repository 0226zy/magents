package react

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// timeScale 把归档里的真实耗时按比例缩小，让这条测试在百毫秒量级内跑完，
// 同时保留各轮之间的相对关系。
const timeScale = 100

// timedFormula 按归档顺序回放每次 fiber 的真实耗时。
//
// 轮内哪个 goroutine 领到哪个耗时无关紧要：各轮是串行推进的，
// 每轮领到的仍是那一轮的那几个耗时，轮内耗时的最大值因此保持不变。
type timedFormula struct {
	mu        sync.Mutex
	durations []time.Duration
}

func (f *timedFormula) Tools(context.Context) ([]json.RawMessage, error) {
	return []json.RawMessage{json.RawMessage(declaration)}, nil
}

func (f *timedFormula) ExecuteFiber(context.Context, string, string) (string, error) {
	f.mu.Lock()
	var wait time.Duration
	if len(f.durations) > 0 {
		wait = f.durations[0]
		f.durations = f.durations[1:]
	}
	f.mu.Unlock()

	time.Sleep(wait)
	return "（归档的加密搜索载荷）", nil
}

// TestParallelFiberExecutionBeatsSerialBudget 用归档运行的真实 fiber 耗时实测
// 并行化收益：Python 版在同一轮里串行执行，15 次调用共 81.8 秒；轮内并行后
// 每轮只需该轮最慢的一次，合计 27.6 秒。
func TestParallelFiberExecutionBeatsSerialBudget(t *testing.T) {
	run := loadGoldenRun(t)
	durations := archivedFiberDurations(t)
	if len(durations) != 15 {
		t.Fatalf("归档应有 15 次 fiber 调用，实际 %d", len(durations))
	}

	var serial time.Duration
	for _, duration := range durations {
		serial += duration
	}

	formula := &timedFormula{durations: scaleAll(durations, timeScale)}
	started := time.Now()
	result := replayGoldenWith(t, run, formula)
	elapsed := time.Since(started)

	if result.IsFailure() {
		t.Fatalf("回放不应失败: %q", result.Answer)
	}

	serialScaled := serial / timeScale
	t.Logf("fiber 段耗时：串行预算 %v（真实 %v），本次并行实测 %v（真实约 %v）",
		serialScaled, serial, elapsed, elapsed*timeScale)

	// 真实数据下的并行预算是 27.6s，串行是 81.8s。留足余量，只要求明显快于串行。
	budget := serialScaled * 6 / 10
	if elapsed >= budget {
		t.Fatalf("并行执行未见收益：实测 %v，应低于串行预算的 60%%（%v）", elapsed, budget)
	}
}

func scaleAll(durations []time.Duration, factor time.Duration) []time.Duration {
	scaled := make([]time.Duration, len(durations))
	for i, duration := range durations {
		scaled[i] = duration / factor
	}
	return scaled
}

func archivedFiberDurations(t *testing.T) []time.Duration {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatalf("读取黄金数据失败: %v", err)
	}
	var evidence struct {
		Run struct {
			APITurns []struct {
				Kind    string  `json:"kind"`
				Elapsed float64 `json:"elapsed_seconds"`
			} `json:"api_turns"`
		} `json:"run"`
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatalf("解析黄金数据失败: %v", err)
	}

	var durations []time.Duration
	for _, turn := range evidence.Run.APITurns {
		if turn.Kind == "formula_fiber" {
			durations = append(durations, time.Duration(turn.Elapsed*float64(time.Second)))
		}
	}
	return durations
}
