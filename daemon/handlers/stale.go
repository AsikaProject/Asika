package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/handlers/pr"
	"asika/daemon/stale"
)

var staleMgr *stale.Manager

func InitStaleManager(mgr *stale.Manager) {
	staleMgr = mgr
}

func HandleStaleCheck(c *gin.Context) {
	if staleMgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stale manager not initialized"})
		return
	}

	dryRun := c.Query("dry_run") == "true"
	repoGroup := c.Param("repo_group")

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config not loaded"})
		return
	}

	if repoGroup != "" {
		group := config.GetRepoGroupByName(cfg, repoGroup)
		if group == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found: " + repoGroup})
			return
		}
		if dryRun {
			actions := staleMgr.CheckRepoGroupDryRun(group)
			c.JSON(http.StatusOK, actions)
			return
		}
		staleMgr.CheckRepoGroup(group)
		c.JSON(http.StatusOK, gin.H{"message": "stale check completed for " + repoGroup})
		return
	}

	if dryRun {
		groups := config.GetRepoGroups(cfg)
		var allActions []stale.StaleAction
		for _, g := range groups {
			actions := staleMgr.CheckRepoGroupDryRun(&g)
			allActions = append(allActions, actions...)
		}
		c.JSON(http.StatusOK, allActions)
		return
	}

	go staleMgr.CheckAllGroups()
	c.JSON(http.StatusOK, gin.H{"message": "stale check started for all groups"})
}

func HandleStaleUnmark(c *gin.Context) {
	if staleMgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stale manager not initialized"})
		return
	}

	repoGroup := c.Param("repo_group")
	prNumberStr := c.Param("pr_number")

	if repoGroup == "" || prNumberStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_group and pr_number are required"})
		return
	}

	prNumber, err := strconv.Atoi(prNumberStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pr_number"})
		return
	}

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config not loaded"})
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found: " + repoGroup})
		return
	}

	label := cfg.Stale.StaleLabel
	if label == "" {
		label = "stale"
	}

	removed := false
	for _, pt := range activePlatforms(group) {
		client, ok := pr.GetClients()[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}

		slog.Info("stale: unmarking PR", "pr", prNumber, "platform", pt)
		if err := client.RemoveLabel(c.Request.Context(), owner, repo, prNumber, label); err != nil {
			slog.Error("stale: failed to remove label", "platform", pt, "error", err)
			continue
		}
		removed = true
	}

	if !removed {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove stale label on all platforms"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "stale label removed from PR " + prNumberStr + " in " + repoGroup})
}

func activePlatforms(group *models.RepoGroup) []platforms.PlatformType {
	var result []platforms.PlatformType
	if group.GitHub != "" {
		result = append(result, platforms.PlatformGitHub)
	}
	if group.GitLab != "" {
		result = append(result, platforms.PlatformGitLab)
	}
	if group.Gitea != "" {
		result = append(result, platforms.PlatformGitea)
	}
	return result
}
