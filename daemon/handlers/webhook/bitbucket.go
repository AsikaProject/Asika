package webhook

import (
	"encoding/json"
	"strings"

	"asika/common/events"
	"asika/common/models"
)

// parseBitbucketWebhook parses Bitbucket Cloud webhook payload
func parseBitbucketWebhook(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	var payload struct {
		PullRequest struct {
			ID     int    `json:"id"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Author struct {
				DisplayName string `json:"display_name"`
			} `json:"author"`
			Links struct {
				HTML struct {
					Href string `json:"href"`
				} `json:"html"`
			} `json:"links"`
			Description string `json:"description"`
		} `json:"pullrequest"`
		Comment struct {
			Content struct {
				Raw string `json:"raw"`
			} `json:"content"`
			User struct {
				DisplayName string `json:"display_name"`
			} `json:"user"`
		} `json:"comment"`
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

	if payload.PullRequest.ID == 0 {
		return "", nil, nil
	}

	state := strings.ToLower(payload.PullRequest.State)
	pr := &models.PRRecord{
		Platform:  "bitbucket",
		PRNumber:  payload.PullRequest.ID,
		Title:     payload.PullRequest.Title,
		Author:    payload.PullRequest.Author.DisplayName,
		State:     state,
		RepoGroup: repoGroup,
		Body:      payload.PullRequest.Description,
		HTMLURL:   payload.PullRequest.Links.HTML.Href,
	}

	switch {
	case payload.Comment.Content.Raw != "":
		return string(events.EventPRComment), pr, nil
	case state == "open":
		return string(events.EventPROpened), pr, nil
	case state == "merged", state == "fulfilled":
		return string(events.EventPRMerged), pr, nil
	case state == "declined", state == "closed", state == "rejected":
		return string(events.EventPRClosed), pr, nil
	}

	return "", nil, nil
}
