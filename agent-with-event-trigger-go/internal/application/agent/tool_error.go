package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// describeToolError 渲染工具错误。开启 detailed errors 时附上参数与修复建议——
// 这是本章的核心机制之一：模型靠错误文本自我纠正，信息越具体纠正得越快。
func (s *EventAgentService) describeToolError(err error, toolName string, args map[string]any) string {
	if !s.hints.EnableDetailedErrors {
		return err.Error()
	}

	parts := []string{fmt.Sprintf("Tool '%s' failed with %T: %v", toolName, err, err)}
	if encoded, marshalErr := json.MarshalIndent(args, "", "  "); marshalErr == nil {
		parts = append(parts, "Arguments: "+string(encoded))
	}
	if suggestions := errorSuggestions(err); suggestions != "" {
		parts = append(parts, "Suggestions: "+suggestions)
	}
	return strings.Join(parts, "\n")
}

// errorSuggestions 按错误文本的关键词给出修复建议，与 Python 的建议表一致。
func errorSuggestions(err error) string {
	text := strings.ToLower(err.Error())

	var suggestions []string
	switch {
	case strings.Contains(text, "permission"):
		suggestions = []string{
			"Check file/directory permissions",
			"Try using a different directory or running with appropriate permissions",
		}
	case strings.Contains(text, "not found"), strings.Contains(text, "no such file"):
		suggestions = []string{
			"Verify the file/directory path exists",
			"Check the current working directory",
			"Use absolute paths or create the file/directory first",
		}
	case strings.Contains(text, "syntax"):
		suggestions = []string{
			"Check the code syntax",
			"Ensure proper indentation and valid syntax",
		}
	case strings.Contains(text, "timed out"), strings.Contains(text, "timeout"):
		suggestions = []string{
			"The operation took too long",
			"Try with simpler input or break into smaller steps",
		}
	case strings.Contains(text, "import"):
		suggestions = []string{
			"Required module not available in the restricted environment",
			"Use only standard library modules",
		}
	}
	return strings.Join(suggestions, " | ")
}
