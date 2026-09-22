package moonshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const goldenPath = "../../../../testdata/golden/latest.json"

// archivedRequest 取出归档运行里某一类调用的请求记录。
func archivedRequest(t *testing.T, kind string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatalf("读取黄金数据失败: %v", err)
	}
	var evidence struct {
		Run struct {
			APITurns []struct {
				Kind    string                     `json:"kind"`
				Request map[string]json.RawMessage `json:"request"`
			} `json:"api_turns"`
		} `json:"run"`
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatalf("解析黄金数据失败: %v", err)
	}
	for _, turn := range evidence.Run.APITurns {
		if turn.Kind == kind {
			return turn.Request
		}
	}
	t.Fatalf("归档里没有 %s 调用", kind)
	return nil
}

func keysOf(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func decodeObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return object
}

func assertSameKeys(t *testing.T, what string, archived, produced []string) {
	t.Helper()
	if strings.Join(archived, ",") != strings.Join(produced, ",") {
		t.Fatalf("%s 的字段与 Python 版不一致\nPython: %v\nGo:     %v", what, archived, produced)
	}
}

// TestChatRequestMatchesArchivedPythonRequest 是「协议等价」的直接证据：
// Go 发出的 chat 请求与 Python 版归档下来的请求逐字段一致。
func TestChatRequestMatchesArchivedPythonRequest(t *testing.T) {
	archived := archivedRequest(t, "chat_completion")

	var captured []byte
	client, _ := newChat(t, "kimi-k3", func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Write([]byte(chatBody))
	})
	declaration := json.RawMessage(`{"type":"function","function":{"name":"web_search","parameters":{}}}`)
	history := []Message{
		{Role: "system", Content: "你是 Kimi，一个智能搜索助手。"},
		{Role: "user", Content: "东盟现在有几个成员国？"},
	}
	if _, err := client.Chat(context.Background(), history, []json.RawMessage{declaration}); err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}

	produced := decodeObject(t, captured)
	assertSameKeys(t, "chat 请求", keysOf(archived), keysOf(produced))

	for _, field := range []string{"model", "temperature", "max_tokens", "reasoning_effort"} {
		if string(archived[field]) != string(produced[field]) {
			t.Fatalf("chat 请求的 %s 不一致：Python %s / Go %s",
				field, archived[field], produced[field])
		}
	}

	// 消息的字段集合同样要一致：多一个 reasoning_content 就会让 Moonshot 返回 400。
	archivedMessages := decodeMessages(t, archived["messages"])
	producedMessages := decodeMessages(t, produced["messages"])
	if len(producedMessages) != len(archivedMessages) {
		t.Fatalf("消息条数不一致: Python %d / Go %d", len(archivedMessages), len(producedMessages))
	}
	for i := range archivedMessages {
		assertSameKeys(t, fmt.Sprintf("第 %d 条消息", i+1),
			keysOf(archivedMessages[i]), keysOf(producedMessages[i]))
	}
}

func decodeMessages(t *testing.T, raw json.RawMessage) []map[string]json.RawMessage {
	t.Helper()
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		t.Fatalf("解析 messages 失败: %v", err)
	}
	return messages
}

func TestFormulaRequestsMatchArchivedPythonRequests(t *testing.T) {
	client, recorder := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tools") {
			w.Write([]byte(declarationBody))
			return
		}
		w.Write([]byte(`{"id":"f","status":"succeeded","context":{"encrypted_output":"cipher"}}`))
	})

	client.Tools(context.Background())
	client.ExecuteFiber(context.Background(), "web_search", `{"query":"东盟成员国"}`)

	turns := recorder.Turns()
	toolsRequest := decodeObject(t, turns[0].Request)
	assertSameKeys(t, "声明拉取请求", keysOf(archivedRequest(t, "formula_tools")), keysOf(toolsRequest))

	archivedFiber := archivedRequest(t, "formula_fiber")
	fiberRequest := decodeObject(t, turns[1].Request)
	assertSameKeys(t, "fiber 请求", keysOf(archivedFiber), keysOf(fiberRequest))

	assertSameKeys(t, "fiber 请求体",
		keysOf(decodeObject(t, archivedFiber["body"])),
		keysOf(decodeObject(t, fiberRequest["body"])))

	// arguments 在两边都必须是 JSON 字符串，而不是被解析后的对象。
	for label, body := range map[string]json.RawMessage{
		"Python": archivedFiber["body"],
		"Go":     fiberRequest["body"],
	} {
		var decoded struct {
			Arguments json.RawMessage `json:"arguments"`
		}
		json.Unmarshal(body, &decoded)
		if len(decoded.Arguments) == 0 || decoded.Arguments[0] != '"' {
			t.Fatalf("%s 侧的 arguments 不是 JSON 字符串: %s", label, decoded.Arguments)
		}
	}
}
