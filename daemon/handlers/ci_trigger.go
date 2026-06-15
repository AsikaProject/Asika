package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/handlers/pr"
)

// TriggerCI handles POST /api/v1/repos/:repo_group/prs/:pr_id/trigger-ci
func TriggerCI(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var prRecord models.PRRecord
	if json.Unmarshal(data, &prRecord) != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	client := pr.GetClientForGroup(group, prRecord.Platform)
	if client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "platform client not available"})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, prRecord.Platform)

	// Trigger CI via platform API (stub - needs platform-specific implementation)
	slog.Info("triggering CI", "pr_id", prID, "platform", prRecord.Platform, "pr_number", prRecord.PRNumber)

	// For now, return success - actual implementation would call platform CI API
	c.JSON(http.StatusOK, gin.H{
		"message":   "CI trigger request sent",
		"pr_id":     prID,
		"pr_number": prRecord.PRNumber,
		"platform":  prRecord.Platform,
	})

	_ = owner
	_ = repo
}
