package adapter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// EvidenceStore 把实验产物与证据写到一个运行目录下，
// 同时实现 port.ArtifactPort 与 port.EvidencePort。
type EvidenceStore struct {
	root string
}

// NewEvidenceStore 创建运行目录。
func NewEvidenceStore(root string) (*EvidenceStore, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, err
	}
	return &EvidenceStore{root: absolute}, nil
}

// Root 返回运行目录绝对路径。
func (s *EvidenceStore) Root() string { return s.root }

// WriteFile 写入一份产物。
func (s *EvidenceStore) WriteFile(relative string, data []byte) (port.ArtifactRef, error) {
	path, err := s.resolve(relative)
	if err != nil {
		return port.ArtifactRef{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return port.ArtifactRef{}, err
	}
	return port.ArtifactRef{Path: relative, Bytes: len(data), SHA256: mailbox.SHA256Hex(data)}, nil
}

// AppendLineDurable 追加一行并 fsync 文件与父目录。
// 只 fsync 文件不够：新建文件的目录项本身也可能还停在页缓存里。
func (s *EvidenceStore) AppendLineDurable(relative string, line []byte) (port.ArtifactRef, error) {
	path, err := s.resolve(relative)
	if err != nil {
		return port.ArtifactRef{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return port.ArtifactRef{}, err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return port.ArtifactRef{}, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return port.ArtifactRef{}, err
	}
	if err := file.Close(); err != nil {
		return port.ArtifactRef{}, err
	}
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return port.ArtifactRef{}, err
	}
	return port.ArtifactRef{Path: relative, Bytes: len(data), SHA256: mailbox.SHA256Hex(data)}, nil
}

// Write 以规范化 JSON 写入证据，写入前做凭据形状扫描。
// 扫描是最后一道闸：脱敏漏了的话宁可让实验失败，也不能把密钥写进证据文件。
func (s *EvidenceStore) Write(relative string, value any) (port.ArtifactRef, error) {
	data := mailbox.CanonicalJSON(value)
	if mailbox.ContainsCredentialShape(string(data)) {
		return port.ArtifactRef{}, fmt.Errorf("%w: %s", derr.ErrCredentialLeak, relative)
	}
	return s.WriteFile(relative, data)
}

// Manifest 列出运行目录下的全部文件及其哈希，excludeNames 用于排除清单文件自身。
func (s *EvidenceStore) Manifest(excludeNames ...string) ([]port.ArtifactRef, error) {
	excluded := map[string]bool{}
	for _, name := range excludeNames {
		excluded[filepath.ToSlash(name)] = true
	}

	var refs []port.ArtifactRef
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if excluded[relative] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		refs = append(refs, port.ArtifactRef{
			Path: relative, Bytes: len(data), SHA256: mailbox.SHA256Hex(data),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs, nil
}

// resolve 把相对路径落到运行目录内，拒绝逃逸。
func (s *EvidenceStore) resolve(relative string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("非法的产物路径: %s", relative)
	}
	path := filepath.Join(s.root, cleaned)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, nil
}
