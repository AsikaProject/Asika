package pr

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/polling"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

var clients map[platforms.PlatformType]platforms.PlatformClient

var queueMgr *queue.Manager

var serialWorker *queue.SerialWorker

var syncerRef *syncer.Syncer

var pollerRef *polling.Poller

func GetClients() map[platforms.PlatformType]platforms.PlatformClient {
	return clients
}

func InitClients(c map[platforms.PlatformType]platforms.PlatformClient) {
	clients = c
}

func InitQueueMgr(mgr *queue.Manager) {
	queueMgr = mgr
}

func InitSerialWorker(w *queue.SerialWorker) {
	serialWorker = w
}

func InitSyncer(s *syncer.Syncer) {
	syncerRef = s
}

func InitPoller(p *polling.Poller) {
	pollerRef = p
}

func getClientForGroup(group *models.RepoGroup, platform string) platforms.PlatformClient {
	if platform == "" {
		platform = "github"
	}
	if clients == nil {
		return nil
	}
	return clients[platforms.PlatformType(platform)]
}

func GetClientForGroup(group *models.RepoGroup, platform string) platforms.PlatformClient {
	return getClientForGroup(group, platform)
}

func AddToQueue(pr *models.PRRecord) error {
	if queueMgr == nil {
		return nil
	}
	return queueMgr.AddToQueue(pr)
}

func AddToQueueScheduled(pr *models.PRRecord, scheduleAt time.Time) error {
	if queueMgr == nil {
		return nil
	}
	return queueMgr.AddToQueueScheduled(pr, scheduleAt)
}

func TriggerQueueCheck() {
	if queueMgr != nil {
		go queueMgr.CheckQueue()
	}
}

func GetQueueMgr() *queue.Manager {
	return queueMgr
}

func GetSyncer() *syncer.Syncer {
	return syncerRef
}

func RecheckQueue() {
	if queueMgr != nil {
		go queueMgr.CheckQueue()
	}
}

func RemoveFromQueue(repoGroup, prID string) error {
	if queueMgr == nil {
		return nil
	}
	return queueMgr.RemoveFromQueue(repoGroup, prID)
}

func ClearQueue(repoGroup string) (int, error) {
	if queueMgr == nil {
		return 0, nil
	}
	return queueMgr.ClearQueue(repoGroup)
}

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

	effectivePlatform := platform
	if effectivePlatform == "" && group.Mode == "single" && group.MirrorPlatform != "" {
		effectivePlatform = group.MirrorPlatform
	}

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

	sort.Slice(records, func(i, j int) bool {
		return records[i].PRNumber > records[j].PRNumber
	})

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

func GetPR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusOK, gin.H{"error": "repo group not found"})
		return
	}

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

	ctx := c.Request.Context()

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

	plats := map[string]string{
		"github":    group.GitHub,
		"gitlab":    group.GitLab,
		"gitea":     group.Gitea,
		"forgejo":   group.Forgejo,
		"codeberg":  group.Codeberg,
		"bitbucket": group.Bitbucket,
	}

	for plat, repoPath := range plats {
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

	slog.Warn("PR not found in any platform", "pr_id", prID, "repo_group", repoGroup)
	c.JSON(http.StatusOK, gin.H{"error": "PR not found"})
}
