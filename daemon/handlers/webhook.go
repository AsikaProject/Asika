package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
)

// WebhookHandler handles POST /webhook/:repo_group/:platform
func WebhookHandler(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	platform := c.Param("platform")

	validPlatforms := map[string]bool{"github": true, "gitlab": true, "gitea": true, "forgejo": true, "codeberg": true, "bitbucket": true}
	if !validPlatforms[platform] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported platform: " + platform})
		return
	}

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	pt := platforms.PlatformType(platform)
	client := clients[pt]
	if client == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported platform: " + platform})
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	if !verifyWebhookSignature(platform, client, body, c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	// Generate webhook ID and store in retry queue before processing
	webhookID := uuid.New().String()
	retry := &models.WebhookRetry{
		ID:         webhookID,
		RepoGroup:  repoGroup,
		Platform:   platform,
		Body:       body,
		FailCount:  0,
		LastFailed: time.Time{},
		NextRetry:  time.Time{},
	}
	if err := db.PutWebhookRetry(retry); err != nil {
		slog.Warn("failed to store webhook for retry", "error", err)
	}

	eventType, _, err := processWebhook(platform, repoGroup, body)
	if err != nil {
		slog.Error("failed to process webhook", "error", err, "platform", platform)
		// Update retry entry with failure
		retry.FailCount++
		retry.LastError = err.Error()
		retry.LastFailed = time.Now()
		retry.NextRetry = time.Now().Add(time.Duration(1<<uint(retry.FailCount)) * time.Second)
		db.PutWebhookRetry(retry)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to process webhook"})
		return
	}

	// Success - remove from retry queue
	db.DeleteWebhookRetry(webhookID)

	c.JSON(http.StatusOK, gin.H{"message": "webhook received", "event": eventType})
}

// processWebhook processes a webhook and returns eventType, PR record, and error.
// This is used by both the webhook handler and the retry worker.
func processWebhook(platform, repoGroup string, body []byte) (eventType string, pr *models.PRRecord, err error) {
    eventType, pr, err = parseWebhookEvent(platform, body, repoGroup)
    if err != nil {
        return
    }
    if eventType != "" && pr != nil {
        var payload interface{}
        if events.EventType(eventType) == events.EventPRComment {
            payload = extractCommentPayload(platform, body)
        }
        events.PublishPR(events.EventType(eventType), repoGroup, platform, pr, payload)
        slog.Info("webhook event published", "type", eventType, "repo_group", repoGroup, "platform", platform, "pr", pr.PRNumber)
    }
    return
}

// verifyWebhookSignature verifies the webhook signature based on platform
func verifyWebhookSignature(platform string, client platforms.PlatformClient, body []byte, c *gin.Context) bool {
	switch platform {
	case "github":
		sig := c.GetHeader("X-Hub-Signature-256")
		if sig == "" {
			sig = c.GetHeader("X-Hub-Signature")
		}
		return client.VerifyWebhookSignature(body, sig)
	case "gitlab":
		token := c.GetHeader("X-Gitlab-Token")
		return client.VerifyWebhookSignature(body, token)
	case "gitea":
		sig := c.GetHeader("X-Gitea-Signature")
		return client.VerifyWebhookSignature(body, sig)
	case "forgejo", "codeberg":
		sig := c.GetHeader("X-Gitea-Signature")
		if sig == "" {
			sig = c.GetHeader("X-Forgejo-Signature")
		}
		return client.VerifyWebhookSignature(body, sig)
	case "bitbucket":
		sig := c.GetHeader("X-Hub-Signature")
		return client.VerifyWebhookSignature(body, sig)
	}
	return false
}

// parseWebhookEvent parses the webhook body and returns event type and PR record
func parseWebhookEvent(platform string, body []byte, repoGroup string) (string, *models.PRRecord, error) {
	switch platform {
	case "github":
		return parseGitHubWebhook(body, repoGroup)
	case "gitlab":
		return parseGitLabWebhook(body, repoGroup)
	case "gitea", "forgejo", "codeberg":
		return parseGiteaWebhook(body, repoGroup, platform)
	case "bitbucket":
		return parseBitbucketWebhook(body, repoGroup)
	}
	return "", nil, nil
}

// parseGitHubWebhook parses GitHub webhook payload
func parseGitHubWebhook(body []byte, repoGroup string) (string, *models.PRRecord, error) {
	// Check if this is an issue_comment event on a PR
	var commentCheck struct {
		Action  string `json:"action"`
		Issue   struct {
			PullRequest struct {
				URL string `json:"url"`
			} `json:"pull_request"`
		} `json:"issue"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(body, &commentCheck); err == nil && commentCheck.Action == "created" && commentCheck.Comment.Body != "" && commentCheck.Issue.PullRequest.URL != "" {
		return parseGitHubIssueComment(body, repoGroup)
	}

	// Check if this is a pull_request_review event (for approvals)
	var reviewPayload struct {
		Action      string `json:"action"`
		Review      struct {
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
			Head struct {
				Sha string `json:"sha"`
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

	if err := json.Unmarshal(body, &reviewPayload); err == nil && reviewPayload.Review.State != "" {
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
			State   string `json:"state"`
			Merged  bool   `json:"merged"`
			Draft   bool   `json:"draft"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			Head struct {
				Sha string `json:"sha"`
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
		Author:    payload.PullRequest.User.Login,
		State:     payload.PullRequest.State,
		RepoGroup: repoGroup,
		IsDraft:   payload.PullRequest.Draft,
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
			Number     int    `json:"number"`
			Title      string `json:"title"`
			State      string `json:"state"`
			Draft      bool   `json:"draft"`
			HTMLURL    string `json:"html_url"`
			User       struct {
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
		ObjectKind string `json:"object_kind"`
		EventName  string `json:"event_name"`
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
		ObjectKind string `json:"object_kind"`
		ObjectAttributes struct {
			ID        int    `json:"id"`
			Note      string `json:"note"`
			NoteableType string `json:"noteable_type"`
			Action    string `json:"action"`
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
		Action     string `json:"action"`
		Number     int    `json:"number"`
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
			ID    int    `json:"id"`
			Title string `json:"title"`
			State string `json:"state"`
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

// extractCommentPayload extracts comment data from a webhook payload for PR comment events
func extractCommentPayload(platform string, body []byte) *models.PRCommentPayload {
	switch platform {
	case "github":
		var payload struct {
			Comment struct {
				Body string `json:"body"`
				User struct {
					Login string `json:"login"`
				} `json:"user"`
			} `json:"comment"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil
		}
		return &models.PRCommentPayload{CommentBody: payload.Comment.Body, CommentAuthor: payload.Comment.User.Login}
	case "gitlab":
		var payload struct {
			ObjectAttributes struct {
				Note string `json:"note"`
			} `json:"object_attributes"`
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil
		}
		return &models.PRCommentPayload{CommentBody: payload.ObjectAttributes.Note, CommentAuthor: payload.User.Username}
	case "gitea", "forgejo", "codeberg":
		var payload struct {
			Comment struct {
				Body string `json:"body"`
				User struct {
					Login string `json:"login"`
				} `json:"user"`
			} `json:"comment"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil
		}
		return &models.PRCommentPayload{CommentBody: payload.Comment.Body, CommentAuthor: payload.Comment.User.Login}
	case "bitbucket":
		var payload struct {
			Comment struct {
				Content struct {
					Raw string `json:"raw"`
				} `json:"content"`
			} `json:"comment"`
			Actor struct {
				DisplayName string `json:"display_name"`
			} `json:"actor"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil
		}
		return &models.PRCommentPayload{CommentBody: payload.Comment.Content.Raw, CommentAuthor: payload.Actor.DisplayName}
	}
	return nil
}

// StartWebhookRetryWorker starts a background worker that retries failed webhooks.
// It runs every minute and processes webhooks that are due for retry.
func StartWebhookRetryWorker() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for range ticker.C {
			retries, err := db.GetDueWebhookRetries(time.Now())
			if err != nil {
				slog.Error("failed to get due webhook retries", "error", err)
				continue
			}

			for _, retry := range retries {
				slog.Info("retrying webhook", "id", retry.ID, "repo_group", retry.RepoGroup, "platform", retry.Platform, "fail_count", retry.FailCount)

				_, _, err := processWebhook(retry.Platform, retry.RepoGroup, retry.Body)
				if err != nil {
					slog.Warn("webhook retry failed", "id", retry.ID, "error", err, "fail_count", retry.FailCount)

					retry.FailCount++
					retry.LastError = err.Error()
					retry.LastFailed = time.Now()
					// Exponential backoff: 2^fail_count seconds, max 1 hour
					backoff := time.Duration(1<<uint(min(retry.FailCount, 10))) * time.Second
					if backoff > time.Hour {
						backoff = time.Hour
					}
					retry.NextRetry = time.Now().Add(backoff)

					// Max retries reached (e.g., 10), delete the entry
					if retry.FailCount >= 10 {
						slog.Error("webhook retry max attempts reached, giving up", "id", retry.ID)
						db.DeleteWebhookRetry(retry.ID)
						notifyWebhookPermanentFailure(retry)
						continue
					}

					db.PutWebhookRetry(retry)
					continue
				}

				// Success - remove from retry queue
				slog.Info("webhook retry succeeded", "id", retry.ID)
				db.DeleteWebhookRetry(retry.ID)
			}
		}
	}()
 	slog.Info("webhook retry worker started")
}

func notifyWebhookPermanentFailure(retry *models.WebhookRetry) {
	title := "⚠️ Webhook Permanent Failure"
	body := fmt.Sprintf("Webhook processing has permanently failed after %d retries.\n\nRepo Group: %s\nPlatform: %s\nWebhook ID: %s\nLast Error: %s\nFailed At: %s",
		retry.FailCount, retry.RepoGroup, retry.Platform, retry.ID, retry.LastError, retry.LastFailed.Format(time.RFC3339))
	sendNotifications(title, body)
}
