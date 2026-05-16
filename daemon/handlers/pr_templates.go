package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/handlers/pr"
)

var templatePaths = []string{
	".github/PULL_REQUEST_TEMPLATE.md",
	".github/pull_request_template.md",
	"PULL_REQUEST_TEMPLATE.md",
	"docs/PULL_REQUEST_TEMPLATE.md",
}

var checklistPattern = regexp.MustCompile(`(?m)^\s*[-*]\s+\[([ x])\]`)

// FetchPRTemplate fetches the PR template from the platform.
func FetchPRTemplate(repoGroup, platform string) (*models.PRTemplate, error) {
	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return nil, fmt.Errorf("repo group not found: %s", repoGroup)
	}

	client := pr.GetClientForGroup(group, platform)
	if client == nil {
		return nil, fmt.Errorf("no client for platform: %s", platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("cannot resolve repo for platform %s in group %s", platform, repoGroup)
	}

	for _, path := range templatePaths {
		content, err := client.GetFileContent(context.Background(), owner, repo, path)
		if err != nil || content == "" {
			continue
		}
		hasChecklist := checklistPattern.MatchString(content)
		tpl := &models.PRTemplate{
			RepoGroup:    repoGroup,
			Platform:     platform,
			Content:      content,
			HasChecklist: hasChecklist,
		}
		db.PutPRTemplate(tpl)
		return tpl, nil
	}

	return nil, fmt.Errorf("no PR template found")
}

// ValidateChecklist checks if all checklist items in a PR body are checked.
func ValidateChecklist(body string) (complete bool, total int, unchecked int) {
	matches := checklistPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return true, 0, 0
	}
	total = len(matches)
	for _, m := range matches {
		if m[1] != "x" && m[1] != "X" {
			unchecked++
		}
	}
	return unchecked == 0, total, unchecked
}

// GetPRTemplate handles GET /api/v1/repos/:repo_group/template
func GetPRTemplate(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	platform := c.Query("platform")
	if platform == "" {
		platform = "github"
	}

	tpl, err := db.GetPRTemplate(repoGroup, platform)
	if err != nil || tpl == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no template found"})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// FetchTemplate handles POST /api/v1/repos/:repo_group/template/fetch
func FetchTemplate(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	platform := c.Query("platform")
	if platform == "" {
		platform = "github"
	}

	tpl, err := FetchPRTemplate(repoGroup, platform)
	if err != nil {
		slog.Warn("failed to fetch PR template", "repo_group", repoGroup, "platform", platform, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// CheckChecklist handles POST /api/v1/repos/:repo_group/prs/:pr_id/checklist
func CheckChecklist(c *gin.Context) {
	body := c.PostForm("body")
	if body == "" {
		var req struct {
			Body string `json:"body"`
		}
		if err := c.ShouldBindJSON(&req); err == nil {
			body = req.Body
		}
	}

	complete, total, unchecked := ValidateChecklist(body)
	c.JSON(http.StatusOK, gin.H{
		"complete":  complete,
		"total":     total,
		"unchecked": unchecked,
	})
}

// ParseDependencies extracts Depends-on references from a PR body.
func ParseDependencies(pr *models.PRRecord) []models.PRDependency {
	if pr.Body == "" {
		return nil
	}

	depPattern := regexp.MustCompile(`(?i)depends-on:\s*(https?://\S+)`)
	matches := depPattern.FindAllStringSubmatch(pr.Body, -1)
	if len(matches) == 0 {
		return nil
	}

	var deps []models.PRDependency
	for _, m := range matches {
		url := strings.TrimSpace(m[1])
		deps = append(deps, models.PRDependency{
			PRID:         pr.ID,
			DependsOnURL: url,
			RepoGroup:    pr.RepoGroup,
			Platform:     pr.Platform,
		})
	}
	return deps
}

// SyncDependencies handles POST /api/v1/repos/:repo_group/prs/:pr_id/sync-deps
func SyncDependencies(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup && pr.ID == prID {
			found = &pr
		}
		return nil
	})

	if found == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	deps := ParseDependencies(found)
	if len(deps) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no dependencies found"})
		return
	}

	for _, dep := range deps {
		db.PutPRDependency(&dep)
	}

	c.JSON(http.StatusOK, gin.H{"message": "dependencies synced", "count": len(deps)})
}

// GetPRDependencies handles GET /api/v1/repos/:repo_group/prs/:pr_id/dependencies
func GetPRDependencies(c *gin.Context) {
	prID := c.Param("pr_id")
	deps, err := db.GetPRDependenciesByPR(prID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query dependencies"})
		return
	}
	c.JSON(http.StatusOK, deps)
}

// GetPRDependents handles GET /api/v1/repos/:repo_group/prs/:pr_id/dependents
func GetPRDependents(c *gin.Context) {
	prID := c.Param("pr_id")
	deps, err := db.GetPRDependentsByPR(prID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query dependents"})
		return
	}
	c.JSON(http.StatusOK, deps)
}
