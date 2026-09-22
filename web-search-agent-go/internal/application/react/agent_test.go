package react

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/derr"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/adapter/moonshot"
)

const declaration = `{"type":"function","function":{"name":"web_search","parameters":{"type":"object"}}}`

// fakeChat 按预设顺序吐出模型响应，对应 Python 测试里的 Mock(side_effect=...)。
type fakeChat struct {
	responses []*entity.ChatResponse
	err       error
	calls     int
	seen      [][]entity.Message
	seenTools [][]json.RawMessage
}

func (f *fakeChat) Chat(_ context.Context, messages []entity.Message, tools []json.RawMessage) (*entity.ChatResponse, error) {
	f.calls++
	f.seen = append(f.seen, append([]entity.Message(nil), messages...))
	f.seenTools = append(f.seenTools, tools)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.responses) == 0 {
		return nil, errors.New("fakeChat: 预设响应已耗尽")
	}
	next := f.responses[0]
	f.responses = f.responses[1:]
	return next, nil
}

// fakeFormula 记录每次 fiber 收到的原始参数，并观测并发度。
type fakeFormula struct {
	output      string
	err         error
	toolsErr    error
	delay       time.Duration
	mu          sync.Mutex
	args        []string
	toolsCalls  int
	live        int
	peakLive    int
	outputByArg map[string]string
}

func (f *fakeFormula) Tools(context.Context) ([]json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolsCalls++
	if f.toolsErr != nil {
		return nil, f.toolsErr
	}
	return []json.RawMessage{json.RawMessage(declaration)}, nil
}

func (f *fakeFormula) ExecuteFiber(_ context.Context, _, rawArguments string) (string, error) {
	f.mu.Lock()
	f.args = append(f.args, rawArguments)
	f.live++
	if f.live > f.peakLive {
		f.peakLive = f.live
	}
	f.mu.Unlock()

	time.Sleep(f.delay)

	f.mu.Lock()
	f.live--
	output := f.output
	if mapped, ok := f.outputByArg[rawArguments]; ok {
		output = mapped
	}
	f.mu.Unlock()

	if f.err != nil {
		return "", f.err
	}
	return output, nil
}

func (f *fakeFormula) seenArgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.args...)
}

func toolCall(id, arguments string) entity.ToolCall {
	return entity.ToolCall{
		ID:       id,
		Type:     "function",
		Function: moonshot.FunctionCall{Name: "web_search", Arguments: arguments},
	}
}

func choice(finishReason, content, reasoning string, calls ...entity.ToolCall) *entity.ChatResponse {
	return &entity.ChatResponse{
		Choices: []moonshot.Choice{{
			FinishReason: finishReason,
			Message: moonshot.ChoiceMessage{
				Content:          content,
				ReasoningContent: reasoning,
				ToolCalls:        calls,
			},
		}},
	}
}

func buildAgent(t *testing.T, formula *fakeFormula, responses ...*entity.ChatResponse) (*Agent, *fakeChat, *fakeFormula) {
	t.Helper()
	if formula == nil {
		formula = &fakeFormula{output: "encrypted formula output"}
	}
	chat := &fakeChat{responses: responses}
	return New(chat, formula, Options{}), chat, formula
}

func stepTypes(steps []Step) []StepType {
	types := make([]StepType, 0, len(steps))
	for _, step := range steps {
		types = append(types, step.Type)
	}
	return types
}

func assertStepTypes(t *testing.T, got []Step, want ...StepType) {
	t.Helper()
	gotTypes := stepTypes(got)
	if len(gotTypes) != len(want) {
		t.Fatalf("轨迹步骤数不对\n期望: %v\n实际: %v", want, gotTypes)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Fatalf("轨迹步骤不对\n期望: %v\n实际: %v", want, gotTypes)
		}
	}
}

func TestFormatTraceStepFormatsActionWithUnicodeArguments(t *testing.T) {
	rendered := FormatTraceStep(Step{
		Iteration: 2,
		Type:      StepAction,
		Tool:      "web_search",
		Args:      map[string]any{"query": "서울 날씨"},
	}, DefaultTraceMaxLen)

	if rendered != `🔧 [2] 行动: 调用工具 web_search  参数={"query":"서울 날씨"}` {
		t.Fatalf("渲染结果不对: %s", rendered)
	}
}

func TestFormatTraceStepTruncatesLongContent(t *testing.T) {
	rendered := FormatTraceStep(Step{Iteration: 1, Type: StepThought, Content: "abcdef"}, 3)

	if rendered != "💭 [1] 思考: abc…（省略 3 字）" {
		t.Fatalf("渲染结果不对: %s", rendered)
	}
}

func TestFormatTraceStepCountsRunesNotBytes(t *testing.T) {
	rendered := FormatTraceStep(Step{Iteration: 1, Type: StepObservation, Content: "东盟成员国名单"}, 2)

	if rendered != "👀 [1] 观察: 东盟…（省略 5 字）" {
		t.Fatalf("按字符而非字节截断: %s", rendered)
	}
}

func TestAgentLoopRecordsToolFlowAndFinalAnswer(t *testing.T) {
	agent, chat, formula := buildAgent(t, nil,
		choice("tool_calls", "", "공식 설명을 검색해야 한다.", toolCall("call-1", `{"query": "Moonshot caching"}`)),
		choice("stop", "Context Caching 설명입니다.", ""),
	)

	result := agent.Run(context.Background(), "Context Caching이 뭐야?", 5)

	if result.Answer != "Context Caching 설명입니다." {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	if result.IsFailure() {
		t.Fatal("正常作答不应判为失败")
	}
	assertStepTypes(t, result.Trace, StepThought, StepAction, StepObservation, StepAnswer)
	if chat.calls != 2 {
		t.Fatalf("chat 应调用 2 次，实际 %d", chat.calls)
	}
	if args := formula.seenArgs(); len(args) != 1 || args[0] != `{"query": "Moonshot caching"}` {
		t.Fatalf("转发给 Formula 的必须是模型原文: %v", args)
	}

	history := agent.History()
	if len(history) != 5 {
		t.Fatalf("历史长度不对: %d", len(history))
	}
	assistant := history[2]
	if assistant.Role != "assistant" || assistant.Content != "" || len(assistant.ToolCalls) != 1 {
		t.Fatalf("assistant 消息形状不对: %+v", assistant)
	}
	if assistant.ToolCalls[0].ID != "call-1" || assistant.ToolCalls[0].Type != "function" {
		t.Fatalf("tool_calls 不对: %+v", assistant.ToolCalls[0])
	}
	tool := history[3]
	if tool.Role != "tool" || tool.ToolCallID != "call-1" || tool.Content != "encrypted formula output" {
		t.Fatalf("tool 消息形状不对: %+v", tool)
	}
	final := history[4]
	if final.Role != "assistant" || final.Content != result.Answer || len(final.ToolCalls) != 0 {
		t.Fatalf("最终 assistant 消息不对: %+v", final)
	}
}

// 回传给 Moonshot 的历史里若带上 reasoning_content，会触发 400 tokenization failed。
func TestHistoryNeverCarriesReasoningContent(t *testing.T) {
	agent, chat, _ := buildAgent(t, nil,
		choice("tool_calls", "", "internal thinking", toolCall("call-1", `{"query":"x"}`)),
		choice("stop", "done", "more thinking"),
	)

	agent.Run(context.Background(), "question", 5)

	for _, messages := range chat.seen {
		serialized, err := json.Marshal(messages)
		if err != nil {
			t.Fatalf("序列化历史失败: %v", err)
		}
		if strings.Contains(string(serialized), "thinking") ||
			strings.Contains(string(serialized), "reasoning") {
			t.Fatalf("回传消息里不得出现思考内容: %s", serialized)
		}
	}
}

func TestAgentLoopHandlesMultipleToolCalls(t *testing.T) {
	formula := &fakeFormula{outputByArg: map[string]string{
		`{"query":"first"}`:  "first result",
		`{"query":"second"}`: "second result",
	}}
	agent, _, _ := buildAgent(t, formula,
		choice("tool_calls", "", "",
			toolCall("call-1", `{"query":"first"}`),
			toolCall("call-2", `{"query":"second"}`)),
		choice("stop", "combined answer", ""),
	)

	result := agent.Run(context.Background(), "compare", 5)

	if result.Answer != "combined answer" {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	// 并行执行下 action 先全部发出、observation 后全部发出。
	assertStepTypes(t, result.Trace,
		StepAction, StepAction, StepObservation, StepObservation, StepAnswer)

	var toolMessages []entity.Message
	for _, message := range agent.History() {
		if message.Role == "tool" {
			toolMessages = append(toolMessages, message)
		}
	}
	if len(toolMessages) != 2 {
		t.Fatalf("应有 2 条 tool 消息，实际 %d", len(toolMessages))
	}
	if toolMessages[0].ToolCallID != "call-1" || toolMessages[1].ToolCallID != "call-2" {
		t.Fatalf("tool_call_id 顺序错乱: %+v", toolMessages)
	}
	// 顺序错乱会让 tool_call_id 与内容错配。
	if toolMessages[0].Content != "first result" || toolMessages[1].Content != "second result" {
		t.Fatalf("结果未按原顺序回填: %+v", toolMessages)
	}
}

func TestParallelToolExecutionRespectsLimitAndPreservesOrder(t *testing.T) {
	formula := &fakeFormula{
		delay: 40 * time.Millisecond,
		outputByArg: map[string]string{
			`{"query":"a"}`: "A", `{"query":"b"}`: "B",
			`{"query":"c"}`: "C", `{"query":"d"}`: "D",
		},
	}
	chat := &fakeChat{responses: []*entity.ChatResponse{
		choice("tool_calls", "", "",
			toolCall("call-1", `{"query":"a"}`), toolCall("call-2", `{"query":"b"}`),
			toolCall("call-3", `{"query":"c"}`), toolCall("call-4", `{"query":"d"}`)),
		choice("stop", "done", ""),
	}}
	agent := New(chat, formula, Options{MaxParallelTools: 2})

	started := time.Now()
	result := agent.Run(context.Background(), "question", 5)
	elapsed := time.Since(started)

	if result.Answer != "done" {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	formula.mu.Lock()
	peak := formula.peakLive
	formula.mu.Unlock()
	if peak < 2 {
		t.Fatalf("同轮工具应并行执行，实测峰值并发 %d", peak)
	}
	if peak > 2 {
		t.Fatalf("并发上限未生效，实测峰值并发 %d", peak)
	}
	// 4 次 40ms 调用，限流 2 时约需 80ms；串行需 160ms。
	if elapsed >= 160*time.Millisecond {
		t.Fatalf("耗时 %v，看起来仍是串行执行", elapsed)
	}

	var observations []string
	for _, step := range result.Trace {
		if step.Type == StepObservation {
			observations = append(observations, step.Content)
		}
	}
	want := []string{"A", "B", "C", "D"}
	for i := range want {
		if observations[i] != want[i] {
			t.Fatalf("observation 未按原顺序回填: %v", observations)
		}
	}
}

func TestAgentLoopStopsAtIterationLimit(t *testing.T) {
	agent, chat, _ := buildAgent(t, nil,
		choice("tool_calls", "", "", toolCall("call-1", `{"query":"x"}`)),
		choice("tool_calls", "", "", toolCall("call-2", `{"query":"y"}`)),
	)

	result := agent.Run(context.Background(), "keep searching", 1)

	if result.Answer != MaxIterationsMessage {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	if result.Failure != FailureMaxIterations || !result.IsFailure() {
		t.Fatalf("失败成因不对: %v", result.Failure)
	}
	if chat.calls != 1 {
		t.Fatalf("chat 只应调用 1 次，实际 %d", chat.calls)
	}
}

func TestAgentLoopReturnsReadableError(t *testing.T) {
	chat := &fakeChat{err: errors.New("provider unavailable")}
	agent := New(chat, &fakeFormula{}, Options{})

	result := agent.Run(context.Background(), "question", 5)

	if result.Answer != "搜索过程中出现错误: provider unavailable" {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	if result.Failure != FailureProvider {
		t.Fatalf("失败成因不对: %v", result.Failure)
	}
	if len(result.Trace) != 0 {
		t.Fatalf("首轮失败时轨迹应为空: %v", result.Trace)
	}
}

func TestAgentLoopReportsDeclarationFetchFailure(t *testing.T) {
	formula := &fakeFormula{toolsErr: errors.New("declaration unavailable")}
	chat := &fakeChat{responses: []*entity.ChatResponse{choice("stop", "unused", "")}}
	agent := New(chat, formula, Options{})

	result := agent.Run(context.Background(), "question", 5)

	if !strings.Contains(result.Answer, "declaration unavailable") {
		t.Fatalf("声明拉取失败应被如实报告: %q", result.Answer)
	}
	if chat.calls != 0 {
		t.Fatalf("拉不到声明就不该发 chat 请求，实际 %d 次", chat.calls)
	}
}

// finish_reason=length 且 content 为空，不能伪装成误导性的「无法获取足够信息」。
func TestAgentLoopMarksTruncatedEmptyAnswer(t *testing.T) {
	agent, _, _ := buildAgent(t, nil, choice("length", "", ""))

	result := agent.Run(context.Background(), "question", 5)

	if strings.Contains(result.Answer, "无法获取足够") {
		t.Fatalf("不得误报无法获取信息: %q", result.Answer)
	}
	if !strings.Contains(result.Answer, "截断") {
		t.Fatalf("必须标注截断: %q", result.Answer)
	}
	if result.Trace[len(result.Trace)-1].Type != StepAnswer {
		t.Fatalf("最后一步应是 answer: %v", stepTypes(result.Trace))
	}
	if result.IsFailure() {
		t.Fatal("截断不算失败：它仍然是一个最终答案")
	}
}

// 被 max_tokens 截断的部分答案要带标注返回，绝不能当作完整答案。
func TestAgentLoopMarksTruncatedPartialAnswer(t *testing.T) {
	agent, _, _ := buildAgent(t, nil, choice("length", "部分答案，被截", ""))

	result := agent.Run(context.Background(), "question", 5)

	if !strings.HasPrefix(result.Answer, "部分答案，被截") {
		t.Fatalf("应以已生成内容开头: %q", result.Answer)
	}
	if !strings.Contains(result.Answer, "截断") {
		t.Fatalf("必须标注截断: %q", result.Answer)
	}
	history := agent.History()
	last := history[len(history)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content, "截断") {
		t.Fatalf("历史里应存带标注的版本: %+v", last)
	}
}

// finish_reason=stop 但 content 为空：Python 版退出循环返回 NO_INFO_MESSAGE，
// 而不是继续下一轮迭代。
func TestAgentLoopReturnsNoInfoOnEmptyStop(t *testing.T) {
	agent, chat, _ := buildAgent(t, nil,
		choice("stop", "", ""),
		choice("stop", "本不该被用到", ""),
	)

	result := agent.Run(context.Background(), "question", 5)

	if result.Answer != NoInfoMessage {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	if result.Failure != FailureNoInfo {
		t.Fatalf("失败成因不对: %v", result.Failure)
	}
	if chat.calls != 1 {
		t.Fatalf("不应继续迭代，chat 实际调用 %d 次", chat.calls)
	}
}

// 略微非法的工具 JSON 不能中断 ReAct 循环。
func TestAgentLoopSurvivesMalformedToolArguments(t *testing.T) {
	bad := `{"query": "moonshot",}` // 尾随逗号
	agent, _, formula := buildAgent(t, nil,
		choice("tool_calls", "", "", toolCall("call-bad", bad)),
		choice("stop", "recovered answer", ""),
	)

	result := agent.Run(context.Background(), "what is caching?", 5)

	if result.Answer != "recovered answer" {
		t.Fatalf("循环未存活: %q", result.Answer)
	}
	if args := formula.seenArgs(); len(args) != 1 || args[0] != bad {
		t.Fatalf("传给 Formula 的必须是原始坏字符串: %v", args)
	}
	var action *Step
	for i := range result.Trace {
		if result.Trace[i].Type == StepAction {
			action = &result.Trace[i]
		}
	}
	if action == nil {
		t.Fatal("应仍记录 action 步骤")
	}
	if len(action.Args) != 0 {
		t.Fatalf("坏 JSON 应按空对象渲染: %v", action.Args)
	}
}

func TestUnknownToolProducesErrorObservation(t *testing.T) {
	unknown := entity.ToolCall{
		ID: "call-1", Type: "function",
		Function: moonshot.FunctionCall{Name: "calculator", Arguments: `{}`},
	}
	agent, _, formula := buildAgent(t, nil,
		choice("tool_calls", "", "", unknown),
		choice("stop", "done", ""),
	)

	result := agent.Run(context.Background(), "question", 5)

	var observation string
	for _, step := range result.Trace {
		if step.Type == StepObservation {
			observation = step.Content
		}
	}
	if observation != "Error: unable to find tool by name 'calculator'" {
		t.Fatalf("未知工具的观察文案不对: %q", observation)
	}
	if len(formula.seenArgs()) != 0 {
		t.Fatal("未知工具不应触发 Formula 调用")
	}
}

func TestToolExecutionFailureIsReportedAsProviderFailure(t *testing.T) {
	formula := &fakeFormula{err: errors.New("fiber overloaded")}
	agent, _, _ := buildAgent(t, formula,
		choice("tool_calls", "", "", toolCall("call-1", `{"query":"x"}`)),
	)

	result := agent.Run(context.Background(), "question", 5)

	if result.Failure != FailureProvider || !strings.Contains(result.Answer, "fiber overloaded") {
		t.Fatalf("工具失败应如实上报: %v / %q", result.Failure, result.Answer)
	}
}

// 「Request timed out.」本身读起来像网络故障，必须告诉读者该调哪个旋钮，
// 以及 429 重试会烧掉超时预算。
func TestTimeoutAnswerNamesTheBudgetAndThe429Suspect(t *testing.T) {
	chat := &fakeChat{err: fmt.Errorf("请求失败: %w", context.DeadlineExceeded)}
	agent := New(chat, &fakeFormula{}, Options{Timeout: 180 * time.Second})

	result := agent.Run(context.Background(), "question", 5)

	if !strings.HasPrefix(result.Answer, SearchErrorPrefix) {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	for _, marker := range []string{"180 秒", "SEARCH_TIMEOUT", "429"} {
		if !strings.Contains(result.Answer, marker) {
			t.Fatalf("提示里缺少 %q: %q", marker, result.Answer)
		}
	}
}

func TestRateLimitAnswerSaysItIsARateLimit(t *testing.T) {
	chat := &fakeChat{err: &derr.StatusError{StatusCode: 429, Body: "rate limited"}}
	agent := New(chat, &fakeFormula{}, Options{Timeout: 180 * time.Second})

	result := agent.Run(context.Background(), "question", 5)

	if !strings.Contains(result.Answer, "速率限制") {
		t.Fatalf("答案应指出是速率限制: %q", result.Answer)
	}
}

// 提示是在错误路径里构造的，没有已知超时预算时也必须读得通。
func TestErrorHintWithoutAKnownTimeoutStillReads(t *testing.T) {
	hint := errorHint(context.DeadlineExceeded, 0)

	if !strings.Contains(hint, "SEARCH_TIMEOUT") {
		t.Fatalf("提示不完整: %q", hint)
	}
	prefix, _, _ := strings.Cut(hint, "kimi-k3")
	if strings.Contains(prefix, "秒") {
		t.Fatalf("没有预算时不应编造秒数: %q", hint)
	}
}

func TestErrorHintIsEmptyForUnrelatedFailures(t *testing.T) {
	if hint := errorHint(errors.New("provider unavailable"), 180*time.Second); hint != "" {
		t.Fatalf("无关错误不应附加提示: %q", hint)
	}
}

func TestIsFailureAnswerCoversEveryFallback(t *testing.T) {
	cases := []struct {
		answer string
		want   bool
	}{
		{"搜索过程中出现错误: boom", true},
		{MaxIterationsMessage, true},
		{NoInfoMessage, true},
		{"正常答案", false},
		{truncationNote, false},
	}
	for _, tc := range cases {
		if got := IsFailureAnswer(tc.answer); got != tc.want {
			t.Fatalf("%q 期望 %v，实际 %v", tc.answer, tc.want, got)
		}
	}
}

// 声明的缓存作用域是「一个问题」，不是「一个客户端」：
// 验收断言 one_real_formula_declaration_fetch 要求每个问题恰好一次真实拉取。
func TestDeclarationIsFetchedOncePerQuestion(t *testing.T) {
	formula := &fakeFormula{output: "out"}
	chat := &fakeChat{responses: []*entity.ChatResponse{
		choice("tool_calls", "", "", toolCall("call-1", `{"query":"x"}`)),
		choice("stop", "first answer", ""),
		choice("stop", "second answer", ""),
	}}
	agent := New(chat, formula, Options{})

	agent.Run(context.Background(), "first question", 5)
	if formula.toolsCalls != 1 {
		t.Fatalf("一个问题内应恰好拉取一次声明，实际 %d", formula.toolsCalls)
	}

	agent.Run(context.Background(), "second question", 5)
	if formula.toolsCalls != 2 {
		t.Fatalf("每个新问题都要重新拉取声明，实际 %d", formula.toolsCalls)
	}
}

func TestReuseDeclarationSkipsRefetchAcrossQuestions(t *testing.T) {
	formula := &fakeFormula{output: "out"}
	chat := &fakeChat{responses: []*entity.ChatResponse{
		choice("stop", "first", ""),
		choice("stop", "second", ""),
	}}
	agent := New(chat, formula, Options{ReuseDeclaration: true})

	agent.Run(context.Background(), "first", 5)
	agent.Run(context.Background(), "second", 5)

	if formula.toolsCalls != 1 {
		t.Fatalf("显式放宽后应复用声明，实际拉取 %d 次", formula.toolsCalls)
	}
}

func TestNoSearchModeSkipsDeclarationAndSendsNoTools(t *testing.T) {
	formula := &fakeFormula{}
	chat := &fakeChat{responses: []*entity.ChatResponse{choice("stop", "from memory", "")}}
	agent := New(chat, formula, Options{NoSearch: true})

	if result := agent.Run(context.Background(), "question", 5); result.Answer != "from memory" {
		t.Fatalf("答案不对: %q", result.Answer)
	}
	if formula.toolsCalls != 0 {
		t.Fatal("无搜索模式不应拉取 Formula 声明")
	}
	if len(chat.seenTools[0]) != 0 {
		t.Fatalf("无搜索模式不应带 tools: %v", chat.seenTools[0])
	}
}

func TestRunResetsRecorderForEachQuestion(t *testing.T) {
	recorder := &entity.Recorder{}
	recorder.Append(entity.APITurn{Kind: "chat_completion"})
	chat := &fakeChat{responses: []*entity.ChatResponse{choice("stop", "answer", "")}}
	agent := New(chat, &fakeFormula{}, Options{Recorder: recorder})

	agent.Run(context.Background(), "question", 5)

	if len(recorder.Turns()) != 0 {
		t.Fatalf("每个问题应从空凭据开始，实际 %d 条", len(recorder.Turns()))
	}
}

func TestVerboseModePrintsTraceInOrder(t *testing.T) {
	var buffer bytes.Buffer
	chat := &fakeChat{responses: []*entity.ChatResponse{
		choice("tool_calls", "", "先搜一下", toolCall("call-1", `{"query":"x"}`)),
		choice("stop", "答案", ""),
	}}
	agent := New(chat, &fakeFormula{output: "结果"}, Options{Verbose: true, Output: &buffer})

	agent.Run(context.Background(), "question", 5)

	output := buffer.String()
	for _, icon := range []string{"💭", "🔧", "👀", "✅"} {
		if !strings.Contains(output, icon) {
			t.Fatalf("verbose 输出缺少 %s: %s", icon, output)
		}
	}
	if strings.Index(output, "🔧") > strings.Index(output, "👀") {
		t.Fatalf("行动应先于观察打印: %s", output)
	}
}

func TestQuietModePrintsNothing(t *testing.T) {
	var buffer bytes.Buffer
	chat := &fakeChat{responses: []*entity.ChatResponse{choice("stop", "答案", "")}}
	agent := New(chat, &fakeFormula{}, Options{Output: &buffer})

	agent.Run(context.Background(), "question", 5)

	if buffer.Len() != 0 {
		t.Fatalf("非 verbose 模式不应有输出: %s", buffer.String())
	}
}
