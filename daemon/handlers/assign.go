package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/handlers/pr"
	"asika/daemon/reviewer"
)

// AssignReviewersRequest represents a manual reviewer assignment request.
type AssignReviewersRequest struct {
	Reviewers []string `json:"reviewers" binding:"required"`
}

// AssignReviewers handles POST /api/v1/repos/:repo_group/prs/:pr_id/assign
// Manually assigns reviewers to a PR. Requires approve permission.
func AssignReviewers(c *gin.Context) {
	username := c.GetString("username")
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var req AssignReviewersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: reviewers required"})
		return
	}

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	var prRecord *models.PRRecord
	err := db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var rec models.PRRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			return nil
		}
		if rec.ID == prID && rec.RepoGroup == repoGroup {
			prRecord = &rec
			return errFound
		}
		return nil
	})
	if prRecord == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	_ = err

	client := pr.GetClientForGroup(group, prRecord.Platform)
	if client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no platform client for " + prRecord.Platform})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, prRecord.Platform)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot resolve repo for platform " + prRecord.Platform})
		return
	}

	ctx := context.Background()
	if err := client.RequestReview(ctx, owner, repo, prRecord.PRNumber, req.Reviewers); err != nil {
		slog.Error("failed to assign reviewers", "error", err, "pr", prRecord.PRNumber, "reviewers", req.Reviewers)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign reviewers: " + err.Error()})
		return
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   "Reviewers assigned: " + fmt.Sprintf("%v", req.Reviewers),
		Category:  "pr",
		Actor:     username,
		RepoGroup: repoGroup,
		PRNumber:  prRecord.PRNumber,
		Platform:  prRecord.Platform,
		Action:    "assign_reviewers",
		After:     map[string]interface{}{"reviewers": req.Reviewers},
	})

	slog.Info("reviewers assigned", "pr", prRecord.PRNumber, "reviewers", req.Reviewers, "by", username)
	c.JSON(http.StatusOK, gin.H{
		"message":   "reviewers assigned",
		"pr_id":     prID,
		"reviewers": req.Reviewers,
	})
}

// TriggerCodeOwnersAssign handles POST /api/v1/repos/:repo_group/prs/:pr_id/codeowners-assign
// Re-evaluates CODEOWNERS and assigns reviewers. Requires approve permission.
func TriggerCodeOwnersAssign(c *gin.Context) {
	username := c.GetString("username")
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	var prRecord *models.PRRecord
	err := db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var rec models.PRRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			return nil
		}
		if rec.ID == prID && rec.RepoGroup == repoGroup {
			prRecord = &rec
			return errFound
		}
		return nil
	})
	if prRecord == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	_ = err

	client := pr.GetClientForGroup(group, prRecord.Platform)
	if client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no platform client for " + prRecord.Platform})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, prRecord.Platform)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot resolve repo for platform " + prRecord.Platform})
		return
	}

	ctx := context.Background()
	co, fetchErr := reviewer.GetCodeOwnersForRepo(ctx, client, owner, repo)
	if fetchErr != nil {
		slog.Error("failed to fetch CODEOWNERS", "error", fetchErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch CODEOWNERS"})
		return
	}
	if co == nil {
		c.JSON(http.StatusOK, gin.H{"message": "no CODEOWNERS file found", "assigned": []string{}})
		return
	}

	files, diffErr := client.GetDiffFiles(ctx, owner, repo, prRecord.PRNumber)
	if diffErr != nil {
		slog.Warn("failed to get diff files", "error", diffErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get diff files"})
		return
	}

	owners := co.MatchFiles(files)
	if len(owners) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no CODEOWNERS match", "assigned": []string{}})
		return
	}

	if reviewErr := client.RequestReview(ctx, owner, repo, prRecord.PRNumber, owners); reviewErr != nil {
		slog.Error("failed to assign reviewers from CODEOWNERS", "error", reviewErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign reviewers"})
		return
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   "CODEOWNERS reviewers assigned: " + fmt.Sprintf("%v", owners),
		Category:  "pr",
		Actor:     username,
		RepoGroup: repoGroup,
		PRNumber:  prRecord.PRNumber,
		Platform:  prRecord.Platform,
		Action:    "codeowners_assign",
	})

	c.JSON(http.StatusOK, gin.H{
		"message":  "CODEOWNERS reviewers assigned",
		"assigned": owners,
	})
}

var errFound = fmt.Errorf("__found__")
