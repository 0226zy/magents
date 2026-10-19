package mailbox

import "strings"

// 实验的三种终态。
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusBlocked = "blocked"
)

// Gate 是一项验收门。
type Gate struct {
	Name   string
	Passed bool
}

// Acceptance 是验收结论。Gates 保留切片形式以固定输出顺序，
// GatesJSON 是落盘用的 map。
type Acceptance struct {
	Status    string
	Gates     []Gate
	GatesJSON map[string]bool
}

// AcceptanceInput 汇总验收所需的全部材料。
type AcceptanceInput struct {
	Events           []CanonicalEvent
	Workflows        []Workflow
	Calls            []CallReceipt
	SeedReceipts     []ProviderObject
	CredentialSecret string
	DSNSecret        string
}

// DeriveAcceptance 计算 10 项验收门。名称与判定逐字沿用 Python 版，
// 以便两种实现对同一次运行给出相同结论。
func DeriveAcceptance(in AcceptanceInput) Acceptance {
	byClass := map[Classification]Workflow{}
	for _, workflow := range in.Workflows {
		byClass[workflow.Classification] = workflow
	}
	meeting := byClass[ClassMeetingInvitation]
	complaint := byClass[ClassCustomerComplaint]
	marketing := byClass[ClassMarketing]

	encoded := CanonicalJSONString(map[string]any{
		"events": in.Events, "workflows": in.Workflows, "calls": in.Calls,
	})
	controlPlane := CanonicalJSONString(stripResponseDigests(in.Calls))

	gates := []Gate{
		{"three_real_inbound_unipile_events", threeRealInboundEvents(in.Events)},
		{"fifo_event_queue", fifoOrdered(in.Events, in.Workflows)},
		{"exact_three_scenario_classifications",
			len(byClass) == 3 && len(in.Workflows) == 3 &&
				hasAll(byClass, ClassMeetingInvitation, ClassCustomerComplaint, ClassMarketing)},
		{"meeting_calendar_checked_and_reply_drafted", meetingGate(meeting)},
		{"complaint_extracted_and_high_priority_notification_delivered", complaintGate(complaint)},
		{"marketing_archived_and_verified_through_provider",
			marketing.Archive != nil && marketing.Archive.UpdateObject == "EmailUpdated" &&
				marketing.Archive.Verified},
		{"required_unipile_calls_succeeded", allCallsSucceeded(in.Calls)},
		{"three_seed_messages_sent_through_unipile", len(in.SeedReceipts) == 3},
		{"credentials_and_dsn_absent_from_receipts",
			secretAbsent(encoded, in.CredentialSecret) && secretAbsent(encoded, in.DSNSecret)},
		{"no_simulation_markers_in_control_plane", !ContainsSimulationMarker(controlPlane)},
	}

	status := StatusPassed
	asMap := make(map[string]bool, len(gates))
	for _, gate := range gates {
		asMap[gate.Name] = gate.Passed
		if !gate.Passed {
			status = StatusFailed
		}
	}
	return Acceptance{Status: status, Gates: gates, GatesJSON: asMap}
}

// BlockedAcceptance 是凭据缺失或鉴权失败时的结论：实验没有跑，而不是跑失败了。
func BlockedAcceptance(status string) Acceptance {
	gates := []Gate{
		{"valid_unipile_credentials", false},
		{"real_mailbox_campaign_completed", false},
	}
	asMap := map[string]bool{}
	for _, gate := range gates {
		asMap[gate.Name] = gate.Passed
	}
	return Acceptance{Status: status, Gates: gates, GatesJSON: asMap}
}

func threeRealInboundEvents(events []CanonicalEvent) bool {
	if len(events) != 3 {
		return false
	}
	for _, event := range events {
		if event.Source.Provider != "unipile" || event.Channel != ChannelMailboxPoll ||
			event.ProviderReceiptSHA256 == "" {
			return false
		}
	}
	return true
}

func fifoOrdered(events []CanonicalEvent, workflows []Workflow) bool {
	if len(events) != 3 || len(workflows) != 3 {
		return false
	}
	for i := range events {
		if events[i].Sequence != i || workflows[i].Sequence != i {
			return false
		}
	}
	return true
}

func meetingGate(w Workflow) bool {
	if w.CalendarCheck == nil || w.Draft == nil {
		return false
	}
	return w.CalendarCheck.Performed &&
		(w.Draft.Disposition == "accept" || w.Draft.Disposition == "decline") &&
		w.Draft.Bytes > 100 && len(w.Draft.SHA256) == 64
}

func complaintGate(w Workflow) bool {
	if w.Extracted == nil || w.Notification == nil {
		return false
	}
	return w.Extracted.RequiresHumanFollowUp && w.Extracted.OrderReference != "" &&
		w.Notification.Priority == "high" && w.Notification.Delivered &&
		len(w.Notification.FileSHA256) == 64
}

func allCallsSucceeded(calls []CallReceipt) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !call.Success {
			return false
		}
	}
	return true
}

// stripResponseDigests 去掉响应摘要后再做模拟痕迹扫描：
// 摘要是 64 位十六进制，不可能命中关键词，留着只会拖慢扫描。
func stripResponseDigests(calls []CallReceipt) []CallReceipt {
	out := make([]CallReceipt, len(calls))
	copy(out, calls)
	for i := range out {
		out[i].ResponseSHA256 = ""
	}
	return out
}

// secretAbsent 报告密钥是否确实没出现在证据里。空密钥视为「无可泄露」。
func secretAbsent(encoded, secret string) bool {
	if secret == "" {
		return true
	}
	return !strings.Contains(encoded, secret)
}

func hasAll(byClass map[Classification]Workflow, wanted ...Classification) bool {
	for _, class := range wanted {
		if _, ok := byClass[class]; !ok {
			return false
		}
	}
	return true
}
