package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/0226zy/magents/web-search-agent-go/internal/acceptance"
	"github.com/0226zy/magents/web-search-agent-go/internal/application/session"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/config"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/evidence"
	"github.com/0226zy/magents/web-search-agent-go/internal/shared/deps"
)

// questionTemplate 的检索基准日期由 -as-of 决定：这套事实断言与那一天绑定。
const questionTemplate = `截至 %d 年 %d 月 %d 日，请核查东盟成员资格和印度尼西亚首都的最新状态。
请自主完成研究：先搜索东盟成员国的官方来源，确认当前成员数量、成员名单及东帝汶正式入盟日期；
检查第一轮证据还缺什么，然后至少再执行一次不同的后续搜索，核实雅加达与努山塔拉的当前法律地位以及总统令是否已生效。
最后给出结构化结论、检索日期和可点击的权威来源链接。不要依赖记忆作答。`

const (
	experimentID    = "1-2"
	schemaVersion   = "2.0"
	canonicalSource = "book/chapter1.md#实验-1-2-kimi-k3-原生-agent-能力"
	maxIterations   = 8
)

// document 是写入磁盘的证据文档。
//
// 字段顺序即 JSON 键顺序；全部用 struct 而非 map，这样键序稳定、SHA-256 可复现。
type document struct {
	SchemaVersion           string              `json:"schema_version"`
	ExperimentID            string              `json:"experiment_id"`
	EvidenceMode            string              `json:"evidence_mode"`
	CreatedAt               string              `json:"created_at"`
	CanonicalSource         string              `json:"canonical_source"`
	AsOf                    string              `json:"as_of"`
	CredentialSourceEnv     string              `json:"credential_source_env"`
	CredentialValueRecorded bool                `json:"credential_value_recorded"`
	Host                    evidence.Host       `json:"host"`
	Repository              evidence.Repository `json:"repository"`
	TransientFailedAttempts []failedAttempt     `json:"transient_failed_attempts"`
	Run                     session.Payload     `json:"run"`
	Acceptance              acceptance.Result   `json:"acceptance"`
}

// failedAttempt 如实记录一次失败的尝试，连完整 API 轨迹一起保留。
type failedAttempt struct {
	Attempt    int               `json:"attempt"`
	Answer     string            `json:"answer"`
	Validation acceptance.Result `json:"validation"`
	Run        session.Payload   `json:"run"`
}

// RunExperiment12 是 experiment12 命令的转发入口：
// 通过 Kimi K3 的官方 Formula web-search 工具运行实验 1-2，
// 并把完整的 provider 凭据写成可复核的证据文件。
func RunExperiment12(args []string, stdout, stderr io.Writer) (int, error) {
	var (
		model     string
		attempts  int
		timeout   time.Duration
		outputDir string
		asOfText  string
	)
	flags := flag.NewFlagSet("experiment12", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&model, "model", config.DefaultModel, "模型名称（实验 1-2 只接受 kimi-k3）")
	flags.IntVar(&attempts, "attempts", 3, "最多尝试次数：provider 偶发过载时整轮重跑")
	flags.DurationVar(&timeout, "timeout", config.DefaultSearchTimeout, "单次 provider 调用的超时预算")
	flags.StringVar(&outputDir, "output-dir", "", "证据输出目录（默认 validation/real_<时间戳>）")
	flags.StringVar(&asOfText, "as-of", acceptance.DefaultAsOf.Format("2006-01-02"),
		"检索基准日期：问题文案与事实断言都与它绑定")
	if err := flags.Parse(args); err != nil {
		return 2, err
	}
	if model != "kimi-k3" {
		return 2, errors.New("实验 1-2 要求精确使用 kimi-k3 模型")
	}
	asOf, err := time.Parse("2006-01-02", asOfText)
	if err != nil {
		return 2, fmt.Errorf("-as-of 需要 YYYY-MM-DD 格式: %w", err)
	}

	backend, err := config.Resolve("", "", model, false)
	if err != nil {
		return 2, err
	}

	ctx := context.Background()
	options := acceptance.Options{AsOf: asOf}
	question := formatQuestion(asOf)

	var (
		run      session.Payload
		failures = []failedAttempt{}
	)
	for attempt := 1; attempt <= attempts; attempt++ {
		candidate := runOnce(ctx, backend, question, timeout, stdout)
		validation := acceptance.Validate(toAcceptancePayload(candidate), options)
		if validation.Passed {
			run = candidate
			break
		}
		// Kimi K3 偶尔会在一轮工具调用后就停下，Formula Fiber 也可能瞬时过载。
		// 两者都是诚实的失败尝试：整轮重跑，并把每次失败的完整轨迹留在证据里。
		failures = append(failures, failedAttempt{
			Attempt:    attempt,
			Answer:     candidate.Answer,
			Validation: validation,
			Run:        candidate,
		})
		fmt.Fprintf(stderr, "⚠️  第 %d 次尝试未通过验收，未过项: %v\n", attempt, validation.FailedChecks())
		run = candidate
		if attempt == attempts {
			break
		}
		time.Sleep(time.Duration(1<<attempt) * time.Second)
	}

	now := time.Now().UTC()
	if outputDir == "" {
		outputDir = filepath.Join("validation", "real_"+evidence.Timestamp(now))
	}
	result := acceptance.Validate(toAcceptancePayload(run), options)
	doc := document{
		SchemaVersion:           schemaVersion,
		ExperimentID:            experimentID,
		EvidenceMode:            "real_api",
		CreatedAt:               now.Format(time.RFC3339Nano),
		CanonicalSource:         canonicalSource,
		AsOf:                    asOf.Format("2006-01-02"),
		CredentialSourceEnv:     config.CredentialSourceEnv(),
		Host:                    evidence.CurrentHost(),
		Repository:              evidence.CurrentRepository(ctx),
		TransientFailedAttempts: failures,
		Run:                     run,
		Acceptance:              result,
	}

	// credential_value_recorded 由实际扫描推导，而不是写死成 false。
	serialized, err := evidence.Marshal(doc)
	if err != nil {
		return 1, err
	}
	doc.CredentialValueRecorded = evidence.CredentialValueRecorded(string(serialized))

	evidencePath := filepath.Join(outputDir, "evidence.json")
	digest, err := evidence.WriteWithChecksum(evidencePath, doc)
	if err != nil {
		return 1, err
	}
	if _, err := evidence.Write(filepath.Join("validation", "latest.json"), doc); err != nil {
		return 1, err
	}

	summary, err := evidence.Marshal(result)
	if err != nil {
		return 1, err
	}
	fmt.Fprint(stdout, string(summary))
	fmt.Fprintf(stdout, "Evidence: %s\nSHA-256: %s\n", evidencePath, digest)

	if !result.Passed {
		return 1, nil
	}
	return 0, nil
}

// formatQuestion 把检索基准日期填进问题文案。
func formatQuestion(asOf time.Time) string {
	return fmt.Sprintf(questionTemplate, asOf.Year(), int(asOf.Month()), asOf.Day())
}

func runOnce(ctx context.Context, backend entity.Backend, question string, timeout time.Duration, stdout io.Writer) session.Payload {
	// 每个问题开一个新的 Session：声明的缓存作用域是「一个问题」，
	// 验收断言 one_real_formula_declaration_fetch 要求恰好一次真实拉取。
	agentSession := deps.NewSession(backend, session.Options{
		Timeout: timeout,
		Verbose: true,
		Output:  stdout,
	})
	payload, _ := agentSession.Ask(ctx, question, maxIterations)
	return payload
}

// toAcceptancePayload 把运行载荷转成验收层的只读视图。
//
// 中转一次 JSON 是刻意的：验收层读的就是归档证据的形状，
// 这样实时校验与事后回放走的是同一条解析路径。
func toAcceptancePayload(payload session.Payload) acceptance.Payload {
	serialized, err := evidence.Marshal(payload)
	if err != nil {
		return acceptance.Payload{}
	}
	var view acceptance.Payload
	if err := json.Unmarshal(serialized, &view); err != nil {
		return acceptance.Payload{}
	}
	return view
}
