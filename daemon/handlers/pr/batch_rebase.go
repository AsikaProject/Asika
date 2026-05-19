package pr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/gitutil"
	"asika/common/models"
)

// BatchRebasePRRequest represents the request body for batch rebase
type BatchRebasePRRequest struct {
	PRIDs []string `json:"pr_ids" binding:"required"`
}

// BatchRebaseResult represents the result of a single rebase operation
type BatchRebaseResult struct {
	PRID    string `json:"pr_id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// BatchRebasePR handles POST /api/v1/repos/:repo_group/prs/batch/rebase
func BatchRebasePR(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	var req BatchRebasePRRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: pr_ids is required"})
		return
	}

	if len(req.PRIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr_ids is empty"})
		return
	}

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	results := make([]BatchRebaseResult, 0, len(req.PRIDs))
	for _, prID := range req.PRIDs {
		result := batchRebaseSinglePR(c, group, repoGroup, prID, cfg)
		results = append(results, result)
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

func batchRebaseSinglePR(c *gin.Context, group *models.RepoGroup, repoGroup, prID string, cfg *models.Config) BatchRebaseResult {
	// Find PR
	data, err := db.GetPRByIndex(prID, "", 0)
	if err != nil || data == nil {
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: "PR not found",
		}
	}

	var pr models.PRRecord
	if err := json.Unmarshal(data, &pr); err != nil {
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: "failed to parse PR",
		}
	}

	if pr.State != "open" {
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: fmt.Sprintf("PR is not open (state: %s)", pr.State),
		}
	}

	platform := pr.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client := GetClientForGroup(group, platform)
	if client == nil {
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: "platform client not available",
		}
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: "cannot resolve repo",
		}
	}

	// Get branch info
	branchInfo, err := client.GetPRBranchInfo(c.Request.Context(), owner, repo, pr.PRNumber)
	if err != nil {
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: fmt.Sprintf("failed to get branch info: %v", err),
		}
	}

	if !branchInfo.MaintainerCanModify {
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: "rebase not allowed: PR author has not enabled 'allow edits from maintainers'",
		}
	}

	// Perform rebase
	cloneURL := config.GetCloneURL(platform, owner, repo)
	clonePath := cfg.Git.RepoClonePath
	token := config.GetToken(cfg, platform)

	rebaseErr := performRebaseForBatch(c.Request.Context(), cloneURL, token, branchInfo, clonePath)
	if rebaseErr != nil {
		slog.Error("batch rebase failed", "error", rebaseErr, "pr_id", prID)
		return BatchRebaseResult{
			PRID:    prID,
			Success: false,
			Message: fmt.Sprintf("rebase failed: %v", rebaseErr),
		}
	}

	// Update PR
	pr.HasConflict = false
	pr.Events = append(pr.Events, models.PREvent{
		Action: "batch_rebased",
		Detail: fmt.Sprintf("Batch rebased onto %s", branchInfo.BaseBranch),
	})
	prKey := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	prData, _ := json.Marshal(pr)
	if prData != nil {
		db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	}

	db.AppendAuditLog("info", "PR batch rebased", map[string]interface{}{
		"pr_id":       prID,
		"repo_group":  repoGroup,
		"platform":    platform,
		"head_branch": branchInfo.HeadBranch,
		"base_branch": branchInfo.BaseBranch,
	})

	return BatchRebaseResult{
		PRID:    prID,
		Success: true,
		Message: fmt.Sprintf("PR #%d rebased successfully onto %s", pr.PRNumber, branchInfo.BaseBranch),
	}
}

func performRebaseForBatch(ctx context.Context, cloneURL, token string, branchInfo *models.PRBranchInfo, clonePath string) error {
	return gitutil.Rebase("", cloneURL, token, branchInfo.HeadBranch, branchInfo.BaseBranch, clonePath)
}
