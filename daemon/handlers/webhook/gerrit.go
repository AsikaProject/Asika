package webhook

import (
	"encoding/json"
	"fmt"

	"asika/common/events"
	"asika/common/models"
)

func parseGerritWebhook(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	var event struct {
		Type   string `json:"type"`
		Change struct {
			Project string `json:"project"`
			Number  int    `json:"number"`
			Subject string `json:"subject"`
			Status  string `json:"status"`
			Branch  string `json:"branch"`
			Owner   struct {
				Name     string `json:"name"`
				Username string `json:"username"`
				Email    string `json:"email"`
			} `json:"owner"`
		} `json:"change"`
		PatchSet struct {
			Number int    `json:"number"`
			Ref    string `json:"ref"`
		} `json:"patchSet"`
		Author struct {
			Name     string `json:"name"`
			Username string `json:"username"`
			Email    string `json:"email"`
		} `json:"author"`
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return "", nil, fmt.Errorf("failed to parse gerrit webhook: %w", err)
	}

	pr := &models.PRRecord{
		RepoGroup: repoGroup,
		Platform:  "gerrit",
		PRNumber:  event.Change.Number,
		Title:     event.Change.Subject,
	}
	if event.Change.Owner.Name != "" {
		pr.Author = event.Change.Owner.Name
	} else if event.Change.Owner.Username != "" {
		pr.Author = event.Change.Owner.Username
	}

	switch event.Change.Status {
	case "NEW", "DRAFT":
		pr.State = "open"
	case "MERGED":
		pr.State = "merged"
	case "ABANDONED":
		pr.State = "closed"
	}

	switch event.Type {
	case "patchset-created":
		return string(events.EventPROpened), pr, nil
	case "change-merged":
		return string(events.EventPRMerged), pr, nil
	case "change-abandoned":
		return string(events.EventPRClosed), pr, nil
	case "change-restored":
		return string(events.EventPRReopened), pr, nil
	case "comment-added":
		if event.Comment != "" {
			return string(events.EventPRComment), pr, nil
		}
	}

	return "", nil, nil
}
