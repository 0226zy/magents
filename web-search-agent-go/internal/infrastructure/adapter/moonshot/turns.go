package moonshot

import (
	"strings"
	"time"
)

func elapsedSeconds(started time.Time) float64 {
	return round6(time.Since(started).Seconds())
}

// round6 对齐 Python 的 round(..., 6)。
func round6(v float64) float64 {
	const factor = 1e6
	rounded := float64(int64(v*factor + copySign(0.5, v)))
	return rounded / factor
}

func copySign(magnitude, sign float64) float64 {
	if sign < 0 {
		return -magnitude
	}
	return magnitude
}

func statusPtr(code int) *int { return &code }

func normalizeModel(model string) string {
	return strings.ReplaceAll(strings.ToLower(model), "/", "-")
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
