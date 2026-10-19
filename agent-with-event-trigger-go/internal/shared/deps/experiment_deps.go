package deps

import (
	"context"
	"os"
	"time"

	appmailbox "github.com/0226zy/magents/agent-with-event-trigger-go/internal/application/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/adapter"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/config"
)

// ExperimentRuntime 是实验 6-1 装配出来的运行时。
type ExperimentRuntime struct {
	Service  *appmailbox.ExperimentService
	Evidence port.EvidencePort
	// DSN 与 Token 原样回传，验收要用它们扫描证据里是否残留凭据。
	DSN   string
	Token string

	close func()
}

// Close 释放 HTTP 连接。
func (r *ExperimentRuntime) Close() {
	if r.close != nil {
		r.close()
	}
}

// BuildExperiment 装配实验 6-1 的运行时。
// 缺少 Unipile 凭据时装上离线占位实现，让实验走到 blocked 终态并留下证据。
func BuildExperiment(root string, log port.Logger) (*ExperimentRuntime, error) {
	config.LoadDotEnv()

	store, err := adapter.NewEvidenceStore(root)
	if err != nil {
		return nil, err
	}

	runtime := &ExperimentRuntime{
		Evidence: store,
		DSN:      os.Getenv("UNIPILE_DSN"),
		Token:    os.Getenv("UNIPILE_ACCESS_TOKEN"),
		close:    func() {},
	}

	var client port.MailboxPort = offlineMailbox{}
	if runtime.DSN != "" && runtime.Token != "" {
		unipile, err := adapter.NewUnipileClient(adapter.UnipileConfig{
			DSN: runtime.DSN, AccessToken: runtime.Token,
		})
		if err != nil {
			return nil, err
		}
		client = unipile
		runtime.close = unipile.Close
	}

	runtime.Service = appmailbox.NewExperimentService(client, store, log, time.Now)
	return runtime, nil
}

// NewEvidenceStore 供接口层写 latest.json 这类运行目录之外的汇总文件。
func NewEvidenceStore(root string) (port.EvidencePort, error) {
	return adapter.NewEvidenceStore(root)
}

// offlineMailbox 在缺少凭据时顶位：每个方法都直接返回 ErrOffline，
// 实验因此停在 blocked 而不是崩溃退出。
type offlineMailbox struct{}

func (offlineMailbox) ListAccounts(context.Context) ([]mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) ListFolders(context.Context, string) ([]mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) ListEmails(context.Context, string, time.Time, string, int) ([]mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) GetEmail(context.Context, string) (mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) UpdateEmailFolders(context.Context, string, []string) (mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) SendEmail(context.Context, string, string, string, string) (mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) ListCalendars(context.Context, string) ([]mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) ListCalendarEvents(context.Context, string, string, time.Time, time.Time) ([]mailbox.ProviderObject, error) {
	return nil, mailbox.ErrOffline
}
func (offlineMailbox) Calls() []mailbox.CallReceipt { return nil }
func (offlineMailbox) Close()                       {}
