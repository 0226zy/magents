package session

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0226zy/magents/web-search-agent-go/internal/acceptance"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/adapter/moonshot"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/evidence"
)

const declaration = `{"type":"function","function":{"name":"web_search",` +
	`"parameters":{"type":"object","properties":{"query":{"type":"string"}}}}}`

// fakeMoonshot 是一台按 Moonshot 协议应答的假 provider：
// 第一轮要求并行搜两次，第二轮给出最终答案。
func fakeMoonshot(t *testing.T) *httptest.Server {
	t.Helper()
	chatCalls := 0
	fiberCalls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("请求缺少鉴权头")
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/tools"):
			io.WriteString(w, `{"object":"list","tools":[`+declaration+`]}`)

		case strings.HasSuffix(r.URL.Path, "/fibers"):
			fiberCalls++
			io.WriteString(w, `{"id":"fiber-`+string(rune('a'+fiberCalls))+
				`","status":"succeeded","context":{"encrypted_output":"cipher"}}`)

		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			chatCalls++
			if chatCalls == 1 {
				io.WriteString(w, `{"id":"chat-1","choices":[{"finish_reason":"tool_calls","message":{
                    "content":"","reasoning_content":"先并行搜两条线索","tool_calls":[
                    {"id":"call-1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"first\"}"}},
                    {"id":"call-2","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"second\"}"}}]}}],
                    "usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":10},
                    "completion_tokens_details":{"reasoning_tokens":5}}}`)
				return
			}
			io.WriteString(w, `{"id":"chat-2","choices":[{"finish_reason":"stop","message":{
                "content":"综合结论：见 https://asean.org/a","reasoning_content":"够了"}}],
                "usage":{"prompt_tokens":200,"completion_tokens":30}}`)

		default:
			t.Errorf("未预期的端点: %s", r.URL.Path)
		}
	}))
}

func TestSessionProducesCompleteEvidenceForOneQuestion(t *testing.T) {
	server := fakeMoonshot(t)
	defer server.Close()

	backend := entity.Backend{
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Model:    "kimi-k3",
		Provider: "moonshot",
	}
	payload, result := newTestSession(backend).Ask(context.Background(), "东盟有几个成员国？", 5)

	if result.IsFailure() {
		t.Fatalf("不应失败: %q", payload.Answer)
	}
	if payload.Provider != "moonshot" || payload.Model != "kimi-k3" {
		t.Fatalf("载荷元信息不对: %+v", payload)
	}
	if payload.Question != "东盟有几个成员国？" {
		t.Fatalf("问题未记录: %q", payload.Question)
	}

	kinds := map[string]int{}
	for _, turn := range payload.APITurns {
		kinds[turn.Kind]++
	}
	want := map[string]int{"formula_tools": 1, "chat_completion": 2, "formula_fiber": 2}
	for kind, count := range want {
		if kinds[kind] != count {
			t.Fatalf("%s 凭据应有 %d 条，实际 %d（全部: %v）", kind, count, kinds[kind], kinds)
		}
	}

	// thought, action, action, observation, observation, thought, answer
	if len(payload.Trace) != 7 {
		t.Fatalf("轨迹步骤数不对: %d", len(payload.Trace))
	}
}

// 从真实协议跑出来的载荷，必须能被验收层直接读懂。
func TestSessionPayloadSatisfiesStructuralAcceptanceChecks(t *testing.T) {
	server := fakeMoonshot(t)
	defer server.Close()

	backend := entity.Backend{APIKey: "test-key", BaseURL: server.URL, Model: "kimi-k3", Provider: "moonshot"}
	payload, _ := newTestSession(backend).Ask(context.Background(), "问题", 5)

	serialized, err := evidence.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var view acceptance.Payload
	if err := json.Unmarshal(serialized, &view); err != nil {
		t.Fatalf("验收层无法解析运行载荷: %v", err)
	}

	checks := acceptance.Validate(view, acceptance.Options{}).Checks
	structural := map[string]bool{
		"exact_model":                                   checks.ExactModel,
		"one_real_formula_declaration_fetch":            checks.OneRealFormulaDeclarationFetch,
		"provider_formula_declares_standard_web_search": checks.ProviderFormulaDeclaresStandardWebSearch,
		"formula_tool_declared_each_chat_turn":          checks.FormulaToolDeclaredEachChatTurn,
		"all_fibers_succeeded":                          checks.AllFibersSucceeded,
		"multiple_distinct_formula_fibers":              checks.MultipleDistinctFormulaFibers,
		"fiber_requests_match_model_actions":            checks.FiberRequestsMatchModelActions,
		"reasoning_observed":                            checks.ReasoningObserved,
		"final_answer_observed":                         checks.FinalAnswerObserved,
	}
	for name, passed := range structural {
		if !passed {
			t.Fatalf("结构性断言 %s 未通过", name)
		}
	}
	// 同一轮并行扇出两次搜索：按语义这不算「连续追查」。
	if checks.SequentialSearchRoundsObserved {
		t.Fatal("单轮扇出不应被算作多轮追查")
	}
}

// 证据里绝不能出现鉴权头或密钥值。
func TestSessionEvidenceIsCredentialFree(t *testing.T) {
	server := fakeMoonshot(t)
	defer server.Close()
	t.Setenv("MOONSHOT_API_KEY", "test-key-value-long-enough")

	backend := entity.Backend{
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Model:    "kimi-k3",
		Provider: "moonshot",
	}
	payload, _ := newTestSession(backend).Ask(context.Background(), "问题", 5)

	serialized, err := evidence.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := evidence.AssertCredentialFree(string(serialized)); err != nil {
		t.Fatalf("运行载荷里出现凭据痕迹: %v", err)
	}
	for _, turn := range payload.APITurns {
		if strings.Contains(strings.ToLower(string(turn.Request)), "authorization") {
			t.Fatalf("%s 的请求记录里出现了鉴权头: %s", turn.Kind, turn.Request)
		}
	}
}

func TestSessionUsesNoSearchPathForFallbackBackend(t *testing.T) {
	var chatCalls, otherCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			chatCalls++
			io.WriteString(w, `{"id":"c","choices":[{"finish_reason":"stop","message":{"content":"凭记忆作答"}}],"usage":{}}`)
			return
		}
		otherCalls++
	}))
	defer server.Close()

	backend := entity.Backend{
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Model:    "moonshotai/kimi-k2.6",
		Provider: "openrouter",
	}
	payload, _ := newTestSession(backend).Ask(context.Background(), "问题", 5)

	if payload.Answer != "凭记忆作答" {
		t.Fatalf("答案不对: %q", payload.Answer)
	}
	if otherCalls != 0 {
		t.Fatal("兜底通道不应访问 Formula 端点")
	}
	if chatCalls != 1 {
		t.Fatalf("chat 调用次数不对: %d", chatCalls)
	}
}

// newTestSession 用与 shared/deps 相同的装配方式，把客户端指向 httptest 服务器。
func newTestSession(backend entity.Backend) *Session {
	recorder := entity.NewRecorder()
	httpClient := moonshot.NewHTTPClient()
	chat := moonshot.NewChatClient(backend.APIKey, backend.BaseURL, backend.Model, recorder, httpClient)
	formula := moonshot.NewFormulaClient(backend.APIKey, backend.BaseURL, moonshot.DefaultFormulaURI, recorder, httpClient)
	return Open(backend, chat, formula, recorder, Options{})
}
