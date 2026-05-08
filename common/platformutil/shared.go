package platformutil

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"asika/common/db"
	"asika/common/models"
)

// GetPRByID finds a PR by repo group and ID or number.
func GetPRByID(repoGroup, idOrNumber string) (*models.PRRecord, error) {
	var found *models.PRRecord
	prNumber, _ := strconv.Atoi(idOrNumber)

	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup {
			if pr.ID == idOrNumber || (prNumber > 0 && pr.PRNumber == prNumber) {
				found = &pr
			}
		}
		return nil
	})

	if found == nil {
		return nil, fmt.Errorf("PR not found")
	}
	return found, nil
}

// Truncate truncates a string to the specified length, appending "..." if truncated.
// The maxLen parameter is the total max length including the "..." suffix.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// InactivityDays returns the number of days since the given time.
func InactivityDays(lastActive time.Time) int {
	dur := time.Since(lastActive)
	return int(dur.Hours() / 24)
}

// HasLabelStr checks if the labels slice contains the target label.
// If target is empty, defaultLabel is used instead.
func HasLabelStr(labels []string, target, defaultLabel string) bool {
	check := target
	if check == "" {
		check = defaultLabel
	}
	for _, l := range labels {
		if l == check {
			return true
		}
	}
	return false
}

// ParseInt parses a string to int, returning 0 on failure.
func ParseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
