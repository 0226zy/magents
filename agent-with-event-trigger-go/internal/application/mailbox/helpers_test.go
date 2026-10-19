package mailbox

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// tempStore 是测试用的落盘实现，避免 application 的测试反向依赖 infrastructure。
type tempStore struct {
	root string
}

func newTempStore(t *testing.T) *tempStore {
	t.Helper()
	return &tempStore{root: t.TempDir()}
}

func (s *tempStore) WriteFile(relative string, data []byte) (port.ArtifactRef, error) {
	path := filepath.Join(s.root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return port.ArtifactRef{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return port.ArtifactRef{}, err
	}
	return port.ArtifactRef{Path: relative, Bytes: len(data), SHA256: mailbox.SHA256Hex(data)}, nil
}

func (s *tempStore) AppendLineDurable(relative string, line []byte) (port.ArtifactRef, error) {
	path := filepath.Join(s.root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return port.ArtifactRef{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return port.ArtifactRef{}, err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return port.ArtifactRef{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return port.ArtifactRef{}, err
	}
	return port.ArtifactRef{Path: relative, Bytes: len(data), SHA256: mailbox.SHA256Hex(data)}, nil
}

func readArtifact(t *testing.T, store port.ArtifactPort, relative string) string {
	t.Helper()
	disk, ok := store.(*tempStore)
	if !ok {
		t.Fatalf("不支持的测试存储类型 %T", store)
	}
	data, err := os.ReadFile(filepath.Join(disk.root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("读取产物 %s 失败: %v", relative, err)
	}
	return string(data)
}

func assertFileExists(t *testing.T, store port.ArtifactPort, relative string) {
	t.Helper()
	readArtifact(t, store, relative)
}

func assertFileLargerThan(t *testing.T, store port.ArtifactPort, relative string, size int) {
	t.Helper()
	if got := len(readArtifact(t, store, relative)); got <= size {
		t.Errorf("产物 %s 大小 = %d 字节，期望大于 %d", relative, got, size)
	}
}

// sortedByProviderTime 复用领域层的排序规则，模拟真实轮询后的入队顺序。
func sortedByProviderTime(emails []mailbox.ProviderObject) []mailbox.ProviderObject {
	out := make([]mailbox.ProviderObject, len(emails))
	copy(out, emails)
	sort.Slice(out, func(i, j int) bool { return mailbox.ProviderOrder(out[i], out[j]) })
	return out
}
