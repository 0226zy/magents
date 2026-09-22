package acceptance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const toolDeclaration = `{
  "type": "function",
  "function": {
    "name": "web_search",
    "description": "Search the web",
    "parameters": {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]}
  }
}`

func status(code int) *int { return &code }

func declarationTurn() Turn {
	return Turn{
		Kind:       "formula_tools",
		FormulaURI: FormulaURI,
		Request:    TurnRequest{},
		HTTPStatus: status(200),
		Response:   TurnResponse{Tools: []json.RawMessage{json.RawMessage(toolDeclaration)}},
	}
}

func chatTurn(id string) Turn {
	return Turn{
		Kind:       "chat_completion",
		Request:    TurnRequest{Tools: []json.RawMessage{json.RawMessage(toolDeclaration)}},
		HTTPStatus: status(200),
		Response:   TurnResponse{ID: id, Usage: &Usage{}},
	}
}

func fiberTurn(id, query string) Turn {
	arguments, _ := json.Marshal(`{"query":"` + query + `"}`)
	return Turn{
		Kind:       "formula_fiber",
		FormulaURI: FormulaURI,
		Request:    TurnRequest{Body: &FiberBody{Name: "web_search", Arguments: arguments}},
		HTTPStatus: status(200),
		Response:   TurnResponse{ID: id, Status: "succeeded"},
	}
}

const passingAnswer = "检索日期 2026-07-30：东盟现有 11（十一）个成员，" +
	"Timor-Leste（东帝汶）于 2025-10-26 加入。" +
	"雅加达 Jakarta 在总统令生效前仍是首都，Nusantara 为迁都目标。" +
	"https://asean.org/example https://peraturan.bpk.go.id/example"

func passingPayload() Payload {
	return Payload{
		Provider: "moonshot",
		Model:    "kimi-k3",
		BaseURL:  "https://api.moonshot.cn/v1",
		Answer:   passingAnswer,
		Trace: []Step{
			{Iteration: 1, Type: "thought"},
			{Iteration: 1, Type: "action", Tool: "web_search"},
			{Iteration: 1, Type: "observation", Tool: "web_search"},
			{Iteration: 2, Type: "action", Tool: "web_search"},
			{Iteration: 2, Type: "observation", Tool: "web_search"},
			{Iteration: 3, Type: "answer"},
		},
		APITurns: []Turn{
			declarationTurn(),
			chatTurn("chat-1"),
			fiberTurn("fiber-one", "ASEAN capitals"),
			chatTurn("chat-2"),
			fiberTurn("fiber-two", "ASEAN capital coordinates"),
			chatTurn("chat-3"),
		},
	}
}

func assertCheck(t *testing.T, result Result, name string, want bool) {
	t.Helper()
	for _, entry := range result.Checks.Entries() {
		if entry.Name == name {
			if entry.Passed != want {
				t.Fatalf("%s 期望 %v，实际 %v（未通过项: %v）", name, want, entry.Passed, result.FailedChecks())
			}
			return
		}
	}
	t.Fatalf("没有名为 %s 的断言", name)
}

func TestFiberIDsOnlyAcceptRealSucceededReceipts(t *testing.T) {
	turns := []Turn{
		{Kind: "formula_fiber", HTTPStatus: status(200),
			Response: TurnResponse{ID: "fiber-one", Status: "succeeded"}},
		{Kind: "formula_fiber", HTTPStatus: status(200),
			Response: TurnResponse{ID: "fiber-failed", Status: "failed"}},
		{Kind: "formula_fiber", HTTPStatus: status(500),
			Response: TurnResponse{ID: "fiber-error", Status: "succeeded"}},
		{Kind: "formula_fiber", HTTPStatus: status(200),
			Response: TurnResponse{Status: "succeeded"}},
		{Kind: "chat_completion", HTTPStatus: status(200),
			Response: TurnResponse{ID: "chat-one"}},
	}

	ids := FiberIDs(turns)
	if len(ids) != 1 || ids[0] != "fiber-one" {
		t.Fatalf("只应接受真实成功的凭据，实际 %v", ids)
	}
}

func TestAcceptanceRequiresDistinctSequentialFormulaFibersAndLinks(t *testing.T) {
	result := Validate(passingPayload(), Options{})

	if !result.Passed {
		t.Fatalf("完整正例应全过，未通过项: %v", result.FailedChecks())
	}
	if len(result.FiberIDs) != 2 {
		t.Fatalf("fiber_ids 不对: %v", result.FiberIDs)
	}
	if result.FormulaURI != FormulaURI {
		t.Fatalf("formula_uri 不对: %s", result.FormulaURI)
	}
}

// §0.2 第 4 条的锁定测试：一轮并行扇出多次搜索不构成「连续追查」。
func TestAcceptanceRejectsTwoFibersFromOneSearchRound(t *testing.T) {
	payload := passingPayload()
	payload.Trace = []Step{
		{Iteration: 1, Type: "thought"},
		{Iteration: 1, Type: "action", Tool: "web_search"},
		{Iteration: 1, Type: "action", Tool: "web_search"},
		{Iteration: 2, Type: "answer"},
	}

	result := Validate(payload, Options{})

	assertCheck(t, result, "sequential_search_rounds_observed", false)
	if result.Passed {
		t.Fatal("整体结论必须为 false")
	}
}

// 并行化把轨迹从 action/observation 交替改成了分组。
// 这条测试固化「18 项断言不依赖交错顺序」，避免后续改动引入顺序依赖。
func TestGroupedActionsAndObservationsDoNotChangeVerdict(t *testing.T) {
	interleaved := passingPayload()
	// 把第 2 轮改成「先两个 action 再两个 observation」，并补齐对应的 fiber。
	grouped := passingPayload()
	grouped.Trace = []Step{
		{Iteration: 1, Type: "thought"},
		{Iteration: 1, Type: "action", Tool: "web_search"},
		{Iteration: 1, Type: "observation", Tool: "web_search"},
		{Iteration: 2, Type: "action", Tool: "web_search"},
		{Iteration: 2, Type: "action", Tool: "web_search"},
		{Iteration: 2, Type: "observation", Tool: "web_search"},
		{Iteration: 2, Type: "observation", Tool: "web_search"},
		{Iteration: 3, Type: "answer"},
	}
	grouped.APITurns = append(grouped.APITurns, fiberTurn("fiber-three", "extra"))

	interleavedResult := Validate(interleaved, Options{})
	groupedResult := Validate(grouped, Options{})

	if !interleavedResult.Passed || !groupedResult.Passed {
		t.Fatalf("两种轨迹排布都应通过：交替 %v / 分组 %v",
			interleavedResult.FailedChecks(), groupedResult.FailedChecks())
	}
	for i, entry := range interleavedResult.Checks.Entries() {
		if other := groupedResult.Checks.Entries()[i]; other.Passed != entry.Passed {
			t.Fatalf("%s 因轨迹排布不同而改变结论", entry.Name)
		}
	}
}

// P1-4：Python 版用裸子串匹配 "go.id"，会把无关域名误判为官方来源。
func TestOfficialSourcesRejectsSubstringLookalikeDomains(t *testing.T) {
	payload := passingPayload()
	payload.Answer = strings.Replace(passingAnswer,
		"https://peraturan.bpk.go.id/example", "https://lego.idea.com/example", 1)

	result := Validate(payload, Options{})

	assertCheck(t, result, "official_sources_in_answer", false)
	if result.Passed {
		t.Fatal("误匹配的域名不应让整体通过")
	}
}

// P1-4：最贴题的 timor-leste.gov.tl 不在 Python 版白名单里，会被误判为 false。
func TestOfficialSourcesAcceptsTimorLesteGovernmentDomain(t *testing.T) {
	payload := passingPayload()
	payload.Answer = strings.Replace(passingAnswer,
		"https://peraturan.bpk.go.id/example", "https://timor-leste.gov.tl/example", 1)

	result := Validate(payload, Options{})

	assertCheck(t, result, "official_sources_in_answer", true)
}

func TestOfficialSourcesAcceptsSubdomainsOfInternationalOrg(t *testing.T) {
	payload := passingPayload()
	payload.Answer = strings.Replace(passingAnswer,
		"https://asean.org/example", "https://theaseanmagazine.asean.org/example", 1)

	result := Validate(payload, Options{})

	assertCheck(t, result, "official_sources_in_answer", true)
}

func TestOfficialSourcesRequiresBothOrgAndGovernment(t *testing.T) {
	payload := passingPayload()
	payload.Answer = strings.Replace(passingAnswer,
		"https://asean.org/example", "https://kaltim.antaranews.com/example", 1)

	result := Validate(payload, Options{})

	assertCheck(t, result, "official_sources_in_answer", false)
}

func TestAnswerHostsIgnoresTrailingPunctuation(t *testing.T) {
	hosts := answerHosts("见 https://asean.org/a。以及 （https://timor-leste.gov.tl/b），还有 https://www.mkri.id/c.")

	want := []string{"asean.org", "timor-leste.gov.tl", "www.mkri.id"}
	if len(hosts) != len(want) {
		t.Fatalf("主机名解析不对: %v", hosts)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Fatalf("主机名解析不对: %v", hosts)
		}
	}
}

// P2-1：基准日期是参数，不是散落在断言里的字面量。
func TestRetrievalDateIsParameterized(t *testing.T) {
	payload := passingPayload()
	payload.Answer = strings.Replace(passingAnswer, "2026-07-30", "2026-08-15", 1)

	assertCheck(t, Validate(payload, Options{}), "retrieval_date_reported", false)

	shifted := Options{AsOf: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)}
	assertCheck(t, Validate(payload, shifted), "retrieval_date_reported", true)
}

func TestRetrievalDateAcceptsChineseWriting(t *testing.T) {
	payload := passingPayload()
	payload.Answer = strings.Replace(passingAnswer, "检索日期 2026-07-30",
		"检索日期为 2026 年 7 月 30 日", 1)

	assertCheck(t, Validate(payload, Options{}), "retrieval_date_reported", true)
}

func TestDeclarationFetchMustBeExactlyOnceAndClean(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Payload)
		wantOne bool
	}{
		{"零次拉取", func(p *Payload) { p.APITurns = p.APITurns[1:] }, false},
		{"两次拉取", func(p *Payload) { p.APITurns = append(p.APITurns, declarationTurn()) }, false},
		{"拉取失败", func(p *Payload) {
			p.APITurns[0].Error = json.RawMessage(`{"class":"RuntimeError","message":"boom"}`)
		}, false},
		{"状态码非 200", func(p *Payload) { p.APITurns[0].HTTPStatus = status(503) }, false},
		{"正常一次", func(p *Payload) {}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := passingPayload()
			tc.mutate(&payload)
			assertCheck(t, Validate(payload, Options{}), "one_real_formula_declaration_fetch", tc.wantOne)
		})
	}
}

func TestFiberRequestsMustCarryRawArgumentString(t *testing.T) {
	payload := passingPayload()
	// 参数被解析成对象后重新塞回去：不再是模型生成的原始字符串。
	payload.APITurns[2].Request.Body.Arguments = json.RawMessage(`{"query":"ASEAN capitals"}`)

	assertCheck(t, Validate(payload, Options{}), "fiber_requests_match_model_actions", false)
}

func TestFiberCountMustMatchModelActions(t *testing.T) {
	payload := passingPayload()
	payload.APITurns = append(payload.APITurns, fiberTurn("fiber-extra", "unasked"))

	assertCheck(t, Validate(payload, Options{}), "fiber_requests_match_model_actions", false)
}

func TestAllFibersSucceededRejectsFailedReceipts(t *testing.T) {
	payload := passingPayload()
	payload.APITurns[4].Response.Status = "failed"

	result := Validate(payload, Options{})
	assertCheck(t, result, "all_fibers_succeeded", false)
	assertCheck(t, result, "multiple_distinct_formula_fibers", false)
}

func TestMultipleDistinctFibersRejectsRepeatedIDs(t *testing.T) {
	payload := passingPayload()
	payload.APITurns[4].Response.ID = "fiber-one"

	assertCheck(t, Validate(payload, Options{}), "multiple_distinct_formula_fibers", false)
}

func TestEveryChatTurnMustCarryTheDeclaration(t *testing.T) {
	payload := passingPayload()
	payload.APITurns[3].Request.Tools = nil

	assertCheck(t, Validate(payload, Options{}), "formula_tool_declared_each_chat_turn", false)
}

func TestFinalAnswerObservedRejectsFailureFallbacks(t *testing.T) {
	payload := passingPayload()
	payload.Answer = "搜索过程中出现错误: provider unavailable"

	assertCheck(t, Validate(payload, Options{}), "final_answer_observed", false)
}

func TestDirectMoonshotAPIRejectsFallbackProvider(t *testing.T) {
	payload := passingPayload()
	payload.Provider = "openrouter"
	payload.BaseURL = "https://openrouter.ai/api/v1"

	result := Validate(payload, Options{})
	assertCheck(t, result, "direct_moonshot_api", false)
}

func TestDirectMoonshotAPIToleratesTrailingSlash(t *testing.T) {
	payload := passingPayload()
	payload.BaseURL = "https://api.moonshot.cn/v1/"

	assertCheck(t, Validate(payload, Options{}), "direct_moonshot_api", true)
}

func TestSummarizeUsageAddsUpEveryChatTurn(t *testing.T) {
	first := chatTurn("chat-1")
	first.Response.Usage = &Usage{PromptTokens: 10, CompletionTokens: 3}
	first.Response.Usage.PromptTokensDetails.CachedTokens = 4
	first.Response.Usage.CompletionTokensDetails.ReasoningTokens = 2
	second := chatTurn("chat-2")
	second.Response.Usage = &Usage{PromptTokens: 5, CompletionTokens: 1}

	totals := SummarizeUsage([]Turn{first, second, fiberTurn("f", "q")})

	want := UsageTotals{
		PromptTokens: 15, CompletionTokens: 4, TotalTokens: 19,
		CachedPromptTokens: 4, ReasoningTokens: 2,
	}
	if totals != want {
		t.Fatalf("汇总不对\n期望: %+v\n实际: %+v", want, totals)
	}
}

func TestChecksSerializeInPythonKeyOrder(t *testing.T) {
	serialized, err := json.Marshal(Validate(passingPayload(), Options{}).Checks)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.HasPrefix(string(serialized), `{"direct_moonshot_api":`) {
		t.Fatalf("首个键应为 direct_moonshot_api: %s", serialized)
	}
	if len(Validate(passingPayload(), Options{}).Checks.Entries()) != 18 {
		t.Fatal("断言数应为 18")
	}
}
