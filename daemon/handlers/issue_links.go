package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"asika/common/db"
	"asika/common/models"
)

var issueRefPattern = regexp.MustCompile(`(?i)(?:fixes|closes|resolves|references?|refs?)\s*:?\s*(?:([a-zA-Z0-9_-]+)\/([a-zA-Z0-9_-]+))?#(\d+)`)

// ParseIssueLinks extracts issue references from a PR description.
// Returns a list of IssuePRLink for each matched reference.
func ParseIssueLinks(pr *models.PRRecord) []models.IssuePRLink {
	if pr.Title == "" && pr.ID == "" {
		return nil
	}

	combined := pr.Title
	body := getPRBody(pr)
	if body != "" {
		combined += "\n" + body
	}

	matches := issueRefPattern.FindAllStringSubmatch(combined, -1)
	if len(matches) == 0 {
		return nil
	}

	var links []models.IssuePRLink
	seen := make(map[string]bool)

	for _, m := range matches {
		linkType := strings.ToLower(strings.TrimSpace(m[0]))
		for _, kw := range []string{"fixes", "closes", "resolves", "references", "reference", "refs", "ref"} {
			if strings.HasPrefix(linkType, kw) {
				linkType = kw
				break
			}
		}
		if linkType == "reference" || linkType == "references" || linkType == "refs" || linkType == "ref" {
			linkType = "related"
		}

		var owner, repo string
		if m[1] != "" && m[2] != "" {
			owner = m[1]
			repo = m[2]
		} else {
			owner = "_"
			repo = "_"
		}

		issueNum := 0
		fmt.Sscanf(m[3], "%d", &issueNum)

		issueID := fmt.Sprintf("%s/%s#%d", owner, repo, issueNum)
		key := fmt.Sprintf("%s:%s", issueID, pr.ID)
		if seen[key] {
			continue
		}
		seen[key] = true

		links = append(links, models.IssuePRLink{
			IssueID:   issueID,
			PRID:      pr.ID,
			RepoGroup: pr.RepoGroup,
			Platform:  pr.Platform,
			LinkType:  linkType,
		})
	}

	return links
}

func getPRBody(pr *models.PRRecord) string {
	return pr.Body
}

// GetIssueLinks handles GET /api/v1/repos/:repo_group/issues/:issue_id/prs
func GetIssueLinks(c *gin.Context) {
	issueID := c.Query("issue_id")
	if issueID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "issue_id required"})
		return
	}

	repoGroup := c.Param("repo_group")

	links, err := db.GetIssuePRLinksByIssue(issueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query links"})
		return
	}

	if repoGroup != "" {
		filtered := make([]*models.IssuePRLink, 0, len(links))
		for _, l := range links {
			if l.RepoGroup == repoGroup {
				filtered = append(filtered, l)
			}
		}
		links = filtered
	}

	c.JSON(http.StatusOK, links)
}

// GetPRLinks handles GET /api/v1/repos/:repo_group/prs/:pr_id/issues
func GetPRLinks(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")
	if prID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr_id required"})
		return
	}

	prData, err := db.GetPRByIndex(prID, "", 0)
	if err == nil && prData != nil {
		var pr models.PRRecord
		if json.Unmarshal(prData, &pr) == nil && pr.RepoGroup != repoGroup {
			c.JSON(http.StatusNotFound, gin.H{"error": "PR not found in this repo group"})
			return
		}
	}

	links, err := db.GetIssuePRLinksByPR(prID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query links"})
		return
	}

	c.JSON(http.StatusOK, links)
}

// SyncIssueLinks handles POST /api/v1/repos/:repo_group/prs/:pr_id/sync-links
func SyncIssueLinks(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var prRecord models.PRRecord
	if err := json.Unmarshal(data, &prRecord); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	if prRecord.RepoGroup != repoGroup {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found in this repo group"})
		return
	}
	links := ParseIssueLinks(&prRecord)
	if len(links) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no issue links found"})
		return
	}

	for _, link := range links {
		db.PutIssuePRLink(&link)
	}

	c.JSON(http.StatusOK, gin.H{"message": "links synced", "count": len(links)})
}
