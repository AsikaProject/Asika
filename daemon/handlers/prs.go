package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/polling"
	"asika/daemon/syncer"
)

// clients is a package-level variable to access platform clients
var clients map[platforms.PlatformType]platforms.PlatformClient

// syncerRef is set by InitSyncer from cmd/asikad/main.go
var syncerRef *syncer.Syncer

// pollerRef is set by InitPoller for triggering manual sync
var pollerRef *polling.Poller

// InitClients initializes the platform clients for handlers
func InitClients(c map[platforms.PlatformType]platforms.PlatformClient) {
	clients = c
}

// InitSyncer initializes the syncer for handlers
func InitSyncer(s *syncer.Syncer) {
	syncerRef = s
}

// InitPoller initializes the poller for handlers
func InitPoller(p *polling.Poller) {
	pollerRef = p
}

// ListPRs handles GET /api/v1/repos/:repo_group/prs (8.2)
func ListPRs(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	state := c.Query("state")
	platform := c.Query("platform")
	isDraftStr := c.Query("is_draft")
	author := c.Query("author")
	label := c.Query("label")
	createdAfter := c.Query("created_after")
	updatedAfter := c.Query("updated_after")
	pageStr := c.Query("page")
	perPageStr := c.Query("per_page")
	refresh := c.Query("refresh")

	if refresh == "1" && pollerRef != nil {
		pollerRef.PollOnce()
	}

	records := make([]models.PRRecord, 0)

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusOK, records)
		return
	}

	// In single mode, default platform filter to MirrorPlatform if not explicitly specified
	effectivePlatform := platform
	if effectivePlatform == "" && group.Mode == "single" && group.MirrorPlatform != "" {
		effectivePlatform = group.MirrorPlatform
	}

	// Read PRs from local DB using index prefix scan for fast response
	indexPrefix := repoGroup + ":"
	_ = db.ForEachPrefix(db.BucketPRIndexByRG, db.BucketPRs, indexPrefix, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if effectivePlatform != "" && pr.Platform != effectivePlatform {
			return nil
		}
		if state != "" && pr.State != state {
			return nil
		}
		if isDraftStr != "" {
			isDraft := isDraftStr == "true"
			if pr.IsDraft != isDraft {
				return nil
			}
		}
		if author != "" && pr.Author != author {
			return nil
		}
		if label != "" {
			hasLabel := false
			for _, l := range pr.Labels {
				if l == label {
					hasLabel = true
					break
				}
			}
			if !hasLabel {
				return nil
			}
		}
		if createdAfter != "" {
			if t, err := time.Parse(time.RFC3339, createdAfter); err == nil {
				if !pr.CreatedAt.After(t) {
					return nil
				}
			}
		}
		if updatedAfter != "" {
			if t, err := time.Parse(time.RFC3339, updatedAfter); err == nil {
				if !pr.UpdatedAt.After(t) {
					return nil
				}
			}
		}
		records = append(records, pr)
		return nil
	})

	// Sort by PR number descending (newest first)
	sort.Slice(records, func(i, j int) bool {
		return records[i].PRNumber > records[j].PRNumber
	})

	// Pagination
	page := 1
	perPage := 100
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 && pp <= 100 {
		perPage = pp
	}

	total := len(records)
	start := (page - 1) * perPage
	if start >= total {
		c.JSON(http.StatusOK, gin.H{"data": []models.PRRecord{}, "total": total, "page": page, "per_page": perPage})
		return
	}
	end := start + perPage
	if end > total {
		end = total
	}
	paged := records[start:end]

	c.JSON(http.StatusOK, gin.H{"data": paged, "total": total, "page": page, "per_page": perPage})
}

// GetPR handles GET /api/v1/repos/:repo_group/prs/:pr_id (8.2)
func GetPR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusOK, gin.H{"error": "repo group not found"})
		return
	}

	// Try to find PR in DB using index or scan
	var found *models.PRRecord
	prNumber, convErr := strconv.Atoi(prID)
	if convErr == nil {
		data, err := db.GetPRByIndex("", repoGroup, prNumber)
		if err == nil && data != nil {
			var pr models.PRRecord
			if json.Unmarshal(data, &pr) == nil && pr.RepoGroup == repoGroup {
				found = &pr
			}
		}
	}
	if found == nil {
		data, err := db.GetPRByIndex(prID, "", 0)
		if err == nil && data != nil {
			var pr models.PRRecord
			if json.Unmarshal(data, &pr) == nil {
				if pr.RepoGroup == repoGroup || pr.RepoGroup == "" {
					found = &pr
				}
			}
		}
	}
	if found == nil {
		db.ForEach(db.BucketPRs, func(key, value []byte) error {
			var pr models.PRRecord
			if json.Unmarshal(value, &pr) != nil {
				return nil
			}
			if pr.RepoGroup == repoGroup && (pr.ID == prID || fmt.Sprintf("%d", pr.PRNumber) == prID) {
				found = &pr
			}
			return nil
		})
	}

	if found != nil {
		c.JSON(http.StatusOK, found)
		return
	}

	// Not in DB, try platform APIs
	ctx := c.Request.Context()

	// In single mode, only query the MirrorPlatform
	if group.Mode == "single" && group.MirrorPlatform != "" {
		plat := group.MirrorPlatform
		client := getClientForGroup(group, plat)
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, plat)
			if prNumber > 0 {
				pr, err := client.GetPR(ctx, owner, repo, prNumber)
				if err == nil && pr != nil {
					pr.RepoGroup = repoGroup
					pr.Platform = plat
					c.JSON(http.StatusOK, pr)
					return
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"error": "PR not found"})
		return
	}

	// Multi mode: try all configured platforms
	platforms := map[string]string{
		"github":    group.GitHub,
		"gitlab":    group.GitLab,
		"gitea":     group.Gitea,
		"forgejo":   group.Forgejo,
		"codeberg":  group.Codeberg,
		"bitbucket": group.Bitbucket,
	}

	for plat, repoPath := range platforms {
		if repoPath == "" {
			continue
		}
		client := getClientForGroup(group, plat)
		if client == nil {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, plat)

		if convErr != nil {
			continue
		}

		pr, err := client.GetPR(ctx, owner, repo, prNumber)
		if err != nil {
			continue
		}
		if pr != nil {
			pr.RepoGroup = repoGroup
			pr.Platform = plat
			c.JSON(http.StatusOK, pr)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"error": "PR not found"})
}

// ApprovePR handles POST /api/v1/repos/:repo_group/prs/:pr_id/approve (8.2)
func ApprovePR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	// Try to get platform from DB first, fallback to repo group config
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

	// Add to merge queue (only if PR is still open)
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
				// Trigger immediate queue check
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

// ClosePR handles POST /api/v1/repos/:repo_group/prs/:pr_id/close (8.2)
func ClosePR(c *gin.Context) {
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

	if err := client.ClosePR(c.Request.Context(), owner, repo, prNumber); err != nil {
		slog.Error("failed to close PR", "error", err)
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number":  prNumber,
			"repo_group": repoGroup,
			"actor":      c.GetString("username"),
			"platform":   platform,
			"error":      err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to close PR"})
		return
	}

	// Remove from merge queue if present
	if queueMgr != nil {
		if rmErr := queueMgr.RemoveFromQueue(repoGroup, pr.ID); rmErr != nil {
			slog.Warn("failed to remove closed PR from queue", "pr_id", pr.ID, "error", rmErr)
		}
	}

	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number":  prNumber,
		"repo_group": repoGroup,
		"actor":      c.GetString("username"),
		"platform":   platform,
	})
	c.JSON(http.StatusOK, gin.H{"message": "PR closed"})
}

// ReopenPR handles POST /api/v1/repos/:repo_group/prs/:pr_id/reopen (8.2)
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

	if err := client.ReopenPR(c.Request.Context(), owner, repo, prNumber); err != nil {
		slog.Error("failed to reopen PR", "error", err)
		db.AppendAuditLog("error", "PR reopen failed", map[string]interface{}{
			"pr_number":  prNumber,
			"repo_group": repoGroup,
			"actor":      c.GetString("username"),
			"platform":   platform,
			"error":      err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reopen PR"})
		return
	}

	db.AppendAuditLog("info", "PR reopened", map[string]interface{}{
		"pr_number":  prNumber,
		"repo_group": repoGroup,
		"actor":      c.GetString("username"),
		"platform":   platform,
	})
	c.JSON(http.StatusOK, gin.H{"message": "PR reopened"})
}

// MarkSpam handles POST /api/v1/repos/:repo_group/prs/:pr_id/spam (8.2)
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

	if err := client.ClosePR(c.Request.Context(), owner, repo, prNumber); err != nil {
		slog.Error("failed to mark PR as spam", "error", err)
		db.AppendAuditLog("error", "PR spam marking failed", map[string]interface{}{
			"pr_number":  prNumber,
			"repo_group": repoGroup,
			"actor":      c.GetString("username"),
			"platform":   platform,
			"error":      err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark as spam"})
		return
	}

	// Remove from merge queue if present
	if queueMgr != nil {
		if rmErr := queueMgr.RemoveFromQueue(repoGroup, pr.ID); rmErr != nil {
			slog.Warn("failed to remove spam PR from queue", "pr_id", pr.ID, "error", rmErr)
		}
	}

	// Update PR record to mark as spam
	pr.State = "spam"
	pr.SpamFlag = true
	pr.Platform = platform
	pr.PRNumber = prNumber
	pr.RepoGroup = repoGroup
	updated, _ := json.Marshal(pr)
	dbKey := fmt.Sprintf("%s#%s#%d", repoGroup, platform, prNumber)
	db.PutPRWithIndex(dbKey, updated, pr.ID, repoGroup, prNumber)

	db.AppendAuditLog("warn", "PR marked as spam", map[string]interface{}{
		"pr_number":  prNumber,
		"repo_group": repoGroup,
		"actor":      c.GetString("username"),
		"platform":   platform,
	})

	c.JSON(http.StatusOK, gin.H{"message": "PR marked as spam"})
}

// CommentPR handles POST /api/v1/repos/:repo_group/prs/:pr_id/comment
func CommentPR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var req struct {
		Body string `json:"body" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body is required"})
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

	if err := client.CommentPR(c.Request.Context(), owner, repo, prNumber, req.Body); err != nil {
		slog.Error("failed to comment on PR", "error", err)
		db.AppendAuditLog("error", "PR comment failed", map[string]interface{}{
			"pr_number":  prNumber,
			"repo_group": repoGroup,
			"actor":      c.GetString("username"),
			"platform":   platform,
			"error":      err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to comment on PR"})
		return
	}

	db.AppendAuditLog("info", "PR commented", map[string]interface{}{
		"pr_number":  prNumber,
		"repo_group": repoGroup,
		"actor":      c.GetString("username"),
		"platform":   platform,
		"comment":    req.Body[:min(len(req.Body), 50)],
	})
	c.JSON(http.StatusOK, gin.H{"message": "comment added"})
}

// BatchApprovePR handles POST /api/v1/repos/:repo_group/prs/batch/approve
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

// BatchClosePR handles POST /api/v1/repos/:repo_group/prs/batch/close
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

// BatchLabelPR handles POST /api/v1/repos/:repo_group/prs/batch/label
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

// GetLogs handles GET /api/v1/logs (8.2)
func GetLogs(c *gin.Context) {
	level := c.Query("level")

	logs := make([]models.AuditLog, 0)
	err := db.ForEach(db.BucketLogs, func(key, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		if level != "" && log.Level != level {
			return nil
		}
		logs = append(logs, log)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusOK, logs)
		return
	}

	c.JSON(http.StatusOK, logs)
}

// ExportLogs handles GET /api/v1/logs/export (8.2)
// Returns audit logs as a downloadable JSON file
func ExportLogs(c *gin.Context) {
	format := c.Query("format")
	if format == "" {
		format = "json"
	}
	level := c.Query("level")

	logs := make([]models.AuditLog, 0)
	err := db.ForEach(db.BucketLogs, func(key, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		if level != "" && log.Level != level {
			return nil
		}
		logs = append(logs, log)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read logs"})
		return
	}

	switch format {
	case "json":
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", "attachment; filename=asika-audit-logs.json")
		c.JSON(http.StatusOK, logs)
	case "csv":
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=asika-audit-logs.csv")
		c.String(http.StatusOK, "timestamp,level,message\n")
		for _, l := range logs {
			c.String(http.StatusOK, "%s,%s,\"%s\"\n", l.Timestamp.Format(time.RFC3339), l.Level, strings.ReplaceAll(l.Message, "\"", "\"\""))
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported format: " + format})
	}
}

// getClientForGroup returns the platform client for a repo group
func getClientForGroup(group *models.RepoGroup, platform string) platforms.PlatformClient {
	if platform == "" {
		platform = "github"
	}
	if clients == nil {
		return nil
	}
	return clients[platforms.PlatformType(platform)]
}
