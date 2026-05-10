package pr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

func ApprovePR(c *gin.Context) {
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
	var dbPR models.PRRecord
	if json.Unmarshal(data, &dbPR) == nil && dbPR.Platform != "" {
		platform = dbPR.Platform
	}
	if dbPR.PRNumber > 0 {
		prNumber = dbPR.PRNumber
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

	if err := client.ApprovePR(c.Request.Context(), owner, repo, prNumber); err != nil {
		slog.Error("failed to approve PR", "error", err)
		db.AppendAuditLog("error", "PR approve failed", map[string]interface{}{
			"pr_number":  prNumber,
			"repo_group": repoGroup,
			"actor":      c.GetString("username"),
			"platform":   platform,
			"error":      err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve PR"})
		return
	}

	prFromAPI, apiErr := client.GetPR(c.Request.Context(), owner, repo, prNumber)
	if apiErr != nil {
		slog.Warn("failed to fetch PR after approval", "error", apiErr, "pr_number", prNumber)
	}

	var pr *models.PRRecord
	var isNew bool

	if dbErr == nil && data != nil {
		var existing models.PRRecord
		if json.Unmarshal(data, &existing) == nil {
			pr = &existing
		}
	}

	if pr == nil {
		isNew = true
		pr = &models.PRRecord{
			ID:        fmt.Sprintf("%d", prNumber),
			Platform:  platform,
			RepoGroup: repoGroup,
			PRNumber:  prNumber,
		}
	}

	if pr.ID == "" {
		pr.ID = fmt.Sprintf("%d", prNumber)
	}
	if pr.RepoGroup == "" {
		pr.RepoGroup = repoGroup
	}
	if pr.PRNumber == 0 {
		pr.PRNumber = prNumber
	}
	if pr.Platform == "" {
		pr.Platform = platform
	}

	if prFromAPI != nil {
		if prFromAPI.Title != "" {
			pr.Title = prFromAPI.Title
		}
		if prFromAPI.Author != "" {
			pr.Author = prFromAPI.Author
		}
		if prFromAPI.State != "" {
			pr.State = prFromAPI.State
		}
		if len(prFromAPI.Labels) > 0 {
			pr.Labels = prFromAPI.Labels
		}
		pr.IsDraft = prFromAPI.IsDraft
		pr.HasConflict = prFromAPI.HasConflict
		pr.Platform = platform
		pr.RepoGroup = repoGroup
		pr.PRNumber = prNumber
	}

	pr.IsApproved = true
	dbKey := fmt.Sprintf("%s#%s#%d", repoGroup, platform, prNumber)
	updated, _ := json.Marshal(pr)
	db.PutPRWithIndex(dbKey, updated, pr.ID, pr.RepoGroup, pr.PRNumber)

	addedToQueue := false
	if queueMgr != nil {
		if pr.State != "" && pr.State != "open" {
			slog.Info("skipping queue add for non-open PR", "pr_number", prNumber, "repo_group", repoGroup, "state", pr.State)
		} else {
			if err := queueMgr.AddToQueue(pr); err != nil {
				slog.Warn("failed to add PR to queue", "error", err, "pr_number", prNumber)
			} else {
				addedToQueue = true
				if isNew {
					slog.Info("PR added to merge queue after approval", "pr_number", prNumber, "repo_group", repoGroup)
				}
				go queueMgr.CheckQueue()
			}
		}
	}

	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number":      prNumber,
		"repo_group":     repoGroup,
		"actor":          c.GetString("username"),
		"platform":       platform,
		"added_to_queue": addedToQueue,
	})
	c.JSON(http.StatusOK, gin.H{"message": "PR approved", "queued": addedToQueue})
}

func BatchApprovePR(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	var req struct {
		PRIDs []string `json:"pr_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr_ids is required"})
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

		if err := client.ApprovePR(c.Request.Context(), owner, repo, prNumber); err != nil {
			results[prID] = "failed: " + err.Error()
			slog.Warn("batch approve failed", "pr_id", prID, "error", err)
		} else {
			results[prID] = "success"
			db.AppendAuditLog("info", "PR approved (batch)", map[string]interface{}{
				"pr_number":  prNumber,
				"repo_group": repoGroup,
				"actor":      c.GetString("username"),
				"platform":   platform,
				"batch":      true,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}
