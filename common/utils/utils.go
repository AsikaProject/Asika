package utils

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GenerateUUID generates a new UUID string
func GenerateUUID() string {
	return uuid.New().String()
}

// FormatTime formats a time for display
func FormatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// ParseDuration parses a duration string with defaults
func ParseDuration(s string, defaultDuration time.Duration) time.Duration {
	if s == "" {
		return defaultDuration
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultDuration
	}
	return d
}

// SplitRepo splits a repo string into owner and repo
func SplitRepo(repo string) (string, string, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo format: %s (expected owner/repo)", repo)
	}
	return parts[0], parts[1], nil
}

// JoinRepo joins owner and repo into a repo string
func JoinRepo(owner, repo string) string {
	return owner + "/" + repo
}

// StringSliceContains checks if a string slice contains a value
func StringSliceContains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// UniqueStrings returns a slice with duplicates removed
func UniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, v := range input {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// TruncateString truncates a string to max length
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ParseBool parses a bool with a default value
func ParseBool(s string, defaultVal bool) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return defaultVal
	}
}
