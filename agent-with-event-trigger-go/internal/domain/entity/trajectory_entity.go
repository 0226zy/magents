package entity

// TrajectorySnapshot 是一轮循环结束时的完整状态快照。
//
// 顶层键名与 Python 轨迹文件逐字一致，这样同一个脚本能同时比对两种实现的运行轨迹。
type TrajectorySnapshot struct {
	Timestamp           string                       `json:"timestamp"`
	Iteration           int                          `json:"iteration"`
	Provider            string                       `json:"provider"`
	Model               string                       `json:"model"`
	ConversationHistory []Message                    `json:"conversation_history"`
	ToolCalls           []ToolCallRecord             `json:"tool_calls"`
	TodoList            []TodoItem                   `json:"todo_list"`
	CurrentDirectory    string                       `json:"current_directory"`
	FinalAnswer         *string                      `json:"final_answer"`
	BackgroundProcesses map[string]BackgroundProcess `json:"background_processes"`
	Config              SystemHintConfig             `json:"config"`
}
