package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
)

// CrossSpaceDep represents a cross-space PR dependency record.
type CrossSpaceDep struct {
	SourcePRID      string `json:"source_pr_id"`
	SourceSpace     string `json:"source_space"`
	TargetPRID      string `json:"target_pr_id"`
	TargetSpace     string `json:"target_space"`
	SourceRepoGroup string `json:"source_repo_group"`
	TargetRepoGroup string `json:"target_repo_group"`
	Status          string `json:"status"`
}

var (
	spaceCache   = make(map[string]string)
	spaceCacheMu sync.RWMutex
)

func getSpaceForPR(pr *models.PRRecord) string {
	spaceCacheMu.RLock()
	if s, ok := spaceCache[pr.RepoGroup]; ok {
		spaceCacheMu.RUnlock()
		return s
	}
	spaceCacheMu.RUnlock()

	spaces, err := db.ListTeamSpaces()
	if err != nil {
		return ""
	}
	for _, space := range spaces {
		for _, rg := range space.RepoGroups {
			spaceCacheMu.Lock()
			spaceCache[rg] = space.Name
			spaceCacheMu.Unlock()
			if rg == pr.RepoGroup {
				return space.Name
			}
		}
	}
	return ""
}

func invalidateSpaceCache() {
	spaceCacheMu.Lock()
	spaceCache = make(map[string]string)
	spaceCacheMu.Unlock()
}

// NotifyCrossSpaceDeps checks if a merged PR has cross-space dependents and publishes events.
func NotifyCrossSpaceDeps(pr *models.PRRecord) {
	if pr.ID == "" {
		return
	}

	deps, err := db.GetPRDependentsByPR(pr.ID)
	if err != nil || len(deps) == 0 {
		return
	}

	sourceSpace := getSpaceForPR(pr)
	if sourceSpace == "" {
		return
	}

	for _, dep := range deps {
		dependentPR, err := findPRByGlobalID(dep.PRID)
		if err != nil {
			slog.Warn("cross-space: dependent PR not found", "pr_id", dep.PRID)
			continue
		}

		depSpace := getSpaceForPR(dependentPR)
		if depSpace == "" || depSpace == sourceSpace {
			continue
		}

		crossDep := &CrossSpaceDep{
			SourcePRID:      pr.ID,
			SourceSpace:     sourceSpace,
			TargetPRID:      dependentPR.ID,
			TargetSpace:     depSpace,
			SourceRepoGroup: pr.RepoGroup,
			TargetRepoGroup: dependentPR.RepoGroup,
			Status:          "pending",
		}
		depData, _ := json.Marshal(crossDep)
		depKey := fmt.Sprintf("%s:%s", pr.ID, dependentPR.ID)
		db.Put(db.BucketCrossSpaceDeps, depKey, depData)

		events.PublishPR(events.EventSyncCompleted, dependentPR.RepoGroup, dependentPR.Platform, dependentPR, map[string]interface{}{
			"cross_space_notification": true,
			"source_pr_id":             pr.ID,
			"source_space":             sourceSpace,
			"message":                  fmt.Sprintf("PR #%d in space '%s' that you depend on has been merged. Please rebase.", pr.PRNumber, sourceSpace),
		})

		slog.Info("cross-space notification sent",
			"source_pr", pr.ID, "source_space", sourceSpace,
			"dep_pr", dependentPR.ID, "dep_space", depSpace)
	}
}

func findPRByGlobalID(prID string) (*models.PRRecord, error) {
	data, err := db.GetPRByIndex(prID, "", 0)
	if err == nil && data != nil {
		var pr models.PRRecord
		if err := json.Unmarshal(data, &pr); err == nil {
			return &pr, nil
		}
	}
	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.ID == prID {
			found = &pr
		}
		return nil
	})
	if found == nil {
		return nil, fmt.Errorf("PR not found: %s", prID)
	}
	return found, nil
}

func GetCrossSpaceDeps(c *gin.Context) {
	sourcePRID := c.Param("source_pr_id")
	targetPRID := c.Param("target_pr_id")
	key := fmt.Sprintf("%s:%s", sourcePRID, targetPRID)

	data, err := db.Get(db.BucketCrossSpaceDeps, key)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dependency not found"})
		return
	}

	var dep CrossSpaceDep
	if err := json.Unmarshal(data, &dep); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse"})
		return
	}
	c.JSON(http.StatusOK, dep)
}

func ResolveCrossSpaceDep(c *gin.Context) {
	sourcePRID := c.Param("source_pr_id")
	targetPRID := c.Param("target_pr_id")
	key := fmt.Sprintf("%s:%s", sourcePRID, targetPRID)

	data, err := db.Get(db.BucketCrossSpaceDeps, key)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dependency not found"})
		return
	}

	var dep CrossSpaceDep
	if err := json.Unmarshal(data, &dep); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse"})
		return
	}

	dep.Status = "resolved"
	data, _ = json.Marshal(&dep)
	db.Put(db.BucketCrossSpaceDeps, key, data)

	slog.Info("cross-space dep resolved", "source", sourcePRID, "target", targetPRID)
	c.JSON(http.StatusOK, dep)
}

func ListCrossSpaceDeps(c *gin.Context) {
	prID := c.Param("pr_id")
	deps, err := db.GetPRDependentsByPR(prID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query"})
		return
	}
	c.JSON(http.StatusOK, deps)
}
