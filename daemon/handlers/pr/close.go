package pr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

func ClosePR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")
	reason := c.Query("reason")

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
	beforeLabels := pr.Labels

	if err := client.ClosePR(c.Request.Context(), owner, repo, prNumber); err != nil {
		slog.Error("failed to close PR", "error", err)
		db.AppendAuditLogEx(models.AuditLog{
			Level:     "error",
			Message:   "PR close failed",
			Actor:     c.GetString("username"),
			RepoGroup: repoGroup,
			PRNumber:  prNumber,
			Platform:  platform,
			Action:    "close",
			Context:   map[string]interface{}{"error": err.Error()},
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to close PR"})
		return
	}

	if reason != "" {
		if err := client.CreateLabel(c.Request.Context(), owner, repo, reason, "ededed", "Close reason"); err != nil {
			slog.Warn("failed to create close reason label", "label", reason, "error", err)
		}
		if err := client.AddLabel(c.Request.Context(), owner, repo, prNumber, reason, "ededed"); err != nil {
			slog.Warn("failed to apply close reason label", "label", reason, "error", err)
		}
		pr.Labels = append(pr.Labels, reason)
	}

	pr.State = "closed"
	pr.CloseReason = reason
	pr.UpdatedAt = time.Now()
	prData, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal closed PR", "error", err, "pr_id", pr.ID)
	} else {
		dbKey := fmt.Sprintf("%s#%s#%d", repoGroup, platform, prNumber)
		if err := db.PutPRWithIndex(dbKey, prData, pr.ID, repoGroup, prNumber); err != nil {
			slog.Error("failed to save closed PR", "error", err, "pr_id", pr.ID)
		}
	}

	if queueMgr != nil {
		if rmErr := queueMgr.RemoveFromQueue(repoGroup, pr.ID); rmErr != nil {
			slog.Warn("failed to remove closed PR from queue", "pr_id", pr.ID, "error", rmErr)
		}
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   "PR closed",
		Actor:     c.GetString("username"),
		RepoGroup: repoGroup,
		PRNumber:  prNumber,
		Platform:  platform,
		Action:    "close",
		Before:    map[string]interface{}{"state": beforeState, "labels": beforeLabels},
		After:     map[string]interface{}{"state": "closed", "labels": pr.Labels, "reason": reason},
	})
	c.JSON(http.StatusOK, gin.H{"message": "PR closed"})
}

func MarkSpam(c *gin.Context) {
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
	if err := json.Unmarshal(data, &pr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal PR data"})
		return
	}
	if pr.Platform != "" {
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

	if err := client.ClosePR(c.Request.Context(), owner, repo, prNumber); err != nil {
		slog.Error("failed to mark PR as spam", "error", err)
		db.AppendAuditLogEx(models.AuditLog{
			Level:     "error",
			Message:   "PR spam marking failed",
			Actor:     c.GetString("username"),
			RepoGroup: repoGroup,
			PRNumber:  prNumber,
			Platform:  platform,
			Action:    "mark_spam",
			Context:   map[string]interface{}{"error": err.Error()},
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark as spam"})
		return
	}

	if queueMgr != nil {
		if rmErr := queueMgr.RemoveFromQueue(repoGroup, pr.ID); rmErr != nil {
			slog.Warn("failed to remove spam PR from queue", "pr_id", pr.ID, "error", rmErr)
		}
	}

	pr.State = "spam"
	pr.SpamFlag = true
	pr.CloseReason = "spam"
	pr.Platform = platform
	pr.PRNumber = prNumber
	pr.RepoGroup = repoGroup
	pr.UpdatedAt = time.Now()
	updated, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal spam PR", "error", err, "pr_id", pr.ID)
	} else {
		dbKey := fmt.Sprintf("%s#%s#%d", repoGroup, platform, prNumber)
		if err := db.PutPRWithIndex(dbKey, updated, pr.ID, repoGroup, prNumber); err != nil {
			slog.Error("failed to save spam PR", "error", err, "pr_id", pr.ID)
		}
	}

	existing, _ := db.GetSpamAuthor(pr.Author, platform)
	if existing != nil {
		existing.Count++
		existing.LastSeen = time.Now()
		db.PutSpamAuthor(existing)
	} else {
		db.PutSpamAuthor(&models.SpamAuthor{
			Author:    pr.Author,
			Platform:  platform,
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Count:     1,
		})
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "warn",
		Message:   "PR marked as spam",
		Actor:     c.GetString("username"),
		RepoGroup: repoGroup,
		PRNumber:  prNumber,
		Platform:  platform,
		Action:    "mark_spam",
		Before:    map[string]interface{}{"state": beforeState, "spam_flag": false},
		After:     map[string]interface{}{"state": "spam", "spam_flag": true},
	})

	c.JSON(http.StatusOK, gin.H{"message": "PR marked as spam"})
}

func BatchClosePR(c *gin.Context) {
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

		if err := client.ClosePR(c.Request.Context(), owner, repo, prNumber); err != nil {
			results[prID] = "failed: " + err.Error()
			slog.Warn("batch close failed", "pr_id", prID, "error", err)
		} else {
			results[prID] = "success"

			data, dbErr := db.GetPRByIndex("", repoGroup, prNumber)
			if dbErr == nil && data != nil {
				var pr models.PRRecord
				if json.Unmarshal(data, &pr) == nil {
					pr.State = "closed"
					pr.UpdatedAt = time.Now()
					prData, _ := json.Marshal(pr)
					if prData != nil {
						dbKey := fmt.Sprintf("%s#%s#%d", repoGroup, platform, prNumber)
						db.PutPRWithIndex(dbKey, prData, pr.ID, repoGroup, prNumber)
					}
				}
			}

			db.AppendAuditLog("info", "PR closed (batch)", map[string]interface{}{
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
