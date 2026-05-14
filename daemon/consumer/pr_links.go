package consumer

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"asika/common/models"
)

var issueRefPattern = regexp.MustCompile(`(?i)(?:fixes|closes|resolves|references?)\s*:?\s*(?:([a-zA-Z0-9_-]+)\/([a-zA-Z0-9_-]+))?#(\d+)`)

func syncPRLinks(w *writerActor, pr *models.PRRecord) {
	links := parseIssueLinksFromPR(pr)
	for _, link := range links {
		if err := w.writeIssueLink(&link); err != nil {
			slog.Error("failed to store issue-PRLink", "error", err, "pr_id", pr.ID)
		}
	}
}

func parseIssueLinksFromPR(pr *models.PRRecord) []models.IssuePRLink {
	combined := pr.Title
	if pr.Body != "" {
		combined += "\n" + pr.Body
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
