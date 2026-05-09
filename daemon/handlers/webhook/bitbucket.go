package webhook

import (
	"encoding/json"

	"asika/common/events"
	"asika/common/models"
)

// parseBitbucketWebhook parses Bitbucket Cloud webhook payload
func parseBitbucketWebhook(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	var payload struct {
		Comment struct {
			Content struct {
				Raw string `json:"raw"`
			} `json:"content"`
			User struct {
				DisplayName string `json:"display_name"`
			} `json:"user"`
		} `json:"comment"`
		PullRequest struct {
			ID     int    `json:"id"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Author struct {
				DisplayName string `json:"display_name"`
			} `json:"author"`
		} `json:"pullrequest"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Actor struct {
			DisplayName string `json:"display_name"`
		} `json:"actor"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}

	if payload.Comment.Content.Raw == "" || payload.PullRequest.ID == 0 {
		return "", nil, nil
	}

	pr := &models.PRRecord{
		Platform:  "bitbucket",
		PRNumber:  payload.PullRequest.ID,
		Title:     payload.PullRequest.Title,
		Author:    payload.PullRequest.Author.DisplayName,
		State:     payload.PullRequest.State,
		RepoGroup: repoGroup,
	}

	return string(events.EventPRComment), pr, nil
}
