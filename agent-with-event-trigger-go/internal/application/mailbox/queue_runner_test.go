package mailbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// fakeProvider 模拟 Unipile 的应答，并记录调用回执。
type fakeProvider struct {
	calls           []mailbox.CallReceipt
	calendarEvents  []mailbox.ProviderObject
	archivedAsInbox bool
}

func (p *fakeProvider) record(method, path string, status int) {
	code := status
	p.calls = append(p.calls, mailbox.CallReceipt{
		Method: method, Path: path, Status: &code, Success: status < 400,
		CredentialScheme: "X-API-KEY", ResponseSHA256: mailbox.SHA256String(path),
	})
}

func (p *fakeProvider) ListAccounts(context.Context) ([]mailbox.ProviderObject, error) {
	p.record("GET", "/api/v1/accounts", 200)
	return []mailbox.ProviderObject{{"id": "account-real", "email": "listener@example.com"}}, nil
}

func (p *fakeProvider) ListFolders(context.Context, string) ([]mailbox.ProviderObject, error) {
	p.record("GET", "/api/v1/folders", 200)
	return []mailbox.ProviderObject{{"id": "folder-archive", "name": "Archive", "role": "archive"}}, nil
}

func (p *fakeProvider) ListEmails(context.Context, string, time.Time, string, int) ([]mailbox.ProviderObject, error) {
	p.record("GET", "/api/v1/emails", 200)
	return nil, nil
}

func (p *fakeProvider) GetEmail(_ context.Context, emailID string) (mailbox.ProviderObject, error) {
	p.record("GET", "/api/v1/emails/"+emailID, 200)
	if p.archivedAsInbox {
		return mailbox.ProviderObject{"id": emailID, "role": "inbox", "folders": []any{"Inbox"}}, nil
	}
	return mailbox.ProviderObject{"id": emailID, "role": "archive", "folders": []any{"Archive"}}, nil
}

func (p *fakeProvider) UpdateEmailFolders(_ context.Context, emailID string, _ []string) (mailbox.ProviderObject, error) {
	p.record("PUT", "/api/v1/emails/"+emailID, 200)
	return mailbox.ProviderObject{"object": "EmailUpdated"}, nil
}

func (p *fakeProvider) SendEmail(context.Context, string, string, string, string) (mailbox.ProviderObject, error) {
	p.record("POST", "/api/v1/emails", 201)
	return mailbox.ProviderObject{"object": "EmailSent"}, nil
}

func (p *fakeProvider) ListCalendars(context.Context, string) ([]mailbox.ProviderObject, error) {
	p.record("GET", "/api/v1/calendars", 200)
	return []mailbox.ProviderObject{{"id": "calendar-real", "is_primary": true}}, nil
}

func (p *fakeProvider) ListCalendarEvents(context.Context, string, string, time.Time, time.Time) ([]mailbox.ProviderObject, error) {
	p.record("GET", "/api/v1/calendars/calendar-real/events", 200)
	return p.calendarEvents, nil
}

func (p *fakeProvider) Calls() []mailbox.CallReceipt { return p.calls }
func (p *fakeProvider) Close()                       {}

type silentLogger struct{}

func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}

func email(kind, date, suffix string) mailbox.ProviderObject {
	base := mailbox.ProviderObject{
		"id": "email-" + suffix, "account_id": "account-real",
		"date": date, "role": "inbox", "folders": []any{"Inbox"},
	}
	switch kind {
	case "meeting_invitation":
		base["subject"] = "Meeting invitation: design review"
		base["body_plain"] = "START_UTC: 2026-08-03T10:00:00.000Z\n" +
			"END_UTC: 2026-08-03T11:00:00.000Z"
	case "customer_complaint":
		base["subject"] = "Customer complaint: delayed order"
		base["body_plain"] = "Customer complaint for order #E44-TEST. Please escalate."
	default:
		base["subject"] = "Marketing newsletter"
		base["body_plain"] = "Marketing newsletter promotion. Click to unsubscribe."
	}
	return base
}

func newRunner(t *testing.T, provider port.MailboxPort) (*queueRunner, port.ArtifactPort) {
	t.Helper()
	store := newTempStore(t)
	return &queueRunner{
		client:     provider,
		artifact:   store,
		log:        silentLogger{},
		now:        func() time.Time { return time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC) },
		accountID:  "account-real",
		calendarID: "account-real",
	}, store
}

// 输入故意乱序，验证 FIFO 队列用的是 provider 时间而不是入参顺序。
func TestProcessQueueOrdersByProviderTimeAndRunsThreeWorkflows(t *testing.T) {
	provider := &fakeProvider{}
	runner, store := newRunner(t, provider)

	emails := sortedByProviderTime([]mailbox.ProviderObject{
		email("marketing", "2026-08-01T00:00:03.000Z", "marketing"),
		email("meeting_invitation", "2026-08-01T00:00:01.000Z", "meeting"),
		email("customer_complaint", "2026-08-01T00:00:02.000Z", "complaint"),
	})
	if err := runner.process(context.Background(), emails); err != nil {
		t.Fatalf("process 返回错误: %v", err)
	}

	want := []mailbox.Classification{
		mailbox.ClassMeetingInvitation, mailbox.ClassCustomerComplaint, mailbox.ClassMarketing,
	}
	for i, workflow := range runner.workflows {
		if workflow.Classification != want[i] {
			t.Errorf("第 %d 个工作流分类 = %q，期望 %q", i, workflow.Classification, want[i])
		}
	}

	acceptance := mailbox.DeriveAcceptance(mailbox.AcceptanceInput{
		Events:    runner.events,
		Workflows: runner.workflows,
		Calls:     provider.Calls(),
		SeedReceipts: []mailbox.ProviderObject{
			{"object": "EmailSent"}, {"object": "EmailSent"}, {"object": "EmailSent"},
		},
		CredentialSecret: "unit-secret",
		DSNSecret:        "api.example.invalid:12345",
	})
	if acceptance.Status != mailbox.StatusPassed {
		for _, gate := range acceptance.Gates {
			if !gate.Passed {
				t.Errorf("验收门 %s 未通过", gate.Name)
			}
		}
		t.Fatalf("验收状态 = %q，期望 passed", acceptance.Status)
	}

	assertFileLargerThan(t, store, "artifacts/meeting_reply_draft.txt", 100)
	assertFileExists(t, store, "artifacts/high_priority_notifications.jsonl")
}

// provider 侧归档没生效时必须判失败：只发 PUT 不回读的话这种情况看不出来。
func TestAcceptanceFailsWhenArchiveIsNotVerified(t *testing.T) {
	provider := &fakeProvider{archivedAsInbox: true}
	runner, _ := newRunner(t, provider)

	emails := sortedByProviderTime([]mailbox.ProviderObject{
		email("meeting_invitation", "2026-08-01T00:00:01.000Z", "meeting"),
		email("customer_complaint", "2026-08-01T00:00:02.000Z", "complaint"),
		email("marketing", "2026-08-01T00:00:03.000Z", "marketing"),
	})
	if err := runner.process(context.Background(), emails); err != nil {
		t.Fatal(err)
	}

	acceptance := mailbox.DeriveAcceptance(mailbox.AcceptanceInput{
		Events: runner.events, Workflows: runner.workflows, Calls: provider.Calls(),
		SeedReceipts:     []mailbox.ProviderObject{{}, {}, {}},
		CredentialSecret: "unit-secret", DSNSecret: "api.example.invalid:12345",
	})
	if acceptance.Status != mailbox.StatusFailed {
		t.Fatalf("验收状态 = %q，期望 failed", acceptance.Status)
	}
	if acceptance.GatesJSON["marketing_archived_and_verified_through_provider"] {
		t.Error("归档校验门不应通过")
	}
}

// 日历有冲突时草稿要改成婉拒，而不是照样接受。
func TestMeetingDraftDeclinesWhenCalendarConflicts(t *testing.T) {
	provider := &fakeProvider{calendarEvents: []mailbox.ProviderObject{{
		"id":    "existing",
		"start": map[string]any{"dateTime": "2026-08-03T10:30:00.000Z"},
		"end":   map[string]any{"dateTime": "2026-08-03T11:30:00.000Z"},
	}}}
	runner, store := newRunner(t, provider)

	if err := runner.process(context.Background(),
		[]mailbox.ProviderObject{email("meeting_invitation", "2026-08-01T00:00:01.000Z", "meeting")}); err != nil {
		t.Fatal(err)
	}

	workflow := runner.workflows[0]
	if workflow.CalendarCheck == nil || !workflow.CalendarCheck.Conflict {
		t.Fatalf("应检测到日历冲突，实际 = %+v", workflow.CalendarCheck)
	}
	if workflow.Draft.Disposition != "decline" {
		t.Errorf("草稿处置 = %q，冲突时应为 decline", workflow.Draft.Disposition)
	}

	draft := readArtifact(t, store, "artifacts/meeting_reply_draft.txt")
	if !strings.Contains(strings.ToLower(draft), "decline") {
		t.Errorf("草稿正文未体现婉拒:\n%s", draft)
	}
}

func TestComplaintNotificationIsHighPriorityAndHashed(t *testing.T) {
	provider := &fakeProvider{}
	runner, _ := newRunner(t, provider)

	if err := runner.process(context.Background(),
		[]mailbox.ProviderObject{email("customer_complaint", "2026-08-01T00:00:02.000Z", "complaint")}); err != nil {
		t.Fatal(err)
	}

	workflow := runner.workflows[0]
	if workflow.Extracted == nil || workflow.Extracted.OrderReference != "E44-TEST" {
		t.Fatalf("订单号抽取结果 = %+v", workflow.Extracted)
	}
	if workflow.Notification.Priority != "high" || !workflow.Notification.Delivered {
		t.Errorf("通知应为已投递的高优通知，实际 = %+v", workflow.Notification)
	}
	if len(workflow.Notification.FileSHA256) != 64 {
		t.Errorf("通知文件哈希长度 = %d，期望 64", len(workflow.Notification.FileSHA256))
	}
}
