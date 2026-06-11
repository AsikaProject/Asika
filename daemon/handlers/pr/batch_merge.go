package pr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

type BatchMergeResult struct {
	PRID    string `json:"pr_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func BatchMergePRs(prIDs []string, repoGroup string, method string) []BatchMergeResult {
	results := make([]BatchMergeResult, 0, len(prIDs))
	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		for _, prID := range prIDs {
			results = append(results, BatchMergeResult{
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
			results = append(results, BatchMergeResult{
				PRID:    prID,
				Success: false,
				Error:   fmt.Sprintf("PR not found: %v", err),
			})
			continue
		}

		var pr models.PRRecord
		if err := json.Unmarshal(data, &pr); err != nil {
			results = append(results, BatchMergeResult{
				PRID:    prID,
				Success: false,
				Error:   "failed to parse PR",
			})
			continue
		}

		platform := pr.Platform
		if platform == "" {
			platform = config.GetPlatformForGroup(group)
		}
		client := GetClientForGroup(group, platform)
		if client == nil {
			results = append(results, BatchMergeResult{
				PRID:    prID,
				Success: false,
				Error:   "client not available",
			})
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		owner, repo := config.GetOwnerRepoFromGroup(group, platform)
		mergeMethod := method
		if mergeMethod == "" {
			mergeMethod = "merge"
		}

		err = client.MergePR(ctx, owner, repo, pr.PRNumber, mergeMethod)
		cancel()

		if err != nil {
			results = append(results, BatchMergeResult{
				PRID:    prID,
				Success: false,
				Error:   err.Error(),
			})
			slog.Error("batch merge failed", "pr_id", prID, "error", err)
		} else {
			results = append(results, BatchMergeResult{
				PRID:    prID,
				Success: true,
			})
			slog.Info("batch merge succeeded", "pr_id", prID)
		}
	}

	return results
}
