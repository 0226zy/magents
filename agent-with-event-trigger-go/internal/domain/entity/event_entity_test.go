package entity

import (
	"strings"
	"testing"
	"time"
)

func TestToUserMessageRendersPerEventType(t *testing.T) {
	cases := []struct {
		name     string
		event    Event
		contains []string
	}{
		{
			"邮件带发件人与主题",
			NewEvent(EventEmailReply, "也帮我看看磁盘占用",
				map[string]any{"from": "bob@example.com", "subject": "Re: System Report"},
				fixedTime()),
			[]string{"bob@example.com", "Re: System Report", "也帮我看看磁盘占用"},
		},
		{
			"PR 更新带编号与动作",
			NewEvent(EventGitHubPRUpdate, "请补单测",
				map[string]any{"pr_number": "42", "action": "review_requested"},
				fixedTime()),
			[]string{"#42", "review_requested", "请补单测"},
		},
		{
			"定时器带 ID",
			NewEvent(EventTimerTrigger, "检查备份",
				map[string]any{"timer_id": "daily_backup_check"}, fixedTime()),
			[]string{"daily_backup_check", "检查备份"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			message := testCase.event.ToUserMessage()
			for _, fragment := range testCase.contains {
				if !strings.Contains(message, fragment) {
					t.Errorf("渲染结果缺少 %q:\n%s", fragment, message)
				}
			}
		})
	}
}

// 元数据缺失时要退回占位符，不能渲染出 "From: <nil>"。
func TestToUserMessageFallsBackWhenMetadataMissing(t *testing.T) {
	event := NewEvent(EventEmailReply, "正文", nil, fixedTime())
	message := event.ToUserMessage()

	if strings.Contains(message, "<nil>") || strings.Contains(message, "map[") {
		t.Errorf("缺失元数据渲染出了 Go 内部表示:\n%s", message)
	}
	if !strings.Contains(message, "正文") {
		t.Errorf("正文丢失:\n%s", message)
	}
}

// 只有人直接参与的事件才算用户交互；定时器与超时提醒不算，
// 否则超时监控会被自己产生的提醒事件不断重置，永远不再触发。
func TestIsUserInteraction(t *testing.T) {
	interactive := []EventType{EventWebMessage, EventIMMessage, EventEmailReply}
	for _, eventType := range interactive {
		if !eventType.IsUserInteraction() {
			t.Errorf("%s 应算作用户交互", eventType)
		}
	}
	passive := []EventType{EventTimerTrigger, EventUserTimeout, EventProcessTimeout,
		EventSystemAlert, EventFileChange}
	for _, eventType := range passive {
		if eventType.IsUserInteraction() {
			t.Errorf("%s 不应算作用户交互", eventType)
		}
	}
}

func TestParseEventTypeRejectsUnknown(t *testing.T) {
	if _, err := ParseEventType("web_message"); err != nil {
		t.Fatalf("合法类型被拒: %v", err)
	}
	if _, err := ParseEventType("telepathy"); err == nil {
		t.Fatal("未知事件类型应报错")
	}
}

// 同一毫秒内产生的事件也必须拿到不同 ID，否则轨迹与客户端无法区分它们。
func TestNewEventGeneratesDistinctIDsWithinSameMillisecond(t *testing.T) {
	now := time.Now()
	first := NewEvent(EventWebMessage, "一", nil, now)
	second := NewEvent(EventWebMessage, "二", nil, now)
	if first.ID == "" || first.ID == second.ID {
		t.Errorf("事件 ID 应非空且互不相同: %q / %q", first.ID, second.ID)
	}
}
