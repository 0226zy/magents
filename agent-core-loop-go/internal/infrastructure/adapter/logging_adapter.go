package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Trajectory 把 Agent 核心循环的每一步同时输出到终端（stdout）和一个日志文件，
// 实现 domain/port.TrajectoryPort。
type Trajectory struct {
	mu  sync.Mutex
	out io.Writer // MultiWriter(stdout, file)
	f   *os.File
}

// NewTrajectory 创建轨迹日志。logDir 下的文件名带时间戳，例如 logs/agent-20260922-161530.log。
func NewTrajectory(logDir string) (*Trajectory, error) {
	f, _, err := createLogFile(logDir, time.Now().Format("20060102-150405"), "")
	if err != nil {
		return nil, err
	}
	return &Trajectory{out: io.MultiWriter(os.Stdout, f), f: f}, nil
}

// createLogFile 在 logDir 下创建 agent-<stamp><suffix>.log，返回文件与其路径。
func createLogFile(logDir, stamp, suffix string) (*os.File, string, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("创建日志目录 %s: %w", logDir, err)
	}
	path := filepath.Join(logDir, fmt.Sprintf("agent-%s%s.log", stamp, suffix))
	f, err := os.Create(path)
	if err != nil {
		return nil, "", fmt.Errorf("创建日志文件 %s: %w", path, err)
	}
	return f, path, nil
}

// NewTrajectoryPair 同时创建轨迹日志与 raw 日志，两者共用同一时间戳，便于按名字配对。
func NewTrajectoryPair(logDir string) (*Trajectory, *RawFile, error) {
	stamp := time.Now().Format("20060102-150405")
	rawF, _, err := createLogFile(logDir, stamp, "-raw")
	if err != nil {
		return nil, nil, err
	}
	trajF, _, err := createLogFile(logDir, stamp, "")
	if err != nil {
		rawF.Close()
		return nil, nil, err
	}
	return &Trajectory{out: io.MultiWriter(os.Stdout, trajF), f: trajF}, &RawFile{f: rawF}, nil
}

// Path 返回日志文件路径（供启动横幅提示）。
func (t *Trajectory) Path() string { return t.f.Name() }

// Event 记录一条带时间戳的轨迹事件。
func (t *Trajectory) Event(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writeLocked(fmt.Sprintf("%s %s\n", timestamp(), fmt.Sprintf(format, args...)))
}

// EventJSON 记录一个结构化对象：标题 + 压缩为单行的 JSON 字符串（便于逐行检索日志）。
func (t *Trajectory) EventJSON(label string, value any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	payload, err := json.Marshal(value)
	if err != nil {
		t.writeLocked(fmt.Sprintf("%s %s: <序列化失败: %v>\n", timestamp(), label, err))
		return
	}
	t.writeLocked(fmt.Sprintf("%s %s: %s\n", timestamp(), label, payload))
}

func timestamp() string {
	return "[" + time.Now().Format("15:04:05.000") + "]"
}

func (t *Trajectory) writeLocked(s string) {
	io.WriteString(t.out, s)
}

// Close 关闭日志文件；终端侧无需处理。
func (t *Trajectory) Close() error { return t.f.Close() }

// RawFile 把模型原始响应只写进单独文件（不上终端），实现 domain/port.RawLogPort。
type RawFile struct {
	mu sync.Mutex
	f  *os.File
}

// NewRawFile 创建 raw 日志文件（与轨迹日志同一时间戳）：logs/agent-20260922-161530-raw.log。
func NewRawFile(logDir string) (*RawFile, error) {
	f, _, err := createLogFile(logDir, time.Now().Format("20060102-150405"), "-raw")
	if err != nil {
		return nil, err
	}
	return &RawFile{f: f}, nil
}

// Path 返回 raw 日志文件路径。
func (r *RawFile) Path() string { return r.f.Name() }

// LogResponse 把第 iteration 轮模型返回的原始报文按单行 JSON 写入文件。
func (r *RawFile) LogResponse(iteration int, rawJSON []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.f, "%s 第 %d 轮模型原始响应: %s\n", timestamp(), iteration, rawJSON)
}

// Close 关闭 raw 日志文件。
func (r *RawFile) Close() error { return r.f.Close() }
