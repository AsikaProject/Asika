package webhook

import (
	"encoding/json"

	"asika/common/events"
	"asika/common/models"
)

// parseGiteaWebhook parses Gitea/Forgejo webhook payload
func parseGiteaWebhook(body []byte, repoGroup string, platform string) (string, *models.PRRecord, error) {
	var typeCheck struct {
		Action string `json:"action"`
		Issue  struct {
			PullRequest interface{} `json:"pull_request"`
		} `json:"issue"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(body, &typeCheck); err == nil && typeCheck.Comment.Body != "" && typeCheck.Issue.PullRequest != nil {
		return parseGiteaIssueCommentWebhook(body, repoGroup, platform)
	}

	var payload struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		PullRequest struct {
			Title  string `json:"title"`
			State  string `json:"state"`
			Merged bool   `json:"merged"`
			Draft  bool   `json:"draft"`
			Poster struct {
				Login string `json:"login"`
			} `json:"poster"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}

	author := payload.PullRequest.Poster.Login
	if author == "" {
		author = payload.Sender.Login
	}

	if platform == "" {
		platform = "gitea"
	}

	pr := &models.PRRecord{
		Platform:  platform,
		PRNumber:  payload.Number,
		Title:     payload.PullRequest.Title,
		Author:    author,
		State:     payload.PullRequest.State,
		RepoGroup: repoGroup,
		IsDraft:   payload.PullRequest.Draft,
	}

	if payload.PullRequest.Merged {
		pr.State = "merged"
	}

	eventType := ""
	switch payload.Action {
	case "opened":
		eventType = string(events.EventPROpened)
	case "closed":
		if pr.State == "merged" {
			eventType = string(events.EventPRMerged)
		} else {
			eventType = string(events.EventPRClosed)
		}
	case "reopened":
		eventType = string(events.EventPRReopened)
	case "labeled":
		eventType = string(events.EventPRLabeled)
	case "approved":
		eventType = string(events.EventPRApproved)
	}

	return eventType, pr, nil
}

// parseGiteaIssueCommentWebhook handles Gitea/Forgejo issue_comment events for PRs
func parseGiteaIssueCommentWebhook(body []byte, repoGroup string, platform string) (string, *models.PRRecord, error) {
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Draft  bool   `json:"draft"`
			User   struct {
				Login string `json:"login"`
			} `json:"user"`
			PullRequest interface{} `json:"pull_request"`
		} `json:"issue"`
		Comment struct {
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"comment"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}

	if payload.Action != "created" {
		return "", nil, nil
	}

	if payload.Issue.PullRequest == nil {
		return "", nil, nil
	}

	if platform == "" {
		platform = "gitea"
	}

	pr := &models.PRRecord{
		Platform:  platform,
		PRNumber:  payload.Issue.Number,
		Title:     payload.Issue.Title,
		Author:    payload.Issue.User.Login,
		State:     payload.Issue.State,
		RepoGroup: repoGroup,
		IsDraft:   payload.Issue.Draft,
	}

	return string(events.EventPRComment), pr, nil
}
