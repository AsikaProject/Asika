package pr

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

type BatchCherryPickResult struct {
	PRID        string `json:"pr_id"`
	Success     bool   `json:"success"`
	NewPRNumber int    `json:"new_pr_number,omitempty"`
	NewPRURL    string `json:"new_pr_url,omitempty"`
	Error       string `json:"error,omitempty"`
}

func BatchCherryPickPRs(prIDs []string, repoGroup string, targetBranch string) []BatchCherryPickResult {
	results := make([]BatchCherryPickResult, 0, len(prIDs))
	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		for _, prID := range prIDs {
			results = append(results, BatchCherryPickResult{
				PRID:    prID,
				Success: false,
				Error:   "repo group not found",
			})
		}
		return results
	}

	for _, prID := range prIDs {
		data, err := db.GetPRByIndex(prID, repoGroup, 0)
		if err != nil || data == nil {
			results = append(results, BatchCherryPickResult{
				PRID:    prID,
				Success: false,
				Error:   fmt.Sprintf("PR not found: %v", err),
			})
			continue
		}

		var pr models.PRRecord
		if err := json.Unmarshal(data, &pr); err != nil {
			results = append(results, BatchCherryPickResult{
				PRID:    prID,
				Success: false,
				Error:   "failed to parse PR",
			})
			continue
		}

		results = append(results, BatchCherryPickResult{
			PRID:    prID,
			Success: false,
			Error:   "cherry-pick not yet implemented",
		})
		slog.Warn("batch cherry-pick not implemented", "pr_id", prID, "target", targetBranch)
	}

	return results
}
