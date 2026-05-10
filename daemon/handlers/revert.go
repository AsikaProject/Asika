package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	handlerspr "asika/daemon/handlers/pr"
	commonutil "asika/common/platformutil"
)

// RevertPR handles POST /api/v1/repos/:repo_group/prs/:pr_id/revert
func RevertPR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	pr, err := commonutil.GetPRByID(repoGroup, prID)
	if err != nil || pr == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	if pr.State != "merged" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "can only revert merged PRs"})
		return
	}

	platform := pr.Platform
	client := handlerspr.GetClientForGroup(group, platform)
	if client == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "platform client not available", "platform": platform})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot resolve repo"})
		return
	}

	revertPR, err := client.RevertPR(c.Request.Context(), owner, repo, pr.PRNumber)
	if err != nil {
		slog.Error("failed to revert PR", "error", err)
		db.AppendAuditLog("error", "PR revert failed", map[string]interface{}{
			"pr_number":  pr.PRNumber,
			"repo_group": repoGroup,
			"actor":      c.GetString("username"),
			"platform":   platform,
			"error":      err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revert PR: " + err.Error()})
		return
	}

	newPR := models.PRRecord{
		ID:        pr.ID + "-revert",
		RepoGroup: repoGroup,
		Platform:  platform,
		PRNumber:  revertPR.PRNumber,
		Title:     revertPR.Title,
		Author:    c.GetString("username"),
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Events: []models.PREvent{
			{Timestamp: time.Now(), Action: "reverted", Actor: c.GetString("username"), Detail: fmt.Sprintf("Revert of PR #%d", pr.PRNumber)},
		},
	}
	newPRData, _ := json.Marshal(newPR)
	newKey := fmt.Sprintf("%s#%s#%d", repoGroup, platform, revertPR.PRNumber)
	db.PutPRWithIndex(newKey, newPRData, newPR.ID, repoGroup, revertPR.PRNumber)

	events.PublishPR(events.EventPRReverted, repoGroup, platform, &newPR, nil)

	if err := handlerspr.AddToQueue(&newPR); err != nil {
		slog.Warn("failed to add revert PR to queue", "error", err, "pr_number", revertPR.PRNumber)
	} else {
		handlerspr.TriggerQueueCheck()
	}

	commentBody := fmt.Sprintf("This PR has been reverted by %s. Revert PR: #%d", c.GetString("username"), revertPR.PRNumber)
	if err := client.CommentPR(c.Request.Context(), owner, repo, pr.PRNumber, commentBody); err != nil {
		slog.Warn("failed to comment on reverted PR", "error", err, "pr_number", pr.PRNumber)
	}

	db.AppendAuditLog("info", "PR reverted", map[string]interface{}{
		"pr_number":        pr.PRNumber,
		"repo_group":       repoGroup,
		"actor":            c.GetString("username"),
		"platform":         platform,
		"revert_pr_number": revertPR.PRNumber,
	})

	c.JSON(http.StatusOK, gin.H{
		"message":          "PR reverted successfully",
		"revert_pr_number": revertPR.PRNumber,
		"revert_pr_title":  revertPR.Title,
	})
}
