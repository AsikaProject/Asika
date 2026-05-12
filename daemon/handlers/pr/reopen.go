package pr

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

func ReopenPR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	platform := config.GetPlatformForGroup(group)
	prNumber, err := strconv.Atoi(prID)
	if err != nil || prNumber == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pr_id, must be a number"})
		return
	}

	data, dbErr := db.GetPRByIndex("", repoGroup, prNumber)
	if dbErr != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var pr models.PRRecord
	if json.Unmarshal(data, &pr) == nil && pr.Platform != "" {
		platform = pr.Platform
	}
	if pr.PRNumber > 0 {
		prNumber = pr.PRNumber
	}

	if platform == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "cannot determine platform"})
		return
	}

	client := getClientForGroup(group, platform)
	if client == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "platform client not available (check token configuration)", "platform": platform})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot resolve repo"})
		return
	}

	beforeState := pr.State

	if err := client.ReopenPR(c.Request.Context(), owner, repo, prNumber); err != nil {
		slog.Error("failed to reopen PR", "error", err)
		db.AppendAuditLogEx(models.AuditLog{
			Level:     "error",
			Message:   "PR reopen failed",
			Actor:     c.GetString("username"),
			RepoGroup: repoGroup,
			PRNumber:  prNumber,
			Platform:  platform,
			Action:    "reopen",
			Context:   map[string]interface{}{"error": err.Error()},
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reopen PR"})
		return
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   "PR reopened",
		Actor:     c.GetString("username"),
		RepoGroup: repoGroup,
		PRNumber:  prNumber,
		Platform:  platform,
		Action:    "reopen",
		Before:    map[string]interface{}{"state": beforeState},
		After:     map[string]interface{}{"state": "open"},
	})
	c.JSON(http.StatusOK, gin.H{"message": "PR reopened"})
}
