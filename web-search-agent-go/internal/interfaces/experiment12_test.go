package interfaces

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/0226zy/magents/web-search-agent-go/internal/acceptance"
	"github.com/0226zy/magents/web-search-agent-go/internal/application/react"
	"github.com/0226zy/magents/web-search-agent-go/internal/application/session"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/adapter/moonshot"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/config"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/evidence"
)

func TestExperimentRequiresExactModel(t *testing.T) {
	isolateCredentials(t)
	var stdout, stderr bytes.Buffer

	code, err := RunExperiment12([]string{"-model", "kimi-k2.6"}, &stdout, &stderr)

	if err == nil || !strings.Contains(err.Error(), "kimi-k3") {
		t.Fatalf("应拒绝其他模型，实际 %v", err)
	}
	if code != 2 {
		t.Fatalf("退出码应为 2，实际 %d", code)
	}
}

func TestExperimentRejectsMalformedAsOf(t *testing.T) {
	isolateCredentials(t)
	var stdout, stderr bytes.Buffer

	_, err := RunExperiment12([]string{"-as-of", "2026/07/30"}, &stdout, &stderr)

	if err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("应拒绝非法日期，实际 %v", err)
	}
}

func TestExperimentRefusesToRunWithoutMoonshotCredential(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("OPENROUTER_API_KEY", "fallback-key")
	var stdout, stderr bytes.Buffer

	// 实验必须直连 Moonshot：OpenRouter 上没有 Formula web_search，
	// 跑出来的证据无法满足验收。
	_, err := RunExperiment12([]string{}, &stdout, &stderr)

	if !errors.Is(err, config.ErrNoCredential) {
		t.Fatalf("应拒绝启动，实际 %v", err)
	}
}

func TestQuestionCarriesTheAsOfDate(t *testing.T) {
	question := formatQuestion(acceptance.DefaultAsOf)

	if !strings.Contains(question, "2026 年 7 月 30 日") {
		t.Fatalf("问题里应写明基准日期: %s", question)
	}
	if !strings.Contains(question, "不要依赖记忆作答") {
		t.Fatalf("问题文案不完整: %s", question)
	}
}

func TestToAcceptancePayloadPreservesEvidenceShape(t *testing.T) {
	arguments := `{"query":"东盟成员国"}`
	body, _ := json.Marshal(moonshot.FiberRequest{Name: "web_search", Arguments: arguments})
	request, _ := json.Marshal(moonshot.HTTPRequestRecord{
		Method: "POST",
		URL:    "https://api.moonshot.cn/v1/formulas/moonshot/web-search:latest/fibers",
		Body:   body,
	})
	status := 200
	payload := session.Payload{
		Provider: "moonshot",
		Model:    "kimi-k3",
		BaseURL:  "https://api.moonshot.cn/v1",
		Answer:   "答案",
		Trace: []react.Step{
			{Iteration: 1, Type: react.StepAction, Tool: "web_search",
				Args: map[string]any{"query": "东盟成员国"}},
		},
		APITurns: []entity.APITurn{{
			Kind:       "formula_fiber",
			FormulaURI: moonshot.DefaultFormulaURI,
			Request:    request,
			HTTPStatus: &status,
			Response:   json.RawMessage(`{"id":"fiber-1","status":"succeeded"}`),
		}},
	}

	view := toAcceptancePayload(payload)

	if view.Model != "kimi-k3" || view.Provider != "moonshot" {
		t.Fatalf("元信息丢失: %+v", view)
	}
	if len(view.Trace) != 1 || view.Trace[0].Type != "action" || view.Trace[0].Tool != "web_search" {
		t.Fatalf("轨迹未被验收层读懂: %+v", view.Trace)
	}
	if len(view.APITurns) != 1 {
		t.Fatalf("凭据未被验收层读懂: %+v", view.APITurns)
	}
	turn := view.APITurns[0]
	if turn.Request.Body == nil || turn.Request.Body.Name != "web_search" {
		t.Fatalf("fiber 请求体未被读出: %+v", turn.Request)
	}
	// 原始 arguments 必须仍是 JSON 字符串，而不是被解析成对象。
	var decoded string
	if err := json.Unmarshal(turn.Request.Body.Arguments, &decoded); err != nil {
		t.Fatalf("arguments 不再是 JSON 字符串: %s", turn.Request.Body.Arguments)
	}
	if decoded != arguments {
		t.Fatalf("arguments 原文被改写: %q", decoded)
	}
	if turn.Response.ID != "fiber-1" || turn.Response.Status != "succeeded" {
		t.Fatalf("fiber 凭据未被读出: %+v", turn.Response)
	}
}

func TestDocumentSerializesWithStableKeyOrder(t *testing.T) {
	doc := document{
		SchemaVersion:           schemaVersion,
		ExperimentID:            experimentID,
		EvidenceMode:            "real_api",
		TransientFailedAttempts: []failedAttempt{},
	}

	serialized, err := evidence.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	text := string(serialized)
	if !strings.HasPrefix(text, "{\n  \"schema_version\": \"2.0\",") {
		t.Fatalf("键顺序应跟随字段声明: %.120s", text)
	}
	// 失败尝试为空时写成 []，不是 null：证据的形状不随结果变化。
	if !strings.Contains(text, `"transient_failed_attempts": []`) {
		t.Fatalf("空的失败列表应序列化为 []: %s", text)
	}
}
