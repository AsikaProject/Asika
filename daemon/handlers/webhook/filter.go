package webhook

import (
	"regexp"
	"strings"
)

type EventFilter struct {
	EventTypes     []string `json:"event_types,omitempty" toml:"event_types,omitempty"`
	BranchPatterns []string `json:"branch_patterns,omitempty" toml:"branch_patterns,omitempty"`
	IgnoreBots     bool     `json:"ignore_bots,omitempty" toml:"ignore_bots,omitempty"`
}

var botUsernames = []string{"dependabot", "renovate", "github-actions", "gitlab-bot"}

// ShouldProcessEvent checks if an event should be processed based on filters
func ShouldProcessEvent(event string, branch string, author string, filters *EventFilter) bool {
	if filters == nil {
		return true
	}

	// Check if event type is filtered
	if len(filters.EventTypes) > 0 {
		matched := false
		for _, et := range filters.EventTypes {
			if et == event {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check if bot should be ignored
	if filters.IgnoreBots && isBot(author) {
		return false
	}

	// Check branch patterns
	if len(filters.BranchPatterns) > 0 && branch != "" {
		matched := false
		for _, pattern := range filters.BranchPatterns {
			if matchBranchPattern(pattern, branch) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func isBot(username string) bool {
	lower := strings.ToLower(username)
	for _, bot := range botUsernames {
		if strings.Contains(lower, bot) {
			return true
		}
	}
	return strings.HasSuffix(lower, "[bot]")
}

func matchBranchPattern(pattern, branch string) bool {
	if pattern == "*" {
		return true
	}
	if strings.Contains(pattern, "*") {
		pattern = strings.ReplaceAll(pattern, "*", ".*")
		re, err := regexp.Compile("^" + pattern + "$")
		if err == nil && re.MatchString(branch) {
			return true
		}
	}
	return pattern == branch
}

// AddWebhookFilterToConfig adds filter config to models
func init() {
	// This is just a placeholder to show how filters would be used
}
