package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/port"
)

// queueRunner 按 FIFO 逐封处理邮件，是实验「三场景工作流」的执行体。
type queueRunner struct {
	client     port.MailboxPort
	artifact   port.ArtifactPort
	log        port.Logger
	now        func() time.Time
	accountID  string
	calendarID string

	events    []mailbox.CanonicalEvent
	workflows []mailbox.Workflow
}

// process 按队列顺序处理邮件。调用方已按 ProviderOrder 排好序。
func (r *queueRunner) process(ctx context.Context, emails []mailbox.ProviderObject) error {
	for _, email := range emails {
		sequence := len(r.events)

		event, err := mailbox.NewCanonicalEvent(email, sequence)
		if err != nil {
			return err
		}
		classification, err := mailbox.Classify(email)
		if err != nil {
			return err
		}
		event.Context.Classification = classification
		r.events = append(r.events, event)

		workflow, err := r.runWorkflow(ctx, email, classification)
		if err != nil {
			return err
		}
		workflow.Sequence = sequence
		workflow.EmailIDSHA256 = event.Source.EmailIDSHA256
		workflow.Classification = classification
		r.workflows = append(r.workflows, workflow)

		r.log.Info("已处理第 %d 封邮件（%s）", sequence, classification)
	}
	return nil
}

func (r *queueRunner) runWorkflow(ctx context.Context, email mailbox.ProviderObject,
	classification mailbox.Classification) (mailbox.Workflow, error) {

	switch classification {
	case mailbox.ClassMeetingInvitation:
		return r.handleMeeting(ctx, email)
	case mailbox.ClassCustomerComplaint:
		return r.handleComplaint(email)
	default:
		return r.handleMarketing(ctx, email)
	}
}

// handleMeeting 查真实日历判断冲突，并据此写出接受或婉拒的回复草稿。
func (r *queueRunner) handleMeeting(ctx context.Context, email mailbox.ProviderObject) (mailbox.Workflow, error) {
	start, end, err := mailbox.MeetingInterval(email)
	if err != nil {
		return mailbox.Workflow{}, err
	}

	calendars, err := r.client.ListCalendars(ctx, r.calendarID)
	if err != nil {
		return mailbox.Workflow{}, err
	}
	if len(calendars) == 0 {
		return mailbox.Workflow{}, fmt.Errorf("日历冲突检查未返回任何日历")
	}

	calendar := calendars[0]
	for _, candidate := range calendars {
		if isTrue(candidate["is_primary"]) || isTrue(candidate["is_default"]) {
			calendar = candidate
			break
		}
	}
	calendarID := mailbox.StringField(calendar, "id")
	if calendarID == "" {
		return mailbox.Workflow{}, fmt.Errorf("选中的日历缺少 id")
	}

	events, err := r.client.ListCalendarEvents(ctx, r.calendarID, calendarID, start, end)
	if err != nil {
		return mailbox.Workflow{}, err
	}

	conflicts := 0
	for _, event := range events {
		if mailbox.EventOverlaps(event, start, end) {
			conflicts++
		}
	}
	conflict := conflicts > 0

	draft := mailbox.MeetingReplyBody(mailbox.StringField(email, "subject"), conflict)
	ref, err := r.artifact.WriteFile("artifacts/meeting_reply_draft.txt", []byte(draft))
	if err != nil {
		return mailbox.Workflow{}, err
	}

	return mailbox.Workflow{
		CalendarCheck: &mailbox.CalendarCheck{
			Performed:        true,
			CalendarIDSHA256: mailbox.SHA256String(calendarID)[:20],
			EventsExamined:   len(events),
			ConflictCount:    conflicts,
			Conflict:         conflict,
			Start:            mailbox.IsoMillis(start),
			End:              mailbox.IsoMillis(end),
		},
		Draft: &mailbox.Draft{
			Disposition: mailbox.Disposition(conflict),
			Path:        ref.Path,
			Bytes:       ref.Bytes,
			SHA256:      ref.SHA256,
		},
	}, nil
}

// handleComplaint 抽出订单号并投递一条高优通知。
// 通知必须 fsync 落盘：进程被杀时「已提醒人工跟进」这件事不能只存在于内存里。
func (r *queueRunner) handleComplaint(email mailbox.ProviderObject) (mailbox.Workflow, error) {
	order, ok := mailbox.OrderReference(email)
	if !ok {
		return mailbox.Workflow{}, fmt.Errorf("投诉邮件缺少订单号")
	}

	notification := mailbox.Notification{
		CreatedAt:      r.now().UTC().Format(time.RFC3339),
		Priority:       "high",
		Delivered:      true,
		Channel:        "durable_console_and_jsonl",
		Classification: string(mailbox.ClassCustomerComplaint),
		Subject:        mailbox.StringField(email, "subject"),
		OrderReference: order,
		Summary:        "Customer reports an unresolved delayed order and requests human follow-up.",
	}

	line, err := json.Marshal(notification)
	if err != nil {
		return mailbox.Workflow{}, err
	}
	ref, err := r.artifact.AppendLineDurable("artifacts/high_priority_notifications.jsonl", line)
	if err != nil {
		return mailbox.Workflow{}, err
	}

	r.log.Warn("HIGH PRIORITY: customer complaint for order #%s", order)

	notification.Path = ref.Path
	notification.FileSHA256 = ref.SHA256
	return mailbox.Workflow{
		Extracted: &mailbox.Extracted{
			OrderReference:        order,
			RequiresHumanFollowUp: true,
		},
		Notification: &notification,
	}, nil
}

// handleMarketing 把营销邮件归档，再回读校验归档确实生效。
// 只发 PUT 不回读的话，provider 悄悄忽略请求我们也看不出来。
func (r *queueRunner) handleMarketing(ctx context.Context, email mailbox.ProviderObject) (mailbox.Workflow, error) {
	folders, err := r.client.ListFolders(ctx, r.accountID)
	if err != nil {
		return mailbox.Workflow{}, err
	}
	candidates := 0
	for _, folder := range folders {
		if strings.Contains(strings.ToLower(mailbox.StringField(folder, "role")), "archive") ||
			strings.Contains(strings.ToLower(mailbox.StringField(folder, "name")), "archive") {
			candidates++
		}
	}

	emailID := mailbox.StringField(email, "id")
	update, err := r.client.UpdateEmailFolders(ctx, emailID, []string{"archive"})
	if err != nil {
		return mailbox.Workflow{}, err
	}
	verified, err := r.client.GetEmail(ctx, emailID)
	if err != nil {
		return mailbox.Workflow{}, err
	}

	role := strings.ToLower(mailbox.StringField(verified, "role"))
	folderText := folderNames(verified["folders"])
	archived := role == "archive" || strings.Contains(folderText, "archive") ||
		(role != "inbox" && !strings.Contains(folderText, "inbox"))

	return mailbox.Workflow{
		Archive: &mailbox.Archive{
			UpdateObject:            mailbox.StringField(update, "object"),
			ArchiveFolderCandidates: candidates,
			Verified:                archived,
			VerifiedRole:            role,
			VerifiedFoldersSHA256:   mailbox.SHA256String(folderText),
		},
	}, nil
}

// folderNames 把 folders 字段拼成小写空格分隔的文本，供归档判定使用。
func folderNames(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, strings.ToLower(fmt.Sprint(item)))
	}
	return strings.Join(parts, " ")
}

func isTrue(value any) bool {
	flag, ok := value.(bool)
	return ok && flag
}
