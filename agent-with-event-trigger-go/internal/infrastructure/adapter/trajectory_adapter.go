package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// TrajectoryFile 把状态快照写进单个 JSON 文件，实现 port.TrajectoryPort。
type TrajectoryFile struct {
	mu   sync.Mutex
	path string
}

// NewTrajectoryFile 创建轨迹写入器，必要时建好父目录。
func NewTrajectoryFile(path string) (*TrajectoryFile, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建轨迹目录 %s: %w", dir, err)
		}
	}
	return &TrajectoryFile{path: path}, nil
}

// Path 返回轨迹文件路径。
func (t *TrajectoryFile) Path() string { return t.path }

// Save 原子覆盖写入快照。
//
// 每轮循环都会调用一次，直接覆盖目标文件的话，写一半被中断就会留下半截 JSON，
// 连上一轮的完整轨迹也丢了。先写临时文件再 rename，替换是原子的。
func (t *TrajectoryFile) Save(snapshot entity.TrajectorySnapshot) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("序列化轨迹失败: %w", err)
	}

	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, buffer.Bytes(), 0o644); err != nil {
		return fmt.Errorf("写入轨迹临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, t.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("替换轨迹文件失败: %w", err)
	}
	return nil
}

// NopTrajectory 不写任何文件，用于 save_trajectory=false。
type NopTrajectory struct{}

func (NopTrajectory) Save(entity.TrajectorySnapshot) error { return nil }
func (NopTrajectory) Path() string                         { return "" }
