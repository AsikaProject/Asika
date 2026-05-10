package pr

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
)

func BatchLabelPR(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	var req struct {
		PRIDs []string `json:"pr_ids" binding:"required"`
		Label string   `json:"label" binding:"required"`
		Color string   `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr_ids and label are required"})
		return
	}

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	platform := config.GetPlatformForGroup(group)
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

	results := make(map[string]string)
	for _, prID := range req.PRIDs {
		prNumber, err := strconv.Atoi(prID)
		if err != nil || prNumber == 0 {
			results[prID] = "invalid pr_id"
			continue
		}

		if err := client.AddLabel(c.Request.Context(), owner, repo, prNumber, req.Label, req.Color); err != nil {
			results[prID] = "failed: " + err.Error()
			slog.Warn("batch label failed", "pr_id", prID, "error", err)
		} else {
			results[prID] = "success"
			db.AppendAuditLog("info", "PR labeled (batch)", map[string]interface{}{
				"pr_number":  prNumber,
				"repo_group": repoGroup,
				"actor":      c.GetString("username"),
				"platform":   platform,
				"label":      req.Label,
				"batch":      true,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}
