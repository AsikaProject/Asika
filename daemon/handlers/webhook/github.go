package webhook

import (
	"encoding/json"
	"fmt"

	"asika/common/events"
	"asika/common/models"
)

// parseGitHubWebhook parses GitHub webhook payload.
// Uses a single-pass approach: first detect event type with a minimal struct,
// then parse the full payload only once.
func parseGitHubWebhook(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	// Single unmarshal to detect event type and extract common fields
	var eventDetect struct {
		Action  string `json:"action"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
		Issue struct {
			PullRequest struct {
				URL string `json:"url"`
			} `json:"pull_request"`
		} `json:"issue"`
		Review struct {
			State string `json:"state"`
		} `json:"review"`
	}
	if err := json.Unmarshal(body, &eventDetect); err != nil {
		return "", nil, fmt.Errorf("failed to parse webhook: %w", err)
	}

	// Route to specific parser based on detected event type
	if eventDetect.Action == "created" && eventDetect.Comment.Body != "" && eventDetect.Issue.PullRequest.URL != "" {
		return parseGitHubIssueComment(body, repoGroup)
	}

	if eventDetect.Review.State != "" {
		// This is a pull_request_review event — parse full payload once
		var reviewPayload struct {
			Action string `json:"action"`
			Review struct {
				State string `json:"state"`
				User  struct {
					Login string `json:"login"`
				} `json:"user"`
			} `json:"review"`
			PullRequest struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				State   string `json:"state"`
				Merged  bool   `json:"merged"`
				Draft   bool   `json:"draft"`
				HTMLURL string `json:"html_url"`
				User    struct {
					Login string `json:"login"`
				} `json:"user"`
				Labels []struct {
					Name string `json:"name"`
				} `json:"labels"`
			} `json:"pull_request"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &reviewPayload); err != nil {
			return "", nil, err
		}
		// This is a pull_request_review event
		pr := &models.PRRecord{
			Platform:  "github",
			PRNumber:  reviewPayload.PullRequest.Number,
			Title:     reviewPayload.PullRequest.Title,
			Author:    reviewPayload.PullRequest.User.Login,
			State:     reviewPayload.PullRequest.State,
			RepoGroup: repoGroup,
			IsDraft:   reviewPayload.PullRequest.Draft,
		}

		if reviewPayload.PullRequest.Merged {
			pr.State = "merged"
		}

		for _, lbl := range reviewPayload.PullRequest.Labels {
			pr.Labels = append(pr.Labels, lbl.Name)
		}

		if reviewPayload.Review.State == "approved" {
			return string(events.EventPRApproved), pr, nil
		}
		return "", pr, nil
	}

	// Regular pull_request event
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			Body    string `json:"body"`
			State   string `json:"state"`
			Merged  bool   `json:"merged"`
			Draft   bool   `json:"draft"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			Head struct {
				Sha string `json:"sha"`
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}

	pr := &models.PRRecord{
		Platform:  "github",
		PRNumber:  payload.PullRequest.Number,
		Title:     payload.PullRequest.Title,
		Body:      payload.PullRequest.Body,
		Author:    payload.PullRequest.User.Login,
		State:     payload.PullRequest.State,
		RepoGroup: repoGroup,
		IsDraft:   payload.PullRequest.Draft,
		BranchInfo: &models.PRBranchInfo{
			HeadBranch: payload.PullRequest.Head.Ref,
			BaseBranch: payload.PullRequest.Base.Ref,
			HeadSHA:    payload.PullRequest.Head.Sha,
		},
	}

	if payload.PullRequest.Merged {
		pr.State = "merged"
	}

	for _, lbl := range payload.PullRequest.Labels {
		pr.Labels = append(pr.Labels, lbl.Name)
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

// parseGitHubIssueComment handles GitHub issue_comment events for PRs
func parseGitHubIssueComment(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			State   string `json:"state"`
			Draft   bool   `json:"draft"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			PullRequest struct {
				URL string `json:"url"`
			} `json:"pull_request"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
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

	if payload.Issue.PullRequest.URL == "" {
		return "", nil, nil
	}

	pr := &models.PRRecord{
		Platform:  "github",
		PRNumber:  payload.Issue.Number,
		Title:     payload.Issue.Title,
		Author:    payload.Issue.User.Login,
		State:     payload.Issue.State,
		RepoGroup: repoGroup,
		IsDraft:   payload.Issue.Draft,
	}

	for _, lbl := range payload.Issue.Labels {
		pr.Labels = append(pr.Labels, lbl.Name)
	}

	return string(events.EventPRComment), pr, nil
}
