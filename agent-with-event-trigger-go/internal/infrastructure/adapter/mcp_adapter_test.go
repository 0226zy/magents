package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeMCPScript 是一个最小 MCP 服务器：读 stdin 的 JSON-RPC 行，按协议回应。
// 用真实子进程而不是打桩，这样握手顺序、换行分帧、通知不回包都被真正验证到。
const fakeMCPScript = `#!/usr/bin/env python3
import json, sys

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    message = json.loads(line)
    method = message.get("method")
    if method == "notifications/initialized":
        continue
    if method == "initialize":
        result = {"protocolVersion": "2024-11-05", "serverInfo": {"name": "fake"}}
    elif method == "tools/list":
        result = {"tools": [{
            "name": "ping",
            "description": "回显 pong",
            "inputSchema": {"type": "object", "properties": {}},
        }]}
    elif method == "tools/call":
        result = {"content": [{"type": "text", "text": "pong"}]}
    else:
        result = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": message["id"], "result": result}) + "\n")
    sys.stdout.flush()
`

func writeFakeServer(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake_mcp.py")
	if err := os.WriteFile(path, []byte(fakeMCPScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newManager(t *testing.T, specs []MCPServerSpec) *MCPManager {
	t.Helper()
	manager := NewMCPManager(specs, NopLogger{})
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func TestMCPLoadAndCallTool(t *testing.T) {
	python := detectPython()
	if python == "" {
		t.Skip("环境缺少 python3，跳过 MCP 子进程测试")
	}

	manager := newManager(t, []MCPServerSpec{
		{Name: "demo", Command: python, Args: []string{writeFakeServer(t)}},
	})

	report, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(report.Servers) != 1 || !report.Servers[0].Loaded {
		t.Fatalf("加载报告 = %+v", report)
	}
	if report.ToolsCount != 1 {
		t.Errorf("工具数 = %d，期望 1", report.ToolsCount)
	}

	// 工具名要带 server 前缀：OpenAI 兼容接口不接受带点号的名字，
	// 不加前缀则两个 server 的同名工具会互相覆盖。
	if !manager.Has("demo_ping") {
		t.Fatalf("工具清单 = %v，期望含 demo_ping", manager.ToolNames())
	}

	result, err := manager.Execute(context.Background(), "demo_ping", map[string]any{})
	if err != nil {
		t.Fatalf("调用工具失败: %v", err)
	}
	if encoded := toJSON(t, result); !containsText(encoded, "pong") {
		t.Errorf("工具返回 = %s", encoded)
	}
}

// 某个 server 起不来时要逐个回报原因，不能像 Python 那样三个全部静默失败。
func TestMCPLoadReportsPerServerFailure(t *testing.T) {
	python := detectPython()
	if python == "" {
		t.Skip("环境缺少 python3，跳过 MCP 子进程测试")
	}

	manager := newManager(t, []MCPServerSpec{
		{Name: "good", Command: python, Args: []string{writeFakeServer(t)}},
		{Name: "missing", Command: "/definitely/not/here", Args: nil},
	})

	report, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("单个 server 失败不应让整体报错: %v", err)
	}

	byName := map[string]bool{}
	reasons := map[string]string{}
	for _, server := range report.Servers {
		byName[server.Name] = server.Loaded
		reasons[server.Name] = server.Error
	}
	if !byName["good"] {
		t.Error("正常 server 应加载成功")
	}
	if byName["missing"] {
		t.Error("不存在的命令不应报告为加载成功")
	}
	if reasons["missing"] == "" {
		t.Error("失败的 server 必须带上原因")
	}
}

func TestMCPToolsByServerGroupsNames(t *testing.T) {
	python := detectPython()
	if python == "" {
		t.Skip("环境缺少 python3，跳过 MCP 子进程测试")
	}

	manager := newManager(t, []MCPServerSpec{
		{Name: "demo", Command: python, Args: []string{writeFakeServer(t)}},
	})
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	grouped := manager.ToolsByServer()
	if len(grouped["demo"]) != 1 {
		t.Errorf("分组结果 = %v", grouped)
	}
}
