package moonshot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const declarationBody = `{
  "object": "list",
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "web_search",
        "description": "Search the web",
        "parameters": {"type": "object", "properties": {"query": {"type": "string"}}},
        "provider_only_field": {"kept": true}
      }
    }
  ]
}`

func newFormula(t *testing.T, handler http.HandlerFunc) (*FormulaClient, *Recorder) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	recorder := &Recorder{}
	return NewFormulaClient("secret-key", server.URL, DefaultFormulaURI, recorder, server.Client()), recorder
}

func TestToolsForwardsDeclarationVerbatim(t *testing.T) {
	var gotPath string
	client, recorder := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(declarationBody))
	})

	tools, err := client.Tools(context.Background())
	if err != nil {
		t.Fatalf("Tools 失败: %v", err)
	}
	if gotPath != "/formulas/moonshot/web-search:latest/tools" {
		t.Fatalf("端点拼接不对: %s", gotPath)
	}
	if len(tools) != 1 {
		t.Fatalf("期望 1 条声明，实际 %d", len(tools))
	}
	// provider schema 里未被 Go 类型声明的字段必须原样保留。
	if !strings.Contains(string(tools[0]), "provider_only_field") {
		t.Fatalf("声明未原样透传: %s", tools[0])
	}

	turns := recorder.Turns()
	if len(turns) != 1 || turns[0].Kind != "formula_tools" {
		t.Fatalf("凭据不对: %+v", turns)
	}
	if turns[0].HTTPStatus == nil || *turns[0].HTTPStatus != 200 {
		t.Fatalf("http_status 不对: %+v", turns[0].HTTPStatus)
	}
	if turns[0].FormulaURI != DefaultFormulaURI {
		t.Fatalf("formula_uri 不对: %s", turns[0].FormulaURI)
	}
}

func TestToolsRejectsDeclarationWithoutWebSearch(t *testing.T) {
	client, recorder := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tools":[{"type":"function","function":{"name":"other","parameters":{}}}]}`))
	})

	if _, err := client.Tools(context.Background()); err == nil {
		t.Fatal("声明缺少 web_search 时必须报错")
	} else if !strings.Contains(err.Error(), "does not contain function web_search") {
		t.Fatalf("错误文案不对: %v", err)
	}
	if turns := recorder.Turns(); len(turns) != 1 || turns[0].Error == nil {
		t.Fatalf("失败也必须留下凭据: %+v", turns)
	}
}

func TestToolsRejectsEmptyToolList(t *testing.T) {
	client, _ := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tools":[]}`))
	})

	if _, err := client.Tools(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "has no tools") {
		t.Fatalf("空声明必须报错，实际: %v", err)
	}
}

func TestExecuteFiberSendsRawArgumentsVerbatim(t *testing.T) {
	// 尾随空格与非紧凑写法：重新序列化会把它抹平，原文转发不会。
	raw := `{"query" : "Moonshot K3"}`
	var gotBody []byte
	client, recorder := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"id":"fiber-real","status":"succeeded","context":{"encrypted_output":"cipher"}}`))
	})

	output, err := client.ExecuteFiber(context.Background(), "web_search", raw)
	if err != nil {
		t.Fatalf("ExecuteFiber 失败: %v", err)
	}
	if output != "cipher" {
		t.Fatalf("输出不对: %q", output)
	}

	var body FiberRequest
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v", err)
	}
	if body.Name != "web_search" {
		t.Fatalf("name 不对: %q", body.Name)
	}
	if body.Arguments != raw {
		t.Fatalf("arguments 必须是模型原文\n期望: %q\n实际: %q", raw, body.Arguments)
	}
	// 请求体恰好两个键。
	var shape map[string]json.RawMessage
	json.Unmarshal(gotBody, &shape)
	if len(shape) != 2 {
		t.Fatalf("请求体应只含 name 与 arguments: %v", shape)
	}

	turns := recorder.Turns()
	if len(turns) != 1 || turns[0].Kind != "formula_fiber" {
		t.Fatalf("凭据不对: %+v", turns)
	}
	var response struct {
		ID string `json:"id"`
	}
	json.Unmarshal(turns[0].Response, &response)
	if response.ID != "fiber-real" {
		t.Fatalf("凭据里的 fiber id 不对: %q", response.ID)
	}
}

func TestExecuteFiberPrefersPlainOutput(t *testing.T) {
	client, _ := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"f","status":"succeeded","context":{"output":"plain","encrypted_output":"cipher"}}`))
	})

	output, err := client.ExecuteFiber(context.Background(), "web_search", `{}`)
	if err != nil || output != "plain" {
		t.Fatalf("应优先使用 context.output，实际 %q / %v", output, err)
	}
}

func TestExecuteFiberFallsBackToEncryptedOutput(t *testing.T) {
	client, _ := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"f","status":"succeeded","context":{"output":"","encrypted_output":"cipher"}}`))
	})

	output, err := client.ExecuteFiber(context.Background(), "web_search", `{}`)
	if err != nil || output != "cipher" {
		t.Fatalf("output 为空时应退回 encrypted_output，实际 %q / %v", output, err)
	}
}

func TestExecuteFiberRejectsEmptyOutput(t *testing.T) {
	client, _ := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"f","status":"succeeded","context":{}}`))
	})

	if _, err := client.ExecuteFiber(context.Background(), "web_search", `{}`); err == nil ||
		!strings.Contains(err.Error(), "returned no output") {
		t.Fatalf("两者皆空必须报错，实际: %v", err)
	}
}

func TestExecuteFiberRejectsUnsucceededStatus(t *testing.T) {
	client, _ := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"f","status":"failed","context":{"encrypted_output":"cipher"}}`))
	})

	if _, err := client.ExecuteFiber(context.Background(), "web_search", `{}`); err == nil ||
		!strings.Contains(err.Error(), "did not succeed") {
		t.Fatalf("status 非 succeeded 必须报错，实际: %v", err)
	}
}

func TestFiberErrorKeepsFourXXBody(t *testing.T) {
	client, recorder := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad formula"}}`))
	})

	if _, err := client.ExecuteFiber(context.Background(), "web_search", `{}`); err == nil {
		t.Fatal("4xx 必须报错")
	}
	turns := recorder.Turns()
	if len(turns) != 1 || turns[0].Error == nil {
		t.Fatalf("失败凭据缺失: %+v", turns)
	}
	if turns[0].HTTPStatus == nil || *turns[0].HTTPStatus != 400 {
		t.Fatalf("http_status 应为 400: %+v", turns[0].HTTPStatus)
	}
	// 先解码 body 再判状态码，因此 4xx 的响应体仍然进了证据。
	if !strings.Contains(string(turns[0].Error.Response), "bad formula") {
		t.Fatalf("4xx body 未进入证据: %s", turns[0].Error.Response)
	}
}

func TestFormulaTurnsNeverRecordAuthorization(t *testing.T) {
	client, recorder := newFormula(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-key" {
			t.Errorf("请求未携带鉴权头")
		}
		if strings.HasSuffix(r.URL.Path, "/tools") {
			w.Write([]byte(declarationBody))
			return
		}
		w.Write([]byte(`{"id":"f","status":"succeeded","context":{"encrypted_output":"cipher"}}`))
	})

	client.Tools(context.Background())
	client.ExecuteFiber(context.Background(), "web_search", `{"query":"x"}`)

	for _, turn := range recorder.Turns() {
		assertCredentialFreeTurn(t, turn)
	}
}

func assertCredentialFreeTurn(t *testing.T, turn APITurn) {
	t.Helper()
	serialized, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("序列化凭据失败: %v", err)
	}
	lowered := strings.ToLower(string(serialized))
	for _, forbidden := range []string{"authorization", "bearer", "secret-key"} {
		if strings.Contains(lowered, forbidden) {
			t.Fatalf("%s 凭据里出现了 %q: %s", turn.Kind, forbidden, serialized)
		}
	}
}
