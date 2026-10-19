package mailbox

import (
	"errors"
	"testing"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

func meetingEmail() ProviderObject {
	return ProviderObject{
		"id": "email-meeting", "account_id": "account-real",
		"date": "2026-08-01T00:00:01.000Z", "role": "inbox",
		"folders": []any{"Inbox"},
		"subject": "Meeting invitation: design review",
		"body_plain": "START_UTC: 2026-08-03T10:00:00.000Z\n" +
			"END_UTC: 2026-08-03T11:00:00.000Z",
	}
}

func TestClassifyIsUniqueAndMeetingIntervalIsExact(t *testing.T) {
	email := meetingEmail()

	classification, err := Classify(email)
	if err != nil {
		t.Fatalf("Classify 返回错误: %v", err)
	}
	if classification != ClassMeetingInvitation {
		t.Fatalf("分类 = %q，期望 %q", classification, ClassMeetingInvitation)
	}

	start, end, err := MeetingInterval(email)
	if err != nil {
		t.Fatalf("MeetingInterval 返回错误: %v", err)
	}
	if got := start.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-08-03T10:00:00Z" {
		t.Errorf("会议开始时间 = %s，期望 2026-08-03T10:00:00Z", got)
	}
	if seconds := end.Sub(start).Seconds(); seconds != 3600 {
		t.Errorf("会议时长 = %.0fs，期望 3600s", seconds)
	}
}

// 同时命中两种场景时必须报错而不是任选其一：
// 选错了会让一封投诉邮件被当成营销邮件直接归档。
func TestClassifyRejectsAmbiguousEmail(t *testing.T) {
	email := meetingEmail()
	email["subject"] = "Meeting invitation and marketing newsletter"
	email["body_plain"] = email["body_plain"].(string) + "\nMarketing unsubscribe"

	if _, err := Classify(email); !errors.Is(err, derr.ErrAmbiguousClassification) {
		t.Fatalf("错误 = %v，期望 ErrAmbiguousClassification", err)
	}
}

func TestProviderOrderUsesProviderTimeThenID(t *testing.T) {
	earlier := ProviderObject{"id": "b", "date": "2026-08-01T00:00:01.000Z"}
	later := ProviderObject{"id": "a", "date": "2026-08-01T00:00:03.000Z"}

	if !ProviderOrder(earlier, later) {
		t.Error("应按 provider 时间排序，早到的在前")
	}
	if ProviderOrder(later, earlier) {
		t.Error("排序不对称")
	}

	// 时间相同时用 id 兜底，保证顺序确定。
	first := ProviderObject{"id": "a", "date": "2026-08-01T00:00:01.000Z"}
	second := ProviderObject{"id": "b", "date": "2026-08-01T00:00:01.000Z"}
	if !ProviderOrder(first, second) {
		t.Error("时间相同时应按 id 排序")
	}
}
