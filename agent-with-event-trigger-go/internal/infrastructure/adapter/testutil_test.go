package adapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func toJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(encoded)
}

func containsText(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
