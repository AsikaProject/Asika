package handlers

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

// RebaseResponse represents the result of a rebase operation
type RebaseResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	PRID    string `json:"pr_id,omitempty"`
}

// RebaseSinglePR handles POST /api/v1/repos/:repo_group/prs/:pr_id/rebase
func RebaseSinglePR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	result, err := performRebase(c.Request.Context(), group, repoGroup, prID, cfg)
	if err != nil {
		slog.Error("rebase failed", "error", err, "pr_id", prID, "repo_group", repoGroup)
		c.JSON(http.StatusInternalServerError, RebaseResponse{
			Success: false,
			Message: err.Error(),
			PRID:    prID,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// RebaseQueue handles POST /api/v1/queue/:repo_group/rebase
// Rebases all conflicted PRs in the queue for a repo group
func RebaseQueue(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	var conflictedItems []models.QueueItem
	err := db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		if item.RepoGroup != repoGroup {
			return nil
		}
		pr, findErr := findPRForRebase(item.PRID)
		if findErr != nil || pr == nil {
			return nil
		}
		if pr.HasConflict {
			conflictedItems = append(conflictedItems, item)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan queue"})
		return
	}

	if len(conflictedItems) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no conflicted PRs in queue", "results": []RebaseResponse{}})
		return
	}

	results := make([]RebaseResponse, 0, len(conflictedItems))
	for _, item := range conflictedItems {
		result, rebaseErr := performRebase(c.Request.Context(), group, repoGroup, item.PRID, cfg)
		if rebaseErr != nil {
			results = append(results, RebaseResponse{
				Success: false,
				Message: rebaseErr.Error(),
				PRID:    item.PRID,
			})
		} else {
			results = append(results, *result)
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

func performRebase(ctx context.Context, group *models.RepoGroup, repoGroup, prID string, cfg *models.Config) (*RebaseResponse, error) {
	pr, err := findPRForRebase(prID)
	if err != nil {
		return nil, fmt.Errorf("PR not found: %s", prID)
	}

	if pr.State != "open" {
		return nil, fmt.Errorf("PR is not open (state: %s)", pr.State)
	}

	platform := pr.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client := getClientForGroup(group, platform)
	if client == nil {
		return nil, fmt.Errorf("platform client not available: %s", platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("cannot resolve repo for platform %s", platform)
	}

	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get branch info: %w", err)
	}

	if !branchInfo.MaintainerCanModify {
		return nil, fmt.Errorf("rebase not allowed: PR author has not enabled 'allow edits from maintainers'. Please ask the author to enable it on the PR page")
	}

	pr.BranchInfo = branchInfo
	prKey := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	prData, _ := json.Marshal(pr)
	db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)

	cloneURL := config.GetCloneURL(platform, owner, repo)
	clonePath := cfg.Git.RepoClonePath

	rebaseErr := gitutil.Rebase("", cloneURL, config.GetToken(cfg, platform), branchInfo.HeadBranch, branchInfo.BaseBranch, clonePath)
	if rebaseErr != nil {
		pr.Events = append(pr.Events, models.PREvent{
			Action: "rebase_failed",
			Detail: rebaseErr.Error(),
		})
		prData, _ = json.Marshal(pr)
		db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
		return nil, fmt.Errorf("rebase failed: %w", rebaseErr)
	}

	pr.HasConflict = false
	pr.Events = append(pr.Events, models.PREvent{
		Action: "rebased",
		Detail: fmt.Sprintf("Successfully rebased onto %s", branchInfo.BaseBranch),
	})
	prData, _ = json.Marshal(pr)
	db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)

	db.AppendAuditLog("info", "PR rebased successfully", map[string]interface{}{
		"pr_id":       prID,
		"repo_group":  repoGroup,
		"platform":    platform,
		"head_branch": branchInfo.HeadBranch,
		"base_branch": branchInfo.BaseBranch,
	})

	return &RebaseResponse{
		Success: true,
		Message: fmt.Sprintf("PR #%d rebased successfully onto %s", pr.PRNumber, branchInfo.BaseBranch),
		PRID:    prID,
	}, nil
}

func findPRForRebase(prID string) (*models.PRRecord, error) {
	data, err := db.GetPRByIndex(prID, "", 0)
	if err == nil && data != nil {
		var pr models.PRRecord
		if json.Unmarshal(data, &pr) == nil {
			return &pr, nil
		}
	}

	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(value, &pr) != nil {
			return nil
		}
		if pr.ID == prID {
			found = &pr
		}
		return nil
	})
	if found == nil {
		return nil, fmt.Errorf("PR not found: %s", prID)
	}
	return found, nil
}



// CherryPickRequest represents a cherry-pick operation request
type CherryPickRequest struct {
	TargetBranch string `json:"target_branch" binding:"required"`
}

// CherryPickResponse represents the result of a cherry-pick operation
type CherryPickResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	PRID    string `json:"pr_id,omitempty"`
}

// CherryPickSinglePR handles POST /api/v1/repos/:repo_group/prs/:pr_id/cherry-pick
func CherryPickSinglePR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var req CherryPickRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_branch is required"})
		return
	}

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	result, err := performCherryPick(c.Request.Context(), group, repoGroup, prID, req.TargetBranch, cfg)
	if err != nil {
		slog.Error("cherry-pick failed", "error", err, "pr_id", prID, "repo_group", repoGroup)
		c.JSON(http.StatusInternalServerError, CherryPickResponse{
			Success: false,
			Message: err.Error(),
			PRID:    prID,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func performCherryPick(ctx context.Context, group *models.RepoGroup, repoGroup, prID, targetBranch string, cfg *models.Config) (*CherryPickResponse, error) {
	pr, err := findPRForRebase(prID)
	if err != nil {
		return nil, fmt.Errorf("PR not found: %s", prID)
	}

	if pr.State != "merged" {
		return nil, fmt.Errorf("PR is not merged (state: %s), only merged PRs can be cherry-picked", pr.State)
	}

	if pr.MergeCommitSHA == "" {
		return nil, fmt.Errorf("PR has no merge commit SHA, cannot cherry-pick")
	}

	platform := pr.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client := getClientForGroup(group, platform)
	if client == nil {
		return nil, fmt.Errorf("platform client not available: %s", platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("cannot resolve repo for platform %s", platform)
	}

	// Fetch fresh PR info to ensure we have the merge commit SHA
	freshPR, getErr := client.GetPR(ctx, owner, repo, pr.PRNumber)
	if getErr == nil && freshPR != nil && freshPR.MergeCommitSHA != "" {
		pr.MergeCommitSHA = freshPR.MergeCommitSHA
	}

	cloneURL := config.GetCloneURL(platform, owner, repo)
	token := config.GetToken(cfg, platform)
	clonePath := cfg.Git.RepoClonePath

	// Clone, checkout target branch, cherry-pick, push
	cpErr := gitutil.CherryPickRemote("", cloneURL, token, targetBranch, pr.MergeCommitSHA, clonePath)
	if cpErr != nil {
		pr.Events = append(pr.Events, models.PREvent{
			Action: "cherry_pick_failed",
			Detail: cpErr.Error(),
		})
		prData, _ := json.Marshal(pr)
		prKey := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
		db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
		return nil, fmt.Errorf("cherry-pick failed: %w", cpErr)
	}

	pr.Events = append(pr.Events, models.PREvent{
		Action: "cherry_picked",
		Detail: fmt.Sprintf("Cherry-picked %s onto %s", pr.MergeCommitSHA[:8], targetBranch),
	})
	prData, _ := json.Marshal(pr)
	prKey := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)

	db.AppendAuditLog("info", "PR cherry-picked successfully", map[string]interface{}{
		"pr_id":        prID,
		"repo_group":   repoGroup,
		"platform":     platform,
		"merge_commit": pr.MergeCommitSHA,
		"target_branch": targetBranch,
	})

	return &CherryPickResponse{
		Success: true,
		Message: fmt.Sprintf("Cherry-picked %s onto %s", pr.MergeCommitSHA[:8], targetBranch),
		PRID:    prID,
	}, nil
}
