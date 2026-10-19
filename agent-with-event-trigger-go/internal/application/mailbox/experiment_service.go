// Package mailbox 编排实验 6-1：向真实邮箱投递三封带标记的邮件，轮询收件箱，
// 按 FIFO 逐封执行场景工作流，最后产出脱敏证据与验收结论。
//
// 判定规则全部来自 domain/mailbox 的纯函数；这里只负责顺序、IO 与证据组装。
package mailbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// RunInput 是一次实验运行的参数。
type RunInput struct {
	CampaignID         string
	Recipient          string
	ListenAccountID    string
	SenderAccountID    string
	CalendarAccountID  string
	PollTimeout        time.Duration
	PollInterval       time.Duration
	PreflightOnly      bool
	CredentialSecret   string
	DSNSecret          string
	SchemaSourceStatus []map[string]any
	Protocol           any
}

// Summary 是写进 summary.json 的实验结论。
type Summary struct {
	Experiment            string                   `json:"experiment"`
	CampaignID            string                   `json:"campaign_id"`
	GeneratedAt           string                   `json:"generated_at"`
	Provider              string                   `json:"provider"`
	Status                string                   `json:"status"`
	OfficialComplete      bool                     `json:"official_complete"`
	Listener              string                   `json:"listener,omitempty"`
	OfficialSchemaReceipt []map[string]any         `json:"official_schema_receipts,omitempty"`
	AccountCount          int                      `json:"account_count,omitempty"`
	AccountReceipts       any                      `json:"account_receipts,omitempty"`
	SeedReceipts          any                      `json:"seed_receipts,omitempty"`
	Events                []mailbox.CanonicalEvent `json:"events,omitempty"`
	Workflows             []mailbox.Workflow       `json:"workflows,omitempty"`
	APICalls              []mailbox.CallReceipt    `json:"api_calls"`
	Acceptance            map[string]any           `json:"acceptance"`
	Blocker               map[string]any           `json:"blocker_or_error,omitempty"`
	CredentialsPresent    map[string]bool          `json:"credentials_present,omitempty"`
}

// ExperimentService 编排实验 6-1。
type ExperimentService struct {
	client   port.MailboxPort
	artifact port.ArtifactPort
	log      port.Logger
	now      func() time.Time
}

// NewExperimentService 构造编排服务。
func NewExperimentService(client port.MailboxPort, artifact port.ArtifactPort,
	log port.Logger, now func() time.Time) *ExperimentService {

	if now == nil {
		now = time.Now
	}
	return &ExperimentService{client: client, artifact: artifact, log: log, now: now}
}

// Run 执行实验，无论成功失败都返回可落盘的 Summary。
//
// 失败的运行同样是证据：凭据无效要留下 blocked 回执，而不是什么都不写。
func (s *ExperimentService) Run(ctx context.Context, in RunInput) Summary {
	summary := Summary{
		Experiment:            "6-1",
		CampaignID:            in.CampaignID,
		GeneratedAt:           s.now().UTC().Format(time.RFC3339),
		Provider:              "unipile",
		OfficialSchemaReceipt: in.SchemaSourceStatus,
	}

	err := s.run(ctx, in, &summary)
	summary.APICalls = s.client.Calls()
	if summary.APICalls == nil {
		// 落 [] 而不是 null：证据的消费方可以直接数长度，不必先判空。
		summary.APICalls = []mailbox.CallReceipt{}
	}

	if err == nil {
		return summary
	}

	status := mailbox.StatusFailed
	if isBlocked(err, in) {
		status = mailbox.StatusBlocked
	}
	acceptance := mailbox.BlockedAcceptance(status)
	summary.Status = status
	summary.OfficialComplete = false
	summary.Acceptance = map[string]any{"status": acceptance.Status, "gates": acceptance.GatesJSON}
	summary.Blocker = map[string]any{"type": fmt.Sprintf("%T", err), "message": err.Error()}
	summary.CredentialsPresent = map[string]bool{
		"UNIPILE_DSN":          in.DSNSecret != "",
		"UNIPILE_ACCESS_TOKEN": in.CredentialSecret != "",
	}
	s.log.Error("实验 6-1 结束于 %s: %v", status, err)
	return summary
}

func (s *ExperimentService) run(ctx context.Context, in RunInput, summary *Summary) error {
	if in.CredentialSecret == "" || in.DSNSecret == "" {
		return derr.ErrMissingCredentials
	}

	accounts, err := s.client.ListAccounts(ctx)
	if err != nil {
		return err
	}

	if in.PreflightOnly {
		summary.Status = "preflight_passed"
		summary.AccountCount = len(accounts)
		summary.AccountReceipts = mailbox.Redact(toAnySlice(accounts))
		summary.Acceptance = map[string]any{
			"status": "preflight_passed",
			"gates":  map[string]bool{"valid_unipile_credentials": true},
		}
		return nil
	}

	listen, err := chooseAccount(accounts, in.ListenAccountID)
	if err != nil {
		return err
	}
	sender := listen
	if in.SenderAccountID != "" {
		if sender, err = chooseAccount(accounts, in.SenderAccountID); err != nil {
			return err
		}
	}
	calendar := listen
	if in.CalendarAccountID != "" {
		if calendar, err = chooseAccount(accounts, in.CalendarAccountID); err != nil {
			return err
		}
	}

	recipient := in.Recipient
	if recipient == "" {
		recipient = accountEmail(listen)
	}
	if recipient == "" {
		return errors.New("无法推断监听邮箱地址，请用 -recipient 指定")
	}

	listenID := mailbox.StringField(listen, "id")
	senderID := mailbox.StringField(sender, "id")
	calendarID := mailbox.StringField(calendar, "id")

	startedAt := s.now().UTC().Add(-time.Minute)
	seeds, err := s.seedMessages(ctx, senderID, recipient, in.CampaignID)
	if err != nil {
		return err
	}

	emails, err := s.pollCampaignEmails(ctx, listenID, in.CampaignID, startedAt,
		in.PollTimeout, in.PollInterval)
	if err != nil {
		return err
	}

	runner := &queueRunner{
		client:     s.client,
		artifact:   s.artifact,
		log:        s.log,
		now:        s.now,
		accountID:  listenID,
		calendarID: calendarID,
	}
	if err := runner.process(ctx, emails); err != nil {
		return err
	}

	acceptance := mailbox.DeriveAcceptance(mailbox.AcceptanceInput{
		Events:           runner.events,
		Workflows:        runner.workflows,
		Calls:            s.client.Calls(),
		SeedReceipts:     seeds,
		CredentialSecret: in.CredentialSecret,
		DSNSecret:        in.DSNSecret,
	})

	summary.Status = acceptance.Status
	summary.OfficialComplete = acceptance.Status == mailbox.StatusPassed
	summary.Listener = mailbox.ChannelMailboxPoll
	summary.AccountReceipts = map[string]any{
		"listener": mailbox.Redact(listen),
		"sender":   mailbox.Redact(sender),
		"calendar": mailbox.Redact(calendar),
	}
	summary.SeedReceipts = mailbox.Redact(toAnySlice(seeds))
	summary.Events = runner.events
	summary.Workflows = runner.workflows
	summary.Acceptance = map[string]any{"status": acceptance.Status, "gates": acceptance.GatesJSON}
	return nil
}

// isBlocked 区分「实验没跑起来」与「实验跑了但没通过」。
// 凭据缺失或 401 属于前者，不能当作实验失败的证据。
func isBlocked(err error, in RunInput) bool {
	if errors.Is(err, derr.ErrMissingCredentials) {
		return true
	}
	if in.CredentialSecret == "" || in.DSNSecret == "" {
		return true
	}
	var apiErr *mailbox.APIError
	return errors.As(err, &apiErr) && apiErr.Status == 401
}

func toAnySlice(items []mailbox.ProviderObject) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}
