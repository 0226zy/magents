package adapter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

func TestEvidenceStoreWritesAndHashes(t *testing.T) {
	store, err := NewEvidenceStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	ref, err := store.Write("summary.json", map[string]any{"status": "passed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.SHA256) != 64 || ref.Bytes == 0 {
		t.Errorf("产物引用 = %+v", ref)
	}

	data, err := os.ReadFile(filepath.Join(store.Root(), "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status":"passed"`) {
		t.Errorf("落盘内容 = %s", data)
	}
}

// 凭据形状是最后一道闸：脱敏漏了宁可让实验失败，也不能把密钥写进证据。
func TestEvidenceStoreRejectsCredentialShapes(t *testing.T) {
	store, err := NewEvidenceStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Write("leak.json", map[string]any{
		"access_token": "sk-abcdefghijklmnopqrstuvwxyz0123",
	})
	if !errors.Is(err, derr.ErrCredentialLeak) {
		t.Fatalf("错误 = %v，期望 ErrCredentialLeak", err)
	}
	if _, statErr := os.Stat(filepath.Join(store.Root(), "leak.json")); statErr == nil {
		t.Error("被拒绝的内容不应落盘")
	}
}

func TestEvidenceStoreRejectsPathEscape(t *testing.T) {
	store, err := NewEvidenceStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFile("../outside.txt", []byte("x")); err == nil {
		t.Fatal("逃逸出运行目录的路径应被拒绝")
	}
}

func TestAppendLineDurableAccumulatesLines(t *testing.T) {
	store, err := NewEvidenceStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.AppendLineDurable("artifacts/notes.jsonl", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	ref, err := store.AppendLineDurable("artifacts/notes.jsonl", []byte(`{"n":2}`))
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(store.Root(), "artifacts", "notes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2 {
		t.Errorf("行数 = %d，期望 2", lines)
	}
	if ref.Bytes != len(data) {
		t.Errorf("引用字节数 = %d，文件实际 %d", ref.Bytes, len(data))
	}
}

func TestManifestExcludesNamedFiles(t *testing.T) {
	store, err := NewEvidenceStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFile("a.txt", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFile("manifest.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}

	files, err := store.Manifest("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" {
		t.Errorf("清单 = %+v，应排除 manifest.json 自身", files)
	}
}

// 轨迹每轮都写，写一半崩溃不能留下半截 JSON。
func TestTrajectoryWriteIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trajectory.json")
	trajectory, err := NewTrajectoryFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := trajectory.Save(entity.TrajectorySnapshot{Iteration: 1}); err != nil {
		t.Fatal(err)
	}
	if err := trajectory.Save(entity.TrajectorySnapshot{Iteration: 2}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"iteration": 2`) &&
		!strings.Contains(string(data), `"iteration":2`) {
		t.Errorf("轨迹内容 = %s", data)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("临时文件应在重命名后消失")
	}
}
