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

func GetPRDiff(c *gin.Context) {
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
	if dbErr == nil && data != nil {
		var pr models.PRRecord
		if json.Unmarshal(data, &pr) == nil && pr.Platform != "" {
			platform = pr.Platform
		}
		if pr.PRNumber > 0 {
			prNumber = pr.PRNumber
		}
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

	diffFiles, err := client.GetPRDiff(c.Request.Context(), owner, repo, prNumber)
	if err != nil {
		slog.Error("failed to get PR diff", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get PR diff"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"files": diffFiles})
}

func CommentPRLine(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var req models.InlineComment
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: body, commit_sha, file_path, and line are required"})
		return
	}

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
	if dbErr == nil && data != nil {
		var pr models.PRRecord
		if json.Unmarshal(data, &pr) == nil && pr.Platform != "" {
			platform = pr.Platform
		}
		if pr.PRNumber > 0 {
			prNumber = pr.PRNumber
		}
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

	if err := client.CommentPRLine(c.Request.Context(), owner, repo, prNumber, req); err != nil {
		slog.Error("failed to post inline comment", "error", err)
		db.AppendAuditLog("error", "PR inline comment failed", map[string]interface{}{
			"pr_number":  prNumber,
			"repo_group": repoGroup,
			"actor":      c.GetString("username"),
			"platform":   platform,
			"file_path":  req.FilePath,
			"line":       req.Line,
			"error":      err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to post inline comment"})
		return
	}

	db.AppendAuditLog("info", "PR inline comment added", map[string]interface{}{
		"pr_number":  prNumber,
		"repo_group": repoGroup,
		"actor":      c.GetString("username"),
		"platform":   platform,
		"file_path":  req.FilePath,
		"line":       req.Line,
		"comment":    req.Body[:min(len(req.Body), 50)],
	})
	c.JSON(http.StatusOK, gin.H{"message": "inline comment added"})
}
