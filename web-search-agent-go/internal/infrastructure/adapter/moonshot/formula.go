package moonshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultFormulaURI 是 kimi-k3 官方 web 搜索 Formula。
const DefaultFormulaURI = "moonshot/web-search:latest"

// FormulaClient 负责两个 Formula 端点：
//
//	GET  {baseURL}/formulas/{uri}/tools
//	POST {baseURL}/formulas/{uri}/fibers
type FormulaClient struct {
	transport
	baseURL string
	uri     string
	turns   *Recorder
}

// NewFormulaClient 构造 Formula 协议客户端。
func NewFormulaClient(apiKey, baseURL, uri string, recorder *Recorder, httpClient *http.Client) *FormulaClient {
	if uri == "" {
		uri = DefaultFormulaURI
	}
	return &FormulaClient{
		transport: newTransport(apiKey, httpClient),
		baseURL:   strings.TrimRight(baseURL, "/"),
		uri:       uri,
		turns:     recorder,
	}
}

// URI 返回 Formula 标识。
func (f *FormulaClient) URI() string { return f.uri }

type toolsResponse struct {
	Tools []json.RawMessage `json:"tools"`
}

// Tools 拉取 provider 的权威工具声明。
//
// 返回的每条声明都是 json.RawMessage 原文：它会被原样转发进 chat 请求，
// 中途解码再编码会丢掉 provider schema 里任何未声明的字段。
func (f *FormulaClient) Tools(ctx context.Context) ([]json.RawMessage, error) {
	url := fmt.Sprintf("%s/formulas/%s/tools", f.baseURL, f.uri)
	request, err := json.Marshal(HTTPRequestRecord{Method: http.MethodGet, URL: url})
	if err != nil {
		return nil, err
	}

	started := time.Now()
	status, payload, err := f.do(ctx, http.MethodGet, url, nil)

	fail := func(failure error) ([]json.RawMessage, error) {
		f.turns.Append(APITurn{
			Kind:       "formula_tools",
			FormulaURI: f.uri,
			Request:    request,
			HTTPStatus: status,
			Elapsed:    elapsedSeconds(started),
			Error:      newTurnError(failure, payload),
		})
		return nil, failure
	}

	if err != nil {
		return fail(err)
	}

	var decoded toolsResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fail(fmt.Errorf("解析 Formula 声明响应失败: %w", err))
	}
	if len(decoded.Tools) == 0 {
		return fail(errors.New("Formula declaration response has no tools"))
	}
	if !HasWebSearchDeclaration(decoded.Tools) {
		return fail(errors.New("Formula declaration does not contain function web_search"))
	}

	f.turns.Append(APITurn{
		Kind:       "formula_tools",
		FormulaURI: f.uri,
		Request:    request,
		HTTPStatus: status,
		Response:   payload,
		Elapsed:    elapsedSeconds(started),
	})
	return decoded.Tools, nil
}

type fiberResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Context struct {
		Output          json.RawMessage `json:"output"`
		EncryptedOutput json.RawMessage `json:"encrypted_output"`
	} `json:"context"`
}

// ExecuteFiber 按模型请求的原样执行一次 Formula Fiber。
//
// rawArguments 必须是模型生成的原始 JSON 字符串，而不是解析后重新序列化的副本。
//
// 成功路径的四个条件缺一不可：
//  1. HTTP 2xx
//  2. status == "succeeded"
//  3. context.output 非空，否则退回 context.encrypted_output
//  4. 两者都空 → 错误 "Succeeded Formula Fiber returned no output"
//
// 实测 Moonshot 的 web-search Formula 只返回 encrypted_output，
// context.output 分支从未触发；保留它是为了兼容其他 Formula。
func (f *FormulaClient) ExecuteFiber(ctx context.Context, name, rawArguments string) (string, error) {
	if rawArguments == "" {
		rawArguments = "{}"
	}
	url := fmt.Sprintf("%s/formulas/%s/fibers", f.baseURL, f.uri)
	body, err := json.Marshal(FiberRequest{Name: name, Arguments: rawArguments})
	if err != nil {
		return "", err
	}
	request, err := json.Marshal(HTTPRequestRecord{Method: http.MethodPost, URL: url, Body: body})
	if err != nil {
		return "", err
	}

	started := time.Now()
	status, payload, err := f.do(ctx, http.MethodPost, url, body)

	fail := func(failure error) (string, error) {
		f.turns.Append(APITurn{
			Kind:       "formula_fiber",
			FormulaURI: f.uri,
			Request:    request,
			HTTPStatus: status,
			Elapsed:    elapsedSeconds(started),
			Error:      newTurnError(failure, payload),
		})
		return "", failure
	}

	if err != nil {
		return fail(err)
	}

	var decoded fiberResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fail(fmt.Errorf("解析 Formula Fiber 响应失败: %w", err))
	}
	if decoded.Status != "succeeded" {
		return fail(fmt.Errorf("Formula Fiber did not succeed: %q", decoded.Status))
	}
	output, ok := rawToText(decoded.Context.Output)
	if !ok {
		if output, ok = rawToText(decoded.Context.EncryptedOutput); !ok {
			return fail(errors.New("Succeeded Formula Fiber returned no output"))
		}
	}

	f.turns.Append(APITurn{
		Kind:       "formula_fiber",
		FormulaURI: f.uri,
		Request:    request,
		HTTPStatus: status,
		Response:   payload,
		Elapsed:    elapsedSeconds(started),
	})
	return output, nil
}

// rawToText 把 Formula 的输出转成可回传给模型的文本。
//
// 对应 agent.py:295-297：字符串直接用，其他 JSON 值按原文保留
// （原文即 ensure_ascii=False 的等价物，非 ASCII 不会被转义）。
func rawToText(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return "", false
		}
		return text, text != ""
	}
	return string(trimmed), true
}
