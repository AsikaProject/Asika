package webhook

import (
	"encoding/json"

	"asika/common/events"
	"asika/common/models"
)

// parseGitLabWebhook parses GitLab webhook payload
func parseGitLabWebhook(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	var kindCheck struct {
		ObjectKind string `json:"object_kind"`
	}
	if err := json.Unmarshal(body, &kindCheck); err != nil {
		return "", nil, err
	}

	if kindCheck.ObjectKind == "note" {
		return parseGitLabNoteWebhook(body, repoGroup)
	}

	var payload struct {
		ObjectKind       string `json:"object_kind"`
		EventName        string `json:"event_name"`
		ObjectAttributes struct {
			IID    int    `json:"iid"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Action string `json:"action"`
			Merged bool   `json:"merged"`
		} `json:"object_attributes"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
		Labels []struct {
			Title string `json:"title"`
		} `json:"labels"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}

	// Detect WIP/Draft PRs by title prefix
	isDraft := false
	title := payload.ObjectAttributes.Title
	if len(title) >= 4 && (title[:4] == "WIP:" || title[:4] == "wip:") {
		isDraft = true
	}
	if len(title) >= 7 && (title[:7] == "Draft:" || title[:7] == "draft:") {
		isDraft = true
	}

	pr := &models.PRRecord{
		Platform:  "gitlab",
		PRNumber:  payload.ObjectAttributes.IID,
		Title:     title,
		Author:    payload.User.Username,
		State:     payload.ObjectAttributes.State,
		RepoGroup: repoGroup,
		IsDraft:   isDraft,
	}

	if payload.ObjectAttributes.Merged {
		pr.State = "merged"
	}

	for _, lbl := range payload.Labels {
		pr.Labels = append(pr.Labels, lbl.Title)
	}

	eventType := ""
	switch payload.ObjectAttributes.State {
	case "opened", "reopened":
		if payload.ObjectAttributes.State == "reopened" {
			eventType = string(events.EventPRReopened)
		} else {
			eventType = string(events.EventPROpened)
		}
	case "closed":
		eventType = string(events.EventPRClosed)
	case "merged":
		eventType = string(events.EventPRMerged)
	}

	return eventType, pr, nil
}

// parseGitLabNoteWebhook handles GitLab Note Hook events for MR comments
func parseGitLabNoteWebhook(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	var payload struct {
		ObjectKind       string `json:"object_kind"`
		ObjectAttributes struct {
			ID           int    `json:"id"`
			Note         string `json:"note"`
			NoteableType string `json:"noteable_type"`
			Action       string `json:"action"`
		} `json:"object_attributes"`
		MergeRequest struct {
			IID   int    `json:"iid"`
			Title string `json:"title"`
			State string `json:"state"`
		} `json:"merge_request"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}

	if payload.ObjectAttributes.NoteableType != "MergeRequest" {
		return "", nil, nil
	}

	if payload.ObjectAttributes.Action != "create" && payload.ObjectAttributes.Action != "" {
		if payload.ObjectAttributes.Action != "create" {
			return "", nil, nil
		}
	}

	pr := &models.PRRecord{
		Platform:  "gitlab",
		PRNumber:  payload.MergeRequest.IID,
		Title:     payload.MergeRequest.Title,
		Author:    payload.User.Username,
		State:     payload.MergeRequest.State,
		RepoGroup: repoGroup,
	}

	return string(events.EventPRComment), pr, nil
}
