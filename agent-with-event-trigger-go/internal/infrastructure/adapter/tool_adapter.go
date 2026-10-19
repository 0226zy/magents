package adapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// commandTimeout 是 shell 与代码执行的硬超时，与 Python 版一致。
const commandTimeout = 30 * time.Second

// BuiltinTools 提供 4 个内置工具，并持有当前工作目录。
// 同时实现 port.ToolPort 与 port.WorkspacePort——目录状态天然属于这里，
// 因为只有 execute_command 的 cd 会改变它。
type BuiltinTools struct {
	mu        sync.RWMutex
	cwd       string
	initialWD string
	// pythonBin 是 code_interpreter 使用的解释器，可为空（此时该工具不声明）。
	pythonBin string
}

// NewBuiltinTools 以当前工作目录为起点构造工具集。
func NewBuiltinTools() *BuiltinTools {
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	return &BuiltinTools{cwd: wd, initialWD: wd, pythonBin: detectPython()}
}

// detectPython 查找可用的 Python 解释器。
// Go 没有进程内 Python，code_interpreter 只能交给子进程；找不到解释器时
// 不声明这个工具，而不是声明了却必定失败。
func detectPython() string {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// CurrentDirectory 返回当前工作目录。
func (t *BuiltinTools) CurrentDirectory() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cwd
}

// Reset 把工作目录恢复到进程启动时的位置。
func (t *BuiltinTools) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cwd = t.initialWD
}

// Has 报告是否提供该工具。
func (t *BuiltinTools) Has(name string) bool {
	switch name {
	case "read_file", "write_file", "execute_command":
		return true
	case "code_interpreter":
		return t.pythonBin != ""
	default:
		return false
	}
}

// Definitions 返回提供给模型的工具声明。
func (t *BuiltinTools) Definitions() []entity.ToolDefinition {
	defs := []entity.ToolDefinition{
		{
			Type: "function",
			Function: entity.FunctionSpec{
				Name: "read_file",
				Description: "Read the contents of a text file. Returns error for binary files. " +
					"Supports partial reading for large files.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{
							"type":        "string",
							"description": "Path to the file to read (absolute or relative to current directory)",
						},
						"begin_line": map[string]any{
							"type":        "integer",
							"description": "Optional: Line number to start reading from (1-based indexing)",
						},
						"number_lines": map[string]any{
							"type":        "integer",
							"description": "Optional: Number of lines to read from begin_line",
						},
					},
					"required": []string{"file_path"},
				},
			},
		},
		{
			Type: "function",
			Function: entity.FunctionSpec{
				Name:        "write_file",
				Description: "Write content to a file (creates or overwrites)",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{"type": "string", "description": "Path to the file to write"},
						"content":   map[string]any{"type": "string", "description": "Content to write to the file"},
					},
					"required": []string{"file_path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: entity.FunctionSpec{
				Name:        "execute_command",
				Description: "Execute a shell command in the current directory",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command":     map[string]any{"type": "string", "description": "Shell command to execute"},
						"working_dir": map[string]any{"type": "string", "description": "Optional working directory for the command"},
					},
					"required": []string{"command"},
				},
			},
		},
	}

	if t.pythonBin != "" {
		defs = append(defs, entity.ToolDefinition{
			Type: "function",
			Function: entity.FunctionSpec{
				Name:        "code_interpreter",
				Description: "Execute Python code in a subprocess",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"code": map[string]any{"type": "string", "description": "Python code to execute"},
					},
					"required": []string{"code"},
				},
			},
		})
	}
	return defs
}

// Execute 按名字分发到具体工具。
func (t *BuiltinTools) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "read_file":
		return t.readFile(args)
	case "write_file":
		return t.writeFile(args)
	case "execute_command":
		return t.executeCommand(ctx, args)
	case "code_interpreter":
		return t.codeInterpreter(ctx, args)
	default:
		return nil, fmt.Errorf("%w: %s", derr.ErrUnknownTool, name)
	}
}

// resolvePath 把相对路径解析到当前工作目录下。
func (t *BuiltinTools) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(t.CurrentDirectory(), path)
}

func (t *BuiltinTools) readFile(args map[string]any) (any, error) {
	rawPath, err := requireString(args, "file_path")
	if err != nil {
		return nil, err
	}
	path := t.resolvePath(rawPath)

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8Valid(data) {
		return map[string]any{
			"success":   false,
			"error":     "Cannot read binary file. This tool only supports text files.",
			"file_path": path,
			"is_binary": true,
		}, nil
	}

	content := string(data)
	beginLine, hasBegin := optionalInt(args, "begin_line")
	numberLines, hasNumber := optionalInt(args, "number_lines")
	if !hasBegin && !hasNumber {
		return map[string]any{
			"success":      true,
			"file_path":    path,
			"content":      content,
			"size_bytes":   info.Size(),
			"lines":        len(splitLinesKeepEnds(content)),
			"partial_read": false,
		}, nil
	}

	all := splitLinesKeepEnds(content)
	total := len(all)
	start := 0
	if hasBegin {
		start = beginLine - 1
	}
	if start < 0 {
		start = 0
	}
	if start >= total {
		return map[string]any{
			"success":     false,
			"error":       fmt.Sprintf("begin_line %d is beyond file length (%d lines)", beginLine, total),
			"file_path":   path,
			"total_lines": total,
		}, nil
	}
	end := total
	if hasNumber {
		end = min(start+numberLines, total)
	}

	return map[string]any{
		"success":      true,
		"file_path":    path,
		"content":      strings.Join(all[start:end], ""),
		"size_bytes":   info.Size(),
		"total_lines":  total,
		"begin_line":   start + 1,
		"end_line":     end,
		"lines_read":   end - start,
		"partial_read": true,
	}, nil
}

func (t *BuiltinTools) writeFile(args map[string]any) (any, error) {
	rawPath, err := requireString(args, "file_path")
	if err != nil {
		return nil, err
	}
	content, err := requireString(args, "content")
	if err != nil {
		return nil, err
	}
	path := t.resolvePath(rawPath)

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{
		"success":       true,
		"file_path":     path,
		"bytes_written": len(content),
		"lines_written": len(splitLinesKeepEnds(content)),
	}, nil
}

func (t *BuiltinTools) executeCommand(ctx context.Context, args map[string]any) (any, error) {
	command, err := requireString(args, "command")
	if err != nil {
		return nil, err
	}

	// cd 要改变 Agent 自身的工作目录，所以不能交给子 shell——子 shell 退出后
	// 目录变更就丢了。这里拦下来直接改状态。
	if target, ok := strings.CutPrefix(strings.TrimSpace(command), "cd "); ok {
		newDir := t.resolvePath(strings.TrimSpace(target))
		info, statErr := os.Stat(newDir)
		if statErr != nil || !info.IsDir() {
			return nil, fmt.Errorf("directory not found: %s", newDir)
		}
		absolute, absErr := filepath.Abs(newDir)
		if absErr != nil {
			absolute = newDir
		}
		t.mu.Lock()
		t.cwd = absolute
		t.mu.Unlock()
		return map[string]any{
			"success":     true,
			"command":     command,
			"output":      "Changed directory to: " + absolute,
			"return_code": 0,
		}, nil
	}

	workingDir := t.CurrentDirectory()
	if raw, ok := optionalString(args, "working_dir"); ok && raw != "" {
		workingDir = t.resolvePath(raw)
	}

	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = workingDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("command timed out after %s: %s", commandTimeout, command)
	}

	result := map[string]any{
		"success":     runErr == nil,
		"command":     command,
		"output":      stdout.String(),
		"return_code": cmd.ProcessState.ExitCode(),
		"working_dir": workingDir,
	}
	if stderr.Len() > 0 {
		result["error"] = stderr.String()
	}
	return result, nil
}

func (t *BuiltinTools) codeInterpreter(ctx context.Context, args map[string]any) (any, error) {
	code, err := requireString(args, "code")
	if err != nil {
		return nil, err
	}
	if t.pythonBin == "" {
		return nil, fmt.Errorf("%w: 未找到 python3 解释器", derr.ErrUnknownTool)
	}

	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, t.pythonBin, "-c", code)
	cmd.Dir = t.CurrentDirectory()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("code execution timed out after %s", commandTimeout)
	}
	return map[string]any{
		"success": runErr == nil,
		"stdout":  stdout.String(),
		"stderr":  stderr.String(),
	}, nil
}

// SystemState 渲染 system hint 里的系统状态段。
func SystemState(now string, cwd string) string {
	shell := fmt.Sprintf("Linux Shell (%s)", envOr("SHELL", "bash"))
	switch runtime.GOOS {
	case "windows":
		shell = "Windows Command Prompt or PowerShell"
	case "darwin":
		shell = "macOS Terminal (zsh/bash)"
	}
	return strings.Join([]string{
		"Current Time: " + now,
		"Current Directory: " + cwd,
		fmt.Sprintf("System: %s (%s)", runtime.GOOS, runtime.GOARCH),
		"Shell Environment: " + shell,
		"Go Version: " + runtime.Version(),
	}, "\n")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
