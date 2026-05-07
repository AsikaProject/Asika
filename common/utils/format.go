package utils

import (
	"encoding/json"
	"fmt"
)

// ToFloat64 converts an interface value to float64.
func ToFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

// FormatHours formats a duration in hours to a human-readable string.
func FormatHours(hours float64) string {
	if hours < 1 {
		return fmt.Sprintf("%.0f min", hours*60)
	}
	if hours < 24 {
		return fmt.Sprintf("%.1f hours", hours)
	}
	days := hours / 24
	if days < 30 {
		return fmt.Sprintf("%.1f days", days)
	}
	return fmt.Sprintf("%.0f days", days)
}
