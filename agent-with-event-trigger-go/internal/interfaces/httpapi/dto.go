package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// EventRequest 是 POST /event 的请求体。
type EventRequest struct {
	EventType string         `json:"event_type"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata"`
}

// Validate 把请求体转成领域事件。未知事件类型是客户端错误，不是服务端错误。
func (r EventRequest) Validate() (entity.Event, error) {
	eventType, err := entity.ParseEventType(r.EventType)
	if err != nil {
		return entity.Event{}, err
	}
	if strings.TrimSpace(r.Content) == "" {
		return entity.Event{}, errors.New("content 不能为空")
	}
	// 走 NewEvent 而不是直接构造：ID 与时间戳要在这里生成，
	// 否则响应里的 event_id 会是空串，客户端无法把请求关联到轨迹。
	return entity.NewEvent(eventType, r.Content, r.Metadata, time.Now()), nil
}

// ProcessRequest 是进程注册/注销的请求体。
//
// name 与 process_name 都接受：Python 的服务端声明 name，客户端却发 process_name，
// 两端对不上，注册必然 422。这里两种拼写都认。
type ProcessRequest struct {
	ProcessID   string         `json:"process_id"`
	Name        string         `json:"name"`
	ProcessName string         `json:"process_name"`
	Metadata    map[string]any `json:"metadata"`
}

// ResolvedName 返回最终使用的进程名。
func (r ProcessRequest) ResolvedName() string {
	if r.Name != "" {
		return r.Name
	}
	if r.ProcessName != "" {
		return r.ProcessName
	}
	return r.ProcessID
}

// Validate 校验必填项。
func (r ProcessRequest) Validate() error {
	if strings.TrimSpace(r.ProcessID) == "" {
		return errors.New("process_id 不能为空")
	}
	return nil
}

// MonitoringConfigRequest 是 POST /monitoring/start 的可选阈值覆盖（单位秒）。
type MonitoringConfigRequest struct {
	IntervalSeconds       *float64 `json:"interval_seconds"`
	UserTimeoutSeconds    *float64 `json:"user_timeout_seconds"`
	ProcessTimeoutSeconds *float64 `json:"process_timeout_seconds"`
}

// decodeJSON 解析请求体，空体视为空对象（允许 POST /agent/reset 这类无体请求）。
func decodeJSON(raw []byte, target any) error {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	return decoder.Decode(target)
}
