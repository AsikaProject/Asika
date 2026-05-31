package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/handlers/pr"
)

// TrackDeployment handles POST /api/v1/repos/:repo_group/deployments
// Records a deployment event and links it to merged PRs.
func TrackDeployment(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	cfg := config.Current()
	if cfg == nil || !cfg.Deployment.Enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deployment tracking not enabled"})
		return
	}

	var req struct {
		Environment string                 `json:"environment" binding:"required"`
		SHA         string                 `json:"sha" binding:"required"`
		Branch      string                 `json:"branch"`
		Status      string                 `json:"status" binding:"required"`
		TriggeredBy string                 `json:"triggered_by"`
		PRIDs       []string               `json:"pr_ids"`
		Meta        map[string]interface{} `json:"meta"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	deployment := gin.H{
		"id":           config.GenerateUUID(),
		"repo_group":   repoGroup,
		"environment":  req.Environment,
		"sha":          req.SHA,
		"branch":       req.Branch,
		"status":       req.Status,
		"triggered_by": req.TriggeredBy,
		"pr_ids":       req.PRIDs,
		"meta":         req.Meta,
		"created_at":   time.Now(),
	}

	data, _ := json.Marshal(deployment)
	if err := db.Put(db.BucketSyncHistory, deployment["id"].(string), data); err != nil {
		slog.Error("failed to store deployment record", "error", err)
	}

	if cfg.Deployment.WebhookURL != "" && req.Status == "failed" && cfg.Deployment.NotifyOnFail {
		go sendDeploymentWebhook(cfg.Deployment.WebhookURL, deployment)
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   fmt.Sprintf("Deployment to %s: %s", req.Environment, req.Status),
		Actor:     c.GetString("username"),
		RepoGroup: repoGroup,
		Action:    "deployment",
		After:     deployment,
	})

	c.JSON(http.StatusOK, gin.H{
		"message":       "deployment recorded",
		"deployment_id": deployment["id"],
	})
}

// GetDeployments handles GET /api/v1/repos/:repo_group/deployments
func GetDeployments(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	cfg := config.Current()
	if cfg == nil || !cfg.Deployment.Enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deployment tracking not enabled"})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit > 200 {
		limit = 200
	}

	deployments := make([]gin.H, 0)
	count := 0
	db.ForEach(db.BucketSyncHistory, func(key, value []byte) error {
		if count >= limit {
			return fmt.Errorf("done")
		}
		var dep gin.H
		if json.Unmarshal(value, &dep) != nil {
			return nil
		}
		if dep["repo_group"] == repoGroup {
			deployments = append(deployments, dep)
			count++
		}
		return nil
	})

	c.JSON(http.StatusOK, gin.H{
		"repo_group":  repoGroup,
		"deployments": deployments,
		"count":       len(deployments),
	})
}

// DeploymentStatus handles POST /api/v1/deployment-status (external webhook)
// Receives deployment status updates from external CI/CD systems.
func DeploymentStatus(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	var payload struct {
		RepoGroup   string `json:"repo_group"`
		Environment string `json:"environment"`
		SHA         string `json:"sha"`
		Status      string `json:"status"`
		Branch      string `json:"branch"`
	}
	if json.Unmarshal(body, &payload) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	slog.Info("deployment status received",
		"repo_group", payload.RepoGroup,
		"environment", payload.Environment,
		"status", payload.Status,
		"sha", payload.SHA,
	)

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   fmt.Sprintf("Deployment status: %s -> %s", payload.Environment, payload.Status),
		RepoGroup: payload.RepoGroup,
		Action:    "deployment_status",
		After: map[string]interface{}{
			"environment": payload.Environment,
			"status":      payload.Status,
			"sha":         payload.SHA,
		},
	})

	c.JSON(http.StatusOK, gin.H{"message": "status received"})
}

// AutoTrackDeployment scans recently merged PRs and creates deployment records.
func AutoTrackDeployment(repoGroup, platform, branch string) {
	cfg := config.Current()
	if cfg == nil || !cfg.Deployment.Enabled || !cfg.Deployment.AutoTrack {
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return
	}

	client := pr.GetClientForGroup(group, platform)
	if client == nil {
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return
	}

	ctx := context.Background()
	prs, err := client.ListPRs(ctx, owner, repo, "merged")
	if err != nil {
		slog.Warn("auto-track deployment: failed to list merged PRs", "error", err)
		return
	}

	cutoff := time.Now().Add(-5 * time.Minute)
	for _, mergedPR := range prs {
		if mergedPR.MergedAt.IsZero() || mergedPR.MergedAt.Before(cutoff) {
			continue
		}
		if mergedPR.BranchInfo != nil && mergedPR.BranchInfo.BaseBranch != branch {
			continue
		}

		deployment := gin.H{
			"id":          config.GenerateUUID(),
			"repo_group":  repoGroup,
			"platform":    platform,
			"environment": "production",
			"sha":         mergedPR.MergeCommitSHA,
			"branch":      branch,
			"status":      "success",
			"pr_ids":      []string{mergedPR.ID},
			"created_at":  time.Now(),
		}
		data, _ := json.Marshal(deployment)
		db.Put(db.BucketSyncHistory, deployment["id"].(string), data)

		slog.Info("auto-tracked deployment from merged PR",
			"pr", mergedPR.PRNumber, "sha", mergedPR.MergeCommitSHA, "repo_group", repoGroup)
	}
}

func sendDeploymentWebhook(url string, deployment gin.H) {
	payload, _ := json.Marshal(map[string]interface{}{
		"text": fmt.Sprintf("Deployment %s to %s: %s",
			deployment["id"], deployment["environment"], deployment["status"]),
		"deployment": deployment,
	})
	resp, err := http.Post(url, "application/json", io.NopCloser(
		&jsonReader{data: payload},
	))
	if err != nil {
		slog.Error("failed to send deployment webhook", "error", err)
		return
	}
	defer resp.Body.Close()
}

type jsonReader struct {
	data []byte
	pos  int
}

func (r *jsonReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
