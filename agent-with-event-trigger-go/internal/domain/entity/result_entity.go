package entity

// EventResult 是一次事件处理的结果。
//
// ToolCallsCount 与 ToolCallsTotal 分开：Python 版用累计值冒充单次值，
// 客户端看到的「本次调用了 17 个工具」其实是进程启动以来的总数。
type EventResult struct {
	EventID        string  `json:"event_id"`
	FinalAnswer    *string `json:"final_answer"`
	Iterations     int     `json:"iterations"`
	ToolCallsCount int     `json:"tool_calls_count"`
	ToolCallsTotal int     `json:"tool_calls_total"`
	TodoItems      int     `json:"todo_items"`
	Success        bool    `json:"success"`
	TrajectoryFile string  `json:"trajectory_file,omitempty"`
}

// AgentStatus 是 /agent/status 返回的运行时状态。
type AgentStatus struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	CurrentDirectory string `json:"current_directory"`
	ToolCallsCount   int    `json:"tool_calls_count"`
	TodoItems        int    `json:"todo_items"`
	HistoryMessages  int    `json:"history_messages"`
	Processes        int    `json:"background_processes"`
	MCPToolsLoaded   bool   `json:"mcp_tools_loaded"`
	MCPToolsCount    int    `json:"mcp_tools_count"`
}
