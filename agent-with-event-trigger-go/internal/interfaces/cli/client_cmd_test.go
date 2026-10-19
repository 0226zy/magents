package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAgentServer 实现事件服务的最小子集，记录收到的事件类型。
type fakeAgentServer struct {
	mu    sync.Mutex
	paths []string
	types []string
}

func (f *fakeAgentServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONBody(w, map[string]any{"status": "healthy"})
	})
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		f.mu.Lock()
		f.paths = append(f.paths, "/event")
		eventType, _ := body["event_type"].(string)
		f.types = append(f.types, eventType)
		f.mu.Unlock()

		writeJSONBody(w, map[string]any{
			"event_id": "evt_1", "final_answer": "已处理", "iterations": 1,
			"tool_calls_count": 0, "tool_calls_total": 0, "todo_items": 0, "success": true,
		})
	})
	mux.HandleFunc("/agent/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONBody(w, map[string]any{"provider": "fake", "model": "fake-model"})
	})
	mux.HandleFunc("/process/register", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONBody(w, map[string]any{"success": true})
	})
	mux.HandleFunc("/monitoring/start", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONBody(w, map[string]any{"success": true, "running": true})
	})
	return mux
}

func (f *fakeAgentServer) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.types))
	copy(out, f.types)
	return out
}

func writeJSONBody(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func TestParseClientOptionsRejectsUnknownEventType(t *testing.T) {
	if _, err := ParseClientOptions([]string{"-event-type", "telepathy"}); err == nil {
		t.Fatal("未知事件类型应在解析阶段就被拒绝")
	}
	if _, err := ParseClientOptions([]string{"-mode", "magic"}); err == nil {
		t.Fatal("未知模式应被拒绝")
	}
}

func TestRunClientSendsSingleMessage(t *testing.T) {
	fake := &fakeAgentServer{}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	var out bytes.Buffer
	opts, err := ParseClientOptions([]string{
		"-server", server.URL, "-message", "创建脚本", "-event-type", "timer_trigger",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := RunClient(context.Background(), opts, &out); err != nil {
		t.Fatalf("RunClient 返回错误: %v", err)
	}

	if types := fake.eventTypes(); len(types) != 1 || types[0] != "timer_trigger" {
		t.Errorf("服务端收到的事件类型 = %v", types)
	}
	if !strings.Contains(out.String(), "已处理") {
		t.Errorf("输出未展示最终回答:\n%s", out.String())
	}
}

func TestRunClientTestModeCoversAllSources(t *testing.T) {
	fake := &fakeAgentServer{}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	var out bytes.Buffer
	opts, err := ParseClientOptions([]string{"-server", server.URL, "-mode", "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RunClient(context.Background(), opts, &out); err != nil {
		t.Fatalf("RunClient 返回错误: %v", err)
	}

	types := fake.eventTypes()
	if len(types) != 6 {
		t.Fatalf("事件数 = %d，期望覆盖 6 种来源", len(types))
	}
	for _, want := range []string{"web_message", "im_message", "email_reply",
		"github_pr_update", "timer_trigger", "system_alert"} {
		if !slicesContain(types, want) {
			t.Errorf("测试场景缺少事件类型 %s", want)
		}
	}
}

// 连不上服务器时要给出明确提示，而不是抛一串栈。
func TestRunClientReportsUnreachableServer(t *testing.T) {
	var out bytes.Buffer
	opts, err := ParseClientOptions([]string{"-server", "http://127.0.0.1:1", "-message", "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RunClient(context.Background(), opts, &out); err == nil {
		t.Fatal("连接失败时应返回错误")
	}
	if !strings.Contains(out.String(), "无法连接服务器") {
		t.Errorf("输出缺少连接失败提示:\n%s", out.String())
	}
}

func TestSplitTypedInput(t *testing.T) {
	cases := []struct{ line, wantType, wantContent string }{
		{"timer_trigger: 检查备份", "timer_trigger", "检查备份"},
		{"随便说点什么", "web_message", "随便说点什么"},
		{"注意: 这不是事件类型", "web_message", "注意: 这不是事件类型"},
	}
	for _, testCase := range cases {
		gotType, gotContent := splitTypedInput(testCase.line)
		if gotType != testCase.wantType || gotContent != testCase.wantContent {
			t.Errorf("splitTypedInput(%q) = (%q, %q)，期望 (%q, %q)",
				testCase.line, gotType, gotContent, testCase.wantType, testCase.wantContent)
		}
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
