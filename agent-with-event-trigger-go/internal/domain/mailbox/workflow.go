package mailbox

import (
	"fmt"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

// ChannelMailboxPoll 是实验声明的事件通道：轮询真实邮箱，而非本地文件或合成事件。
const ChannelMailboxPoll = "unipile_mailbox_poll"

// CallReceipt 是一次 provider API 调用的回执。
// 请求体已脱敏，响应只留摘要与长度——既能证明调用发生过，又不泄露内容。
type CallReceipt struct {
	Method           string  `json:"method"`
	Path             string  `json:"path"`
	Request          any     `json:"request"`
	CredentialScheme string  `json:"credential_scheme"`
	Status           *int    `json:"status"`
	Success          bool    `json:"success"`
	LatencySeconds   float64 `json:"latency_seconds"`
	ResponseSHA256   string  `json:"response_sha256,omitempty"`
	ResponseBytes    int     `json:"response_bytes,omitempty"`
	ResponseShape    any     `json:"response_shape,omitempty"`
	ErrorType        string  `json:"error_type,omitempty"`
	ErrorTitle       string  `json:"error_title,omitempty"`
}

// EventSource 标明事件来自哪个 provider 对象。
type EventSource struct {
	Type            string `json:"type"`
	Provider        string `json:"provider"`
	EmailIDSHA256   string `json:"email_id_sha256"`
	AccountIDSHA256 string `json:"account_id_sha256"`
}

// EventContent 是事件内容的脱敏摘要。
type EventContent struct {
	Subject        string `json:"subject"`
	BodySHA256     string `json:"body_sha256"`
	BodyCharacters int    `json:"body_characters"`
}

// EventContext 是事件的附加上下文。
type EventContext struct {
	ProviderDate   any            `json:"provider_date"`
	Role           any            `json:"role"`
	FoldersCount   int            `json:"folders_count"`
	Classification Classification `json:"classification,omitempty"`
}

// CanonicalEvent 是写入证据的事件记录。
type CanonicalEvent struct {
	Sequence              int          `json:"sequence"`
	Source                EventSource  `json:"source"`
	Channel               string       `json:"channel"`
	Content               EventContent `json:"content"`
	Context               EventContext `json:"context"`
	ProviderReceiptSHA256 string       `json:"provider_receipt_sha256"`
}

// NewCanonicalEvent 从 provider 邮件对象构造事件记录。
// 缺少 id / account_id 的对象不能作为「真实入站事件」的凭据，直接报错。
func NewCanonicalEvent(email ProviderObject, sequence int) (CanonicalEvent, error) {
	emailID := StringField(email, "id")
	accountID := StringField(email, "account_id")
	if emailID == "" || accountID == "" {
		return CanonicalEvent{}, fmt.Errorf("%w: provider 邮件对象缺少 id/account_id",
			derr.ErrAmbiguousClassification)
	}

	body := EmailText(email)
	folders, _ := email["folders"].([]any)

	return CanonicalEvent{
		Sequence: sequence,
		Source: EventSource{
			Type:            "email",
			Provider:        "unipile",
			EmailIDSHA256:   shortHash(emailID),
			AccountIDSHA256: shortHash(accountID),
		},
		Channel: ChannelMailboxPoll,
		Content: EventContent{
			Subject:        StringField(email, "subject"),
			BodySHA256:     SHA256String(body),
			BodyCharacters: runeLen(body),
		},
		Context: EventContext{
			ProviderDate: email["date"],
			Role:         email["role"],
			FoldersCount: len(folders),
		},
		ProviderReceiptSHA256: SHA256String(CanonicalJSONString(Redact(email))),
	}, nil
}

// CalendarCheck 记录一次真实日历冲突查询。
type CalendarCheck struct {
	Performed        bool   `json:"performed"`
	CalendarIDSHA256 string `json:"calendar_id_sha256"`
	EventsExamined   int    `json:"events_examined"`
	ConflictCount    int    `json:"conflict_count"`
	Conflict         bool   `json:"conflict"`
	Start            string `json:"start"`
	End              string `json:"end"`
}

// Draft 是生成的回复草稿。
type Draft struct {
	Disposition string `json:"disposition"` // accept | decline
	Path        string `json:"path"`
	Bytes       int    `json:"bytes"`
	SHA256      string `json:"sha256"`
}

// Extracted 是从投诉邮件里抽取的关键信息。
type Extracted struct {
	OrderReference        string `json:"order_reference"`
	RequiresHumanFollowUp bool   `json:"requires_human_follow_up"`
}

// Notification 是落盘的高优通知。
type Notification struct {
	CreatedAt      string `json:"created_at"`
	Priority       string `json:"priority"`
	Delivered      bool   `json:"delivered"`
	Channel        string `json:"channel"`
	Classification string `json:"classification"`
	Subject        string `json:"subject"`
	OrderReference string `json:"order_reference"`
	Summary        string `json:"summary"`
	Path           string `json:"path,omitempty"`
	FileSHA256     string `json:"file_sha256,omitempty"`
}

// Archive 记录营销邮件的归档与回读校验。
type Archive struct {
	UpdateObject            string `json:"update_object"`
	ArchiveFolderCandidates int    `json:"archive_folder_candidates"`
	Verified                bool   `json:"verified"`
	VerifiedRole            string `json:"verified_role"`
	VerifiedFoldersSHA256   string `json:"verified_folders_sha256"`
}

// Workflow 是一封邮件的处理结果。
//
// Python 用 dict 表示，验收时靠 .get("calendar_check", {}).get("performed") 链式取值——
// 键名拼错会静默变成 None，验收门悄悄判 false。这里改成强类型字段，拼错编译期就失败；
// JSON 键名保持一致，证据格式不变。
type Workflow struct {
	Sequence       int            `json:"sequence"`
	EmailIDSHA256  string         `json:"email_id_sha256"`
	Classification Classification `json:"classification"`
	CalendarCheck  *CalendarCheck `json:"calendar_check,omitempty"`
	Draft          *Draft         `json:"draft,omitempty"`
	Extracted      *Extracted     `json:"extracted,omitempty"`
	Notification   *Notification  `json:"notification,omitempty"`
	Archive        *Archive       `json:"archive,omitempty"`
}

// MeetingReplyBody 按是否冲突生成回复草稿正文。
func MeetingReplyBody(subject string, conflict bool) string {
	if subject == "" {
		subject = "Meeting invitation"
	}
	decision := "Thank you for the invitation. I checked the calendar and the proposed time is " +
		"available. I am happy to accept."
	if conflict {
		decision = "Thank you for the invitation. I have a calendar conflict during the proposed " +
			"time, so I must decline. Could we find another time?"
	}
	return fmt.Sprintf("Subject: Re: %s\n\n%s\n", subject, decision)
}

// Disposition 把冲突与否映射为回复态度。
func Disposition(conflict bool) string {
	if conflict {
		return "decline"
	}
	return "accept"
}

func runeLen(s string) int {
	count := 0
	for range s {
		count++
	}
	return count
}
