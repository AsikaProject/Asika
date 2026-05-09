package webhook

import (
	"encoding/json"

	"asika/common/models"
)

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
