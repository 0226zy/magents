package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRawFileLogResponse(t *testing.T) {
	dir := t.TempDir()
	raw, err := NewRawFile(dir)
	if err != nil {
		t.Fatalf("NewRawFile 失败: %v", err)
	}

	raw.LogResponse(1, []byte(`{"id":"chat-1","choices":[]}`))
	raw.LogResponse(2, []byte(`{"id":"chat-2","choices":[{"finish_reason":"stop"}]}`))
	if err := raw.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	content, err := os.ReadFile(raw.Path())
	if err != nil {
		t.Fatalf("读取 raw 文件失败: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "第 1 轮模型原始响应: {\"id\":\"chat-1\"") {
		t.Errorf("缺少第 1 轮记录:\n%s", text)
	}
	if !strings.Contains(text, "第 2 轮模型原始响应: {\"id\":\"chat-2\"") {
		t.Errorf("缺少第 2 轮记录:\n%s", text)
	}
}

func TestNewTrajectoryPairSharesTimestamp(t *testing.T) {
	dir := t.TempDir()
	traj, raw, err := NewTrajectoryPair(dir)
	if err != nil {
		t.Fatalf("NewTrajectoryPair 失败: %v", err)
	}
	defer traj.Close()
	defer raw.Close()

	base := func(p string) string { return filepath.Base(p) }
	if base(traj.Path()) != strings.Replace(base(raw.Path()), "-raw.log", ".log", 1) {
		t.Errorf("两个日志文件名时间戳不一致: %s vs %s", traj.Path(), raw.Path())
	}
}
