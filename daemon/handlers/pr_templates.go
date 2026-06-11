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
var depPattern = regexp.MustCompile(`(?i)depends-on:\s*(https?://\S+)`)
var depURLExtractPattern = regexp.MustCompile(`https?://([^/]+)/([^/]+)/([^/]+)/(?:pull|merge_requests|pulls)/(\d+)`)

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
		if err := db.PutPRTemplate(tpl); err != nil {
			slog.Warn("failed to store PR template", "repo_group", repoGroup, "platform", platform, "error", err)
		}
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
		"checked":   total - unchecked,
	})
}

// GetChecklistProgress handles GET /api/v1/repos/:repo_group/prs/:pr_id/checklist
func GetChecklistProgress(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	var pr models.PRRecord
	if err := json.Unmarshal(data, &pr); err != nil || pr.RepoGroup != repoGroup {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	complete, total, unchecked := ValidateChecklist(pr.Body)

	// Extract checklist items
	matches := checklistPattern.FindAllStringSubmatch(pr.Body, -1)
	items := make([]gin.H, 0, len(matches))
	lines := strings.Split(pr.Body, "\n")
	for _, line := range lines {
		match := checklistPattern.FindStringSubmatch(line)
		if match != nil {
			checked := match[1] == "x" || match[1] == "X"
			text := strings.TrimSpace(checklistPattern.ReplaceAllString(line, ""))
			items = append(items, gin.H{
				"checked": checked,
				"text":    text,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"complete":  complete,
		"total":     total,
		"checked":   total - unchecked,
		"unchecked": unchecked,
		"items":     items,
	})
}


// ParseDependencies extracts Depends-on references from a PR body.
func ParseDependencies(pr *models.PRRecord) []models.PRDependency {
	if pr.Body == "" {
		return nil
	}

	matches := depPattern.FindAllStringSubmatch(pr.Body, -1)
	if len(matches) == 0 {
		return nil
	}

	var deps []models.PRDependency
	for _, m := range matches {
		url := strings.TrimSpace(m[1])
		depRepoGroup := pr.RepoGroup
		depPlatform := pr.Platform
		var depPRNumber int
		if urlMatches := depURLExtractPattern.FindStringSubmatch(url); urlMatches != nil {
			host := urlMatches[1]
			fmt.Sscanf(urlMatches[4], "%d", &depPRNumber)
			depPlatform = detectPlatformFromURLHost(host)
			depRepoGroup = detectRepoGroupFromURL(url, depRepoGroup)
		}
		depID := ""
		if depPRNumber > 0 {
			depID = fmt.Sprintf("%s:%s:%d", depRepoGroup, depPlatform, depPRNumber)
		}
		deps = append(deps, models.PRDependency{
			PRID:          pr.ID,
			DependsOnPRID: depID,
			DependsOnURL:  url,
			RepoGroup:     pr.RepoGroup,
			Platform:      pr.Platform,
		})
	}
	return deps
}

// SyncDependencies handles POST /api/v1/repos/:repo_group/prs/:pr_id/sync-deps
func SyncDependencies(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var pr models.PRRecord
	if err := json.Unmarshal(data, &pr); err != nil || pr.RepoGroup != repoGroup {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	deps := ParseDependencies(&pr)
	if len(deps) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no dependencies found"})
		return
	}

	for _, dep := range deps {
		if err := db.PutPRDependency(&dep); err != nil {
			slog.Error("failed to store PR dependency", "pr_id", dep.PRID, "error", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "dependencies synced", "count": len(deps)})
}

// GetPRDependencies handles GET /api/v1/repos/:repo_group/prs/:pr_id/dependencies
func GetPRDependencies(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	prData, err := db.GetPRByIndex(prID, "", 0)
	if err == nil && prData != nil {
		var pr models.PRRecord
		if json.Unmarshal(prData, &pr) == nil && pr.RepoGroup != repoGroup {
			c.JSON(http.StatusNotFound, gin.H{"error": "PR not found in this repo group"})
			return
		}
	}

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

// GetDependencyGraph handles GET /api/v1/repos/:repo_group/prs/:pr_id/dependency-graph
func GetDependencyGraph(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var pr models.PRRecord
	if err := json.Unmarshal(data, &pr); err != nil || pr.RepoGroup != repoGroup {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	// Build Mermaid graph syntax
	deps, _ := db.GetPRDependenciesByPR(prID)
	dependents, _ := db.GetPRDependentsByPR(prID)

	graph := "graph TD\n"
	graph += fmt.Sprintf("    PR%s[\"%s\"]\n", prID, pr.Title)

	for _, dep := range deps {
		if dep.DependsOnPRID != "" {
			graph += fmt.Sprintf("    PR%s --> PR%s\n", prID, dep.DependsOnPRID)
		}
	}

	for _, dep := range dependents {
		if dep.PRID != "" {
			graph += fmt.Sprintf("    PR%s --> PR%s\n", dep.PRID, prID)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"mermaid": graph,
		"dependencies": deps,
		"dependents": dependents,
	})
}


func detectPlatformFromURLHost(host string) string {
	switch {
	case strings.Contains(host, "github.com"):
		return "github"
	case strings.Contains(host, "gitlab"):
		return "gitlab"
	case strings.Contains(host, "gitea"), strings.Contains(host, "forgejo"):
		return "gitea"
	case strings.Contains(host, "bitbucket.org"):
		return "bitbucket"
	case strings.Contains(host, "codeberg.org"):
		return "codeberg"
	default:
		return "unknown"
	}
}
