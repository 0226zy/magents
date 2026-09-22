package acceptance

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/0226zy/magents/web-search-agent-go/internal/domain/contract"
)

// DefaultAsOf 是实验 1-2 的基准检索日期。
//
// 这套事实断言（东盟 11 国、东帝汶入盟、印尼迁都进度）与该日期绑定：
// 换一个基准日期就要重新校准断言，因此把它做成显式参数而不是散落的字面量。
var DefaultAsOf = time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

// Options 控制与日期绑定的断言。
type Options struct {
	// AsOf 是问题里给定的检索基准日期，零值取 DefaultAsOf。
	AsOf time.Time
}

func (o Options) asOf() time.Time {
	if o.AsOf.IsZero() {
		return DefaultAsOf
	}
	return o.AsOf
}

// Checks 是 18 项断言。字段顺序即 JSON 键顺序，与 Python 版的插入顺序一致，
// 这样两边的证据文件可以逐字节对照。
type Checks struct {
	DirectMoonshotAPI                        bool `json:"direct_moonshot_api"`
	ExactModel                               bool `json:"exact_model"`
	OneRealFormulaDeclarationFetch           bool `json:"one_real_formula_declaration_fetch"`
	ProviderFormulaDeclaresStandardWebSearch bool `json:"provider_formula_declares_standard_web_search"`
	ProviderResponseEachChatTurn             bool `json:"provider_response_each_chat_turn"`
	FormulaToolDeclaredEachChatTurn          bool `json:"formula_tool_declared_each_chat_turn"`
	AllFibersSucceeded                       bool `json:"all_fibers_succeeded"`
	MultipleDistinctFormulaFibers            bool `json:"multiple_distinct_formula_fibers"`
	FiberRequestsMatchModelActions           bool `json:"fiber_requests_match_model_actions"`
	SequentialSearchRoundsObserved           bool `json:"sequential_search_rounds_observed"`
	ReasoningObserved                        bool `json:"reasoning_observed"`
	FinalAnswerObserved                      bool `json:"final_answer_observed"`
	SourceLinksInAnswer                      bool `json:"source_links_in_answer"`
	OfficialSourcesInAnswer                  bool `json:"official_sources_in_answer"`
	CurrentElevenMemberFact                  bool `json:"current_eleven_member_fact"`
	TimorLesteAdmissionDate                  bool `json:"timor_leste_admission_date"`
	IndonesiaCapitalTransitionExplained      bool `json:"indonesia_capital_transition_explained"`
	RetrievalDateReported                    bool `json:"retrieval_date_reported"`
}

// Check 是一项断言的名字与结论。
type Check struct {
	Name   string
	Passed bool
}

// Entries 按证据文件里的键顺序列出全部断言。
func (c Checks) Entries() []Check {
	return []Check{
		{"direct_moonshot_api", c.DirectMoonshotAPI},
		{"exact_model", c.ExactModel},
		{"one_real_formula_declaration_fetch", c.OneRealFormulaDeclarationFetch},
		{"provider_formula_declares_standard_web_search", c.ProviderFormulaDeclaresStandardWebSearch},
		{"provider_response_each_chat_turn", c.ProviderResponseEachChatTurn},
		{"formula_tool_declared_each_chat_turn", c.FormulaToolDeclaredEachChatTurn},
		{"all_fibers_succeeded", c.AllFibersSucceeded},
		{"multiple_distinct_formula_fibers", c.MultipleDistinctFormulaFibers},
		{"fiber_requests_match_model_actions", c.FiberRequestsMatchModelActions},
		{"sequential_search_rounds_observed", c.SequentialSearchRoundsObserved},
		{"reasoning_observed", c.ReasoningObserved},
		{"final_answer_observed", c.FinalAnswerObserved},
		{"source_links_in_answer", c.SourceLinksInAnswer},
		{"official_sources_in_answer", c.OfficialSourcesInAnswer},
		{"current_eleven_member_fact", c.CurrentElevenMemberFact},
		{"timor_leste_admission_date", c.TimorLesteAdmissionDate},
		{"indonesia_capital_transition_explained", c.IndonesiaCapitalTransitionExplained},
		{"retrieval_date_reported", c.RetrievalDateReported},
	}
}

// AllPassed 报告 18 项是否全部成立。
func (c Checks) AllPassed() bool {
	for _, entry := range c.Entries() {
		if !entry.Passed {
			return false
		}
	}
	return true
}

// Result 是验收结论，字段与 Python 版 validate() 的返回一致。
type Result struct {
	Checks     Checks      `json:"checks"`
	Passed     bool        `json:"passed"`
	FormulaURI string      `json:"formula_uri"`
	FiberIDs   []string    `json:"fiber_ids"`
	Usage      UsageTotals `json:"usage"`
}

// FailedChecks 列出未通过的断言名，便于排障。
func (r Result) FailedChecks() []string {
	var failed []string
	for _, entry := range r.Checks.Entries() {
		if !entry.Passed {
			failed = append(failed, entry.Name)
		}
	}
	return failed
}

// Validate 对一次运行执行 18 项断言。
func Validate(payload Payload, options Options) Result {
	turns := payload.APITurns
	chatTurns := turnsOfKind(turns, "chat_completion")
	declarationTurns := turnsOfKind(turns, "formula_tools")
	fiberTurns := turnsOfKind(turns, "formula_fiber")
	ids := FiberIDs(turns)

	var searchActions []Step
	for _, step := range payload.Trace {
		if step.Type == "action" && step.Tool == "web_search" {
			searchActions = append(searchActions, step)
		}
	}

	answer := payload.Answer
	answerLower := strings.ToLower(answer)
	hosts := answerHosts(answer)

	checks := Checks{
		DirectMoonshotAPI: payload.Provider == "moonshot" &&
			strings.TrimRight(payload.BaseURL, "/") == "https://api.moonshot.cn/v1",

		ExactModel: payload.Model == "kimi-k3",

		OneRealFormulaDeclarationFetch: len(declarationTurns) == 1 &&
			declarationTurns[0].FormulaURI == FormulaURI &&
			declarationTurns[0].statusIs(200) &&
			!declarationTurns[0].hasError(),

		ProviderFormulaDeclaresStandardWebSearch: len(declarationTurns) == 1 &&
			contract.HasWebSearch(declarationTurns[0].Response.Tools),

		ProviderResponseEachChatTurn: len(ResponseIDs(turns)) == len(chatTurns) &&
			len(chatTurns) >= 3,

		FormulaToolDeclaredEachChatTurn: len(chatTurns) > 0 &&
			allTurns(chatTurns, func(turn Turn) bool {
				return contract.HasWebSearch(turn.Request.Tools)
			}),

		AllFibersSucceeded: len(fiberTurns) >= 2 && len(ids) == len(fiberTurns),

		MultipleDistinctFormulaFibers: len(ids) >= 2 && distinct(ids) >= 2,

		FiberRequestsMatchModelActions: len(fiberTurns) == len(searchActions) &&
			allTurns(fiberTurns, func(turn Turn) bool {
				return turn.FormulaURI == FormulaURI &&
					turn.Request.Body != nil &&
					turn.Request.Body.Name == "web_search" &&
					isJSONString(turn.Request.Body.Arguments)
			}),

		// 数的是搜索分布在几个不同迭代，而不是搜索总次数：
		// 「一轮并行扇出 N 次」不构成后续追查，必须判定失败。
		SequentialSearchRoundsObserved: distinctIterations(searchActions) >= 2,

		ReasoningObserved: anyStep(payload.Trace, "thought"),

		FinalAnswerObserved: anyStep(payload.Trace, "answer") &&
			!contract.IsFailureAnswer(answer),

		SourceLinksInAnswer: strings.Contains(answer, "http://") ||
			strings.Contains(answer, "https://"),

		OfficialSourcesInAnswer: hasOfficialSources(hosts),

		CurrentElevenMemberFact: containsAny(answerLower, "11", "十一") &&
			containsAny(answerLower, "timor-leste", "东帝汶"),

		TimorLesteAdmissionDate: strings.Contains(answerLower, "2025") &&
			containsAny(answerLower, "10月26", "10 月 26", "10-26", "october 26"),

		IndonesiaCapitalTransitionExplained: containsAny(answerLower, "jakarta", "雅加达") &&
			containsAny(answerLower, "nusantara", "努山塔拉") &&
			containsAny(answerLower, "presidential decree", "presidential decision", "总统令"),

		RetrievalDateReported: retrievalDateReported(answerLower, options.asOf()),
	}

	return Result{
		Checks:     checks,
		Passed:     checks.AllPassed(),
		FormulaURI: FormulaURI,
		FiberIDs:   ids,
		Usage:      SummarizeUsage(turns),
	}
}

// retrievalDateReported 检查模型是否如实复述了问题里给定的检索基准日期。
//
// 判定结构与 Python 版一致（年份 + 任意一种日期写法），只是把那一天从字面量
// 提成参数：换基准日期时不必再逐处改字符串。
func retrievalDateReported(answerLower string, asOf time.Time) bool {
	year, month, day := asOf.Date()
	dayMarkers := []string{
		fmt.Sprintf("%d月%d", int(month), day),
		fmt.Sprintf("%d 月 %d", int(month), day),
		asOf.Format("2006-01-02"),
	}
	return strings.Contains(answerLower, fmt.Sprintf("%d", year)) &&
		containsAny(answerLower, dayMarkers...)
}

// 认定为官方来源的域：国际组织与政府机构。
//
// Python 版用裸子串匹配白名单 ("go.id", "polri.go.id", "mkri.id")：polri.go.id
// 是 go.id 的冗余子串，而裸 go.id 会误命中 lego.idea.com 这类无关域名，最贴题的
// timor-leste.gov.tl 反而不在名单里。这里改为解析答案里的主机名后按域名后缀匹配。
var (
	internationalOrgDomains = []string{"asean.org"}
	governmentDomains       = []string{"go.id", "gov.tl", "mkri.id"}
)

func hasOfficialSources(hosts []string) bool {
	return hasDomain(hosts, internationalOrgDomains) && hasDomain(hosts, governmentDomains)
}

func hasDomain(hosts, domains []string) bool {
	for _, host := range hosts {
		for _, domain := range domains {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return true
			}
		}
	}
	return false
}

// urlPattern 从答案正文里抠出链接。结尾的中英文标点不属于 URL。
var urlPattern = regexp.MustCompile(`https?://[^\s<>"'()\[\]{}，。；、！？）（【】《》]+`)

// answerHosts 返回答案里全部链接的主机名（小写、去端口）。
func answerHosts(answer string) []string {
	var hosts []string
	for _, raw := range urlPattern.FindAllString(answer, -1) {
		raw = strings.TrimRight(raw, ".,;:")
		parsed, err := url.Parse(raw)
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func allTurns(turns []Turn, predicate func(Turn) bool) bool {
	for _, turn := range turns {
		if !predicate(turn) {
			return false
		}
	}
	return true
}

func anyStep(steps []Step, stepType string) bool {
	for _, step := range steps {
		if step.Type == stepType {
			return true
		}
	}
	return false
}

func distinct(values []string) int {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	return len(seen)
}

func distinctIterations(steps []Step) int {
	seen := make(map[int]struct{}, len(steps))
	for _, step := range steps {
		seen[step.Iteration] = struct{}{}
	}
	return len(seen)
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
