package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	appmailbox "github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/shared/deps"
)

// ExperimentOptions 是实验 6-1 的命令行参数。
type ExperimentOptions struct {
	CampaignID        string
	PreflightOnly     bool
	Recipient         string
	ListenAccountID   string
	SenderAccountID   string
	CalendarAccountID string
	PollTimeout       time.Duration
	PollInterval      time.Duration
	ValidationRoot    string
	ProtocolPath      string
}

// ParseExperimentOptions 解析实验参数。
func ParseExperimentOptions(args []string) (ExperimentOptions, error) {
	var opts ExperimentOptions
	var pollTimeout, pollInterval float64

	fs := flag.NewFlagSet("experiment61", flag.ContinueOnError)
	fs.StringVar(&opts.CampaignID, "campaign-id", "", "运行标识，留空按 UTC 时间生成")
	fs.BoolVar(&opts.PreflightOnly, "preflight-only", false, "只验证凭据与账号，不投递邮件")
	fs.StringVar(&opts.Recipient, "recipient", "", "监听邮箱地址，留空时从账号推断")
	fs.StringVar(&opts.ListenAccountID, "listen-account-id", "", "监听账号 ID")
	fs.StringVar(&opts.SenderAccountID, "sender-account-id", "", "发信账号 ID，默认同监听账号")
	fs.StringVar(&opts.CalendarAccountID, "calendar-account-id", "", "日历账号 ID，默认同监听账号")
	fs.Float64Var(&pollTimeout, "poll-timeout", 180, "轮询收件箱的超时（秒）")
	fs.Float64Var(&pollInterval, "poll-interval", 5, "轮询收件箱的间隔（秒）")
	fs.StringVar(&opts.ValidationRoot, "validation-root", "validation", "证据输出根目录")
	fs.StringVar(&opts.ProtocolPath, "protocol", "experiment_protocol.json", "实验协议文件路径")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	opts.PollTimeout = seconds(pollTimeout)
	opts.PollInterval = seconds(pollInterval)
	if opts.CampaignID == "" {
		opts.CampaignID = time.Now().UTC().Format("20060102T150405Z")
	}
	return opts, nil
}

// RunExperiment 执行实验 6-1，返回证据目录。
//
// 无论实验通过、失败还是因凭据缺失被阻塞，都会写出完整证据目录：
// 失败的运行同样是证据，什么都不写才是最糟的结果。
func RunExperiment(ctx context.Context, opts ExperimentOptions, out io.Writer) (string, error) {
	log := deps.NewLogger(out)
	runRoot := filepath.Join(opts.ValidationRoot, opts.CampaignID)

	runtime, err := deps.BuildExperiment(runRoot, log)
	if err != nil {
		return "", err
	}
	defer runtime.Close()

	evidence := runtime.Evidence
	protocol, schemaReceipts := loadProtocol(opts.ProtocolPath)
	if protocol != nil {
		if _, err := evidence.Write("protocol.json", protocol); err != nil {
			return evidence.Root(), err
		}
	}

	summary := runtime.Service.Run(ctx, appmailbox.RunInput{
		CampaignID:         opts.CampaignID,
		Recipient:          opts.Recipient,
		ListenAccountID:    opts.ListenAccountID,
		SenderAccountID:    opts.SenderAccountID,
		CalendarAccountID:  opts.CalendarAccountID,
		PollTimeout:        opts.PollTimeout,
		PollInterval:       opts.PollInterval,
		PreflightOnly:      opts.PreflightOnly,
		CredentialSecret:   runtime.Token,
		DSNSecret:          runtime.DSN,
		SchemaSourceStatus: schemaReceipts,
		Protocol:           protocol,
	})

	if _, err := evidence.Write("summary.json", summary); err != nil {
		return evidence.Root(), err
	}
	manifest, err := buildManifest(evidence, summary)
	if err != nil {
		return evidence.Root(), err
	}
	manifestRef, err := evidence.Write("manifest.json", manifest)
	if err != nil {
		return evidence.Root(), err
	}

	if err := writeLatest(opts, summary, manifestRef); err != nil {
		return evidence.Root(), err
	}

	fmt.Fprintf(out, "\n实验 6-1 结束：status=%s，证据目录 %s\n", summary.Status, evidence.Root())
	return evidence.Root(), nil
}

// writeLatest 在根目录留一个指针文件，指向本次运行的清单及其哈希。
func writeLatest(opts ExperimentOptions, summary appmailbox.Summary, manifest port.ArtifactRef) error {
	latest, err := deps.NewEvidenceStore(opts.ValidationRoot)
	if err != nil {
		return err
	}
	_, err = latest.Write("latest.json", map[string]any{
		"experiment":        "6-1",
		"campaign_id":       opts.CampaignID,
		"status":            summary.Status,
		"official_complete": summary.OfficialComplete,
		"manifest":          filepath.ToSlash(filepath.Join(opts.CampaignID, "manifest.json")),
		"manifest_sha256":   manifest.SHA256,
	})
	return err
}

func buildManifest(evidence port.EvidencePort, summary appmailbox.Summary) (map[string]any, error) {
	// 排除 manifest.json 自身：它的哈希不可能包含自己。
	files, err := evidence.Manifest("manifest.json")
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(files))
	for _, file := range files {
		rows = append(rows, map[string]any{
			"path": file.Path, "bytes": file.Bytes, "sha256": file.SHA256,
		})
	}
	return map[string]any{
		"experiment":        "6-1",
		"campaign_id":       summary.CampaignID,
		"generated_at":      time.Now().UTC().Format(time.RFC3339),
		"status":            summary.Status,
		"official_complete": summary.OfficialComplete,
		"files":             rows,
	}, nil
}

// loadProtocol 读取协议文件，并为其中声明的官方文档来源生成回执。
func loadProtocol(path string) (any, []map[string]any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var protocol any
	if json.Unmarshal(raw, &protocol) != nil {
		return nil, nil
	}

	object, _ := protocol.(map[string]any)
	sources, _ := object["official_schema_sources"].([]any)
	receipts := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		// 协议里的来源可能写成一个 URL 字符串，也可能写成带 name 的对象，两种都接受。
		entry := map[string]any{}
		switch typed := source.(type) {
		case string:
			entry["url"] = typed
		case map[string]any:
			entry = typed
		default:
			continue
		}
		receipt := map[string]any{
			"url":           entry["url"],
			"recorded_hash": mailbox.SHA256String(mailbox.CanonicalJSONString(entry)),
		}
		if name, ok := entry["name"]; ok {
			receipt["name"] = name
		}
		receipts = append(receipts, receipt)
	}
	return protocol, receipts
}
