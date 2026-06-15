package handlers

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/handlers/pr"
)

// AutoLabelPR handles POST /api/v1/repos/:repo_group/prs/:pr_id/auto-label
func AutoLabelPR(c *gin.Context) {
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

	labels := generateAutoLabels(&prRecord)
	if len(labels) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no labels to add", "labels": []string{}})
		return
	}

	// Add labels via platform API
	client := pr.GetClientForGroup(group, prRecord.Platform)
	if client == nil {
		slog.Error("platform client not available for auto-label", "platform", prRecord.Platform, "repo_group", repoGroup, "pr_id", prID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "platform client not available",
			"labels":  labels,
			"applied": false,
		})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, prRecord.Platform)
	applied := 0
	failed := make([]string, 0)
	for _, label := range labels {
		if err := client.AddLabel(c.Request.Context(), owner, repo, prRecord.PRNumber, label, ""); err != nil {
			slog.Warn("failed to add auto label", "label", label, "error", err)
			failed = append(failed, label)
		} else {
			applied++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "labels applied",
		"labels":  labels,
		"applied": applied,
		"failed":  failed,
	})
}

func generateAutoLabels(pr *models.PRRecord) []string {
	var labels []string

	// Size labels
	totalLines := pr.LinesAdded + pr.LinesDeleted
	if totalLines < 10 {
		labels = append(labels, "size/XS")
	} else if totalLines < 100 {
		labels = append(labels, "size/S")
	} else if totalLines < 500 {
		labels = append(labels, "size/M")
	} else if totalLines < 1000 {
		labels = append(labels, "size/L")
	} else {
		labels = append(labels, "size/XL")
	}

	// File type labels
	langMap := make(map[string]int)
	hasDoc := false
	hasConfig := false

	for _, file := range pr.DiffFiles {
		ext := strings.ToLower(filepath.Ext(file))
		base := strings.ToLower(filepath.Base(file))

		switch ext {
		case ".go":
			langMap["go"]++
		case ".js", ".jsx", ".ts", ".tsx":
			langMap["javascript"]++
		case ".py":
			langMap["python"]++
		case ".java":
			langMap["java"]++
		case ".rs":
			langMap["rust"]++
		case ".md", ".rst":
			hasDoc = true
		case ".toml", ".yaml", ".yml", ".json", ".xml":
			hasConfig = true
		}

		if strings.HasPrefix(base, "dockerfile") {
			hasConfig = true
		}
	}

	// Add dominant language label
	maxCount := 0
	dominantLang := ""
	for lang, count := range langMap {
		if count > maxCount {
			maxCount = count
			dominantLang = lang
		}
	}
	if dominantLang != "" {
		labels = append(labels, "lang/"+dominantLang)
	}

	if hasDoc {
		labels = append(labels, "docs")
	}
	if hasConfig {
		labels = append(labels, "config")
	}

	// Risk labels
	if totalLines > 1000 {
		labels = append(labels, "risk/high")
	}

	return labels
}
