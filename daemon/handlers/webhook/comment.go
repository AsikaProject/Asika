package webhook

import (
	"encoding/json"
	"regexp"

	"asika/common/models"
)

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_\-\[\]]+)`)

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
		return &models.PRCommentPayload{CommentBody: payload.Comment.Body, CommentAuthor: payload.Comment.User.Login, Mentions: extractMentions(payload.Comment.Body)}
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
		return &models.PRCommentPayload{CommentBody: payload.ObjectAttributes.Note, CommentAuthor: payload.User.Username, Mentions: extractMentions(payload.ObjectAttributes.Note)}
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
		return &models.PRCommentPayload{CommentBody: payload.Comment.Body, CommentAuthor: payload.Comment.User.Login, Mentions: extractMentions(payload.Comment.Body)}
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
		return &models.PRCommentPayload{CommentBody: payload.Comment.Content.Raw, CommentAuthor: payload.Actor.DisplayName, Mentions: extractMentions(payload.Comment.Content.Raw)}
	}
	return nil
}

func extractMentions(text string) []string {
	matches := mentionRe.FindAllString(text, -1)
	seen := make(map[string]bool)
	var mentions []string
	for _, m := range matches {
		username := m[1:]
		if !seen[username] {
			seen[username] = true
			mentions = append(mentions, username)
		}
	}
	return mentions
}
