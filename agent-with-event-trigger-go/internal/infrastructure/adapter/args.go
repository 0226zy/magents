package adapter

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/derr"
)

// requireString 取必填的字符串参数。
func requireString(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return "", fmt.Errorf("%w: 缺少参数 %s", derr.ErrInvalidToolArguments, key)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%w: 参数 %s 必须是字符串", derr.ErrInvalidToolArguments, key)
	}
	return value, nil
}

// optionalString 取可选的字符串参数。
func optionalString(args map[string]any, key string) (string, bool) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return "", false
	}
	value, ok := raw.(string)
	return value, ok
}

// optionalInt 取可选的整数参数。JSON 解出来的数字是 float64，两种都要接受。
func optionalInt(args map[string]any, key string) (int, bool) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case float64:
		return int(value), true
	case int:
		return value, true
	case int64:
		return int(value), true
	default:
		return 0, false
	}
}

// splitLinesKeepEnds 按行切分并保留行尾换行符，对应 Python 的 readlines()。
// 保留换行符才能让 strings.Join 还原出原始片段。
func splitLinesKeepEnds(content string) []string {
	if content == "" {
		return nil
	}
	var lines []string
	for len(content) > 0 {
		index := strings.IndexByte(content, '\n')
		if index < 0 {
			lines = append(lines, content)
			break
		}
		lines = append(lines, content[:index+1])
		content = content[index+1:]
	}
	return lines
}

func utf8Valid(data []byte) bool { return utf8.Valid(data) }
