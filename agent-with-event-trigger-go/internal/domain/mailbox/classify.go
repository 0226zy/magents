// Package mailbox 存放实验 6-1 的纯规则：时间解析、邮件分类、日历冲突判定、
// 脱敏、规范 JSON 与验收门。全部是无 IO 的纯函数，因此既能校验实时运行，
// 也能对归档证据离线复算。
package mailbox

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

// ProviderObject 是 provider 返回的原始对象。
//
// 形状不受我们控制，因此保留 map 而不解码进 struct：解码会静默丢掉未声明的字段，
// 而证据的完整性恰恰依赖「provider 说了什么就记什么」。
type ProviderObject = map[string]any

// Classification 是三种实验场景之一。
type Classification string

const (
	ClassMeetingInvitation Classification = "meeting_invitation"
	ClassCustomerComplaint Classification = "customer_complaint"
	ClassMarketing         Classification = "marketing"
)

var (
	orderPattern   = regexp.MustCompile(`(?i)order\s*#([a-z0-9-]+)`)
	startUTCRegexp = regexp.MustCompile(`(?i)START_UTC:\s*(\S+)`)
	endUTCRegexp   = regexp.MustCompile(`(?i)END_UTC:\s*(\S+)`)
)

// EmailText 取邮件正文：依次尝试 body_plain、body、text，返回第一个非空白值。
func EmailText(email ProviderObject) string {
	for _, key := range []string{"body_plain", "body", "text"} {
		if value, ok := email[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// StringField 取字符串字段，缺失或类型不符时返回空串。
func StringField(obj ProviderObject, key string) string {
	switch value := obj[key].(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

// Classify 判定邮件属于哪个场景。
//
// 命中数必须恰好为 1：一封同时像会议邀请又像营销推广的邮件，会让后续工作流的
// 归属变得可争辩，因此直接判为歧义而不是择一处理。
func Classify(email ProviderObject) (Classification, error) {
	text := strings.ToLower(StringField(email, "subject") + "\n" + EmailText(email))

	var matches []Classification
	if strings.Contains(text, "meeting invitation") &&
		strings.Contains(text, "start_utc:") && strings.Contains(text, "end_utc:") {
		matches = append(matches, ClassMeetingInvitation)
	}
	if strings.Contains(text, "customer complaint") && orderPattern.MatchString(text) {
		matches = append(matches, ClassCustomerComplaint)
	}
	if strings.Contains(text, "marketing") &&
		(strings.Contains(text, "unsubscribe") || strings.Contains(text, "newsletter")) {
		matches = append(matches, ClassMarketing)
	}

	if len(matches) != 1 {
		return "", fmt.Errorf("%w: %v", derr.ErrAmbiguousClassification, matches)
	}
	return matches[0], nil
}

// OrderReference 抽取投诉邮件里的订单号。
func OrderReference(email ProviderObject) (string, bool) {
	match := orderPattern.FindStringSubmatch(EmailText(email))
	if match == nil {
		return "", false
	}
	return match[1], true
}

// MeetingInterval 解析会议邮件声明的时间区间，要求 end 严格晚于 start。
func MeetingInterval(email ProviderObject) (start, end time.Time, err error) {
	text := EmailText(email)
	startMatch := startUTCRegexp.FindStringSubmatch(text)
	endMatch := endUTCRegexp.FindStringSubmatch(text)
	if startMatch == nil || endMatch == nil {
		return time.Time{}, time.Time{}, derr.ErrInvalidMeetingInterval
	}
	start, okStart := ParseProviderTime(startMatch[1])
	end, okEnd := ParseProviderTime(endMatch[1])
	if !okStart || !okEnd || !end.After(start) {
		return time.Time{}, time.Time{}, derr.ErrInvalidMeetingInterval
	}
	return start, end, nil
}

// providerTimeLayouts 覆盖 provider 可能返回的几种 ISO-8601 变体。
var providerTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// ParseProviderTime 解析 provider 的时间值。
//
// 接受字符串，或 {date_time|dateTime|datetime|date} 四选一的对象；末尾 Z 归一为
// +00:00；没有时区信息时按 UTC 处理。解析失败返回 ok=false，由调用方决定跳过还是报错。
func ParseProviderTime(value any) (time.Time, bool) {
	if nested, ok := value.(map[string]any); ok {
		for _, key := range []string{"date_time", "dateTime", "datetime", "date"} {
			if inner, exists := nested[key]; exists && inner != nil {
				return ParseProviderTime(inner)
			}
		}
		return time.Time{}, false
	}
	if value == nil {
		return time.Time{}, false
	}

	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return time.Time{}, false
	}
	text = strings.Replace(text, "Z", "+00:00", 1)

	for _, layout := range providerTimeLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// EventOverlaps 判断日历事件是否与 [start, end) 冲突。
// 已取消的事件不算冲突——它不占用时间。
func EventOverlaps(event ProviderObject, start, end time.Time) bool {
	if cancelled, ok := event["is_cancelled"].(bool); ok && cancelled {
		return false
	}
	if strings.EqualFold(StringField(event, "status"), "cancelled") {
		return false
	}

	eventStart, okStart := firstTime(event, "start", "start_at")
	eventEnd, okEnd := firstTime(event, "end", "end_at")
	if !okStart || !okEnd {
		return false
	}
	return eventStart.Before(end) && eventEnd.After(start)
}

func firstTime(obj ProviderObject, keys ...string) (time.Time, bool) {
	for _, key := range keys {
		if value, ok := obj[key]; ok && value != nil {
			if parsed, parsedOK := ParseProviderTime(value); parsedOK {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

// ProviderOrder 是 FIFO 队列的排序谓词：先按 provider 时间，再按 id 打破平局。
// 时间缺失的邮件排在最前，与 Python 用 datetime.min 兜底的行为一致。
func ProviderOrder(a, b ProviderObject) bool {
	timeA, _ := ParseProviderTime(a["date"])
	timeB, _ := ParseProviderTime(b["date"])
	if !timeA.Equal(timeB) {
		return timeA.Before(timeB)
	}
	return StringField(a, "id") < StringField(b, "id")
}

// IsoMillis 按 provider 要求的毫秒精度 UTC 格式渲染时间。
func IsoMillis(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000") + "Z"
}
