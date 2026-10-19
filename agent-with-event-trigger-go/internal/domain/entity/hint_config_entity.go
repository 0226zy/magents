package entity

// DefaultTimestampFormat 对应 Python 的 "%Y-%m-%d %H:%M:%S"。
const DefaultTimestampFormat = "2006-01-02 15:04:05"

// SystemHintConfig 控制注入给模型的系统提示内容。
// JSON 键名与 Python 轨迹文件里的 config 段对齐，便于两种实现的轨迹互相比对。
type SystemHintConfig struct {
	EnableTimestamps     bool `json:"enable_timestamps"`
	EnableToolCounter    bool `json:"enable_tool_counter"`
	EnableTodoList       bool `json:"enable_todo_list"`
	EnableDetailedErrors bool `json:"enable_detailed_errors"`
	EnableSystemState    bool `json:"enable_system_state"`
	// TimestampFormat 是 Go 的时间 layout。Python 存的是 strftime 格式串，
	// 两者语义等价但字符串不同，这是唯一无法逐字对齐的字段。
	TimestampFormat   string `json:"timestamp_format"`
	SimulateTimeDelay bool   `json:"simulate_time_delay"`
	SaveTrajectory    bool   `json:"save_trajectory"`
	TrajectoryFile    string `json:"trajectory_file"`
}

// DefaultSystemHintConfig 返回全部提示特性开启的默认配置。
func DefaultSystemHintConfig() SystemHintConfig {
	return SystemHintConfig{
		EnableTimestamps:     true,
		EnableToolCounter:    true,
		EnableTodoList:       true,
		EnableDetailedErrors: true,
		EnableSystemState:    true,
		TimestampFormat:      DefaultTimestampFormat,
		SimulateTimeDelay:    false,
		SaveTrajectory:       true,
		TrajectoryFile:       "event_agent_trajectory.json",
	}
}

// Layout 返回时间格式，未配置时回落到默认值。
func (c SystemHintConfig) Layout() string {
	if c.TimestampFormat == "" {
		return DefaultTimestampFormat
	}
	return c.TimestampFormat
}
