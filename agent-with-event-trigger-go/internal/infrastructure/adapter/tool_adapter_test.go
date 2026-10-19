package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadFileRoundTrip(t *testing.T) {
	tools := NewBuiltinTools()
	path := filepath.Join(t.TempDir(), "note.txt")

	if _, err := tools.Execute(context.Background(), "write_file", map[string]any{
		"file_path": path, "content": "第一行\n第二行\n",
	}); err != nil {
		t.Fatalf("write_file 失败: %v", err)
	}

	result, err := tools.Execute(context.Background(), "read_file", map[string]any{"file_path": path})
	if err != nil {
		t.Fatalf("read_file 失败: %v", err)
	}
	payload := result.(map[string]any)
	if content, _ := payload["content"].(string); !strings.Contains(content, "第二行") {
		t.Errorf("读回的内容 = %v", payload)
	}
}

// 二进制文件要被识别出来而不是塞一堆乱码进模型上下文。
func TestReadFileRejectsBinaryContent(t *testing.T) {
	tools := NewBuiltinTools()
	path := filepath.Join(t.TempDir(), "blob.bin")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := tools.Execute(context.Background(), "read_file", map[string]any{"file_path": path})
	if err != nil {
		return // 以错误形式拒绝同样可接受
	}
	payload := result.(map[string]any)
	if content, _ := payload["content"].(string); strings.ContainsRune(content, '\x00') {
		t.Error("二进制内容不应原样返回")
	}
}

// cd 要改变 Agent 自己的工作目录，否则后续命令还在老目录里执行。
func TestExecuteCommandInterceptsCd(t *testing.T) {
	tools := NewBuiltinTools()
	dir := t.TempDir()

	if _, err := tools.Execute(context.Background(), "execute_command",
		map[string]any{"command": "cd " + dir}); err != nil {
		t.Fatalf("cd 失败: %v", err)
	}

	// macOS 的 TempDir 在 /var 下，而 /var 是 /private/var 的软链，比较时取真实路径。
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(tools.CurrentDirectory())
	if got != want {
		t.Errorf("当前目录 = %q，期望 %q", got, want)
	}

	tools.Reset()
	if tools.CurrentDirectory() == got {
		t.Error("Reset 后应回到初始目录")
	}
}

func TestExecuteCommandCapturesOutput(t *testing.T) {
	tools := NewBuiltinTools()
	result, err := tools.Execute(context.Background(), "execute_command",
		map[string]any{"command": "echo hello-from-shell"})
	if err != nil {
		t.Fatalf("execute_command 失败: %v", err)
	}
	payload := result.(map[string]any)
	if output, _ := payload["output"].(string); !strings.Contains(output, "hello-from-shell") {
		t.Errorf("命令输出 = %v", payload)
	}
}

func TestCodeInterpreterRunsPython(t *testing.T) {
	tools := NewBuiltinTools()
	result, err := tools.Execute(context.Background(), "code_interpreter",
		map[string]any{"code": "print(6 * 7)"})
	if err != nil {
		t.Skipf("环境缺少可用的 python3，跳过: %v", err)
	}
	payload := result.(map[string]any)
	if stdout, _ := payload["stdout"].(string); !strings.Contains(stdout, "42") {
		t.Errorf("python 输出 = %v", payload)
	}
}

func TestUnknownToolIsRejected(t *testing.T) {
	tools := NewBuiltinTools()
	if tools.Has("teleport") {
		t.Fatal("不存在的工具不应被声明为可用")
	}
	if _, err := tools.Execute(context.Background(), "teleport", nil); err == nil {
		t.Fatal("执行未知工具应报错")
	}
}

func TestSystemStateMentionsGoVersion(t *testing.T) {
	state := SystemState("2026-08-01 09:00:00", "/tmp")
	if !strings.Contains(state, "Go Version:") {
		t.Errorf("系统状态应包含 Go 版本:\n%s", state)
	}
}
