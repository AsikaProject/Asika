package webhook

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
)

var (
	webhookClients map[platforms.PlatformType]platforms.PlatformClient
	dedupMu        sync.Mutex
)

// SetClients sets the platform clients for the webhook package.
func SetClients(c map[platforms.PlatformType]platforms.PlatformClient) {
	webhookClients = c
}

func extractDeliveryID(platform string, c *gin.Context) string {
	switch platform {
	case "github":
		return c.GetHeader("X-GitHub-Delivery")
	case "gitlab":
		return c.GetHeader("X-Gitlab-Event-ID")
	case "gitea", "forgejo", "codeberg":
		return c.GetHeader("X-Gitea-Delivery")
	case "bitbucket":
		return c.GetHeader("X-Request-UUID")
	case "gerrit":
		return c.GetHeader("X-Gerrit-Event-ID")
	}
	return ""
}

func dedupKey(platform, repoGroup, deliveryID string) string {
	return platform + ":" + repoGroup + ":" + deliveryID
}

func isDuplicateWebhook(platform, repoGroup, deliveryID string) bool {
	if deliveryID == "" {
		return false
	}
	key := dedupKey(platform, repoGroup, deliveryID)
	data, err := db.GetWebhookDedup(key)
	if err != nil || data == nil {
		return false
	}
	return true
}

func markWebhookProcessed(platform, repoGroup, deliveryID string) {
	if deliveryID == "" {
		return
	}
	key := dedupKey(platform, repoGroup, deliveryID)
	db.PutWebhookDedup(key, []byte(time.Now().Format(time.RFC3339)))
}

func WebhookHandler(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	platform := c.Param("platform")

	validPlatforms := map[string]bool{
		"github": true, "gitlab": true, "gitea": true, "forgejo": true,
		"codeberg": true, "bitbucket": true, "gerrit": true,
	}
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
	client := webhookClients[pt]
	if client == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported platform: " + platform})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	body, err := c.GetRawData()
	if err != nil {
		slog.Warn("failed to read webhook body", "error", err, "platform", platform)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	if !verifyWebhookSignature(platform, client, body, c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	deliveryID := extractDeliveryID(platform, c)

	dedupMu.Lock()
	if isDuplicateWebhook(platform, repoGroup, deliveryID) {
		dedupMu.Unlock()
		slog.Info("duplicate webhook, skipping", "delivery_id", deliveryID, "platform", platform, "repo_group", repoGroup)
		c.JSON(http.StatusOK, gin.H{"message": "webhook already processed", "duplicate": true})
		return
	}
	dedupMu.Unlock()

	webhookID := uuid.New().String()
	retry := &models.WebhookRetry{
		ID:         webhookID,
		DeliveryID: deliveryID,
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

	eventType, _, err := ProcessWebhook(platform, repoGroup, body)
	if err != nil {
		slog.Error("failed to process webhook", "error", err, "platform", platform)
		retry.FailCount++
		retry.LastError = err.Error()
		retry.LastFailed = time.Now()
		retry.NextRetry = time.Now().Add(time.Duration(1<<uint(retry.FailCount)) * time.Second)
		db.PutWebhookRetry(retry)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to process webhook"})
		return
	}

	markWebhookProcessed(platform, repoGroup, deliveryID)

	if eventType == "" {
		slog.Info("webhook processed with no actionable event, cleaning up retry record", "webhook_id", webhookID, "platform", platform)
		db.DeleteWebhookRetry(webhookID)
		c.JSON(http.StatusOK, gin.H{"message": "webhook received", "event": ""})
		return
	}

	db.DeleteWebhookRetry(webhookID)
	db.PutWebhookHealth(repoGroup, platform, time.Now())
	c.JSON(http.StatusOK, gin.H{"message": "webhook received", "event": eventType})
}

// ProcessWebhook processes a webhook and returns eventType, PR record, and error.
// This is used by both the webhook handler and the retry worker.
func ProcessWebhook(platform, repoGroup string, body []byte) (eventType string, pr *models.PRRecord, err error) {
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

func verifyWebhookSignature(platform string, client platforms.PlatformClient, body []byte, c *gin.Context) bool {
	switch platform {
	case "github":
		sig := c.GetHeader("X-Hub-Signature-256")
		if sig == "" {
			sig = c.GetHeader("X-Hub-Signature")
		}
		if sig == "" {
			return false
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
	case "gerrit":
		sig := c.GetHeader("X-Gerrit-Signature")
		return client.VerifyWebhookSignature(body, sig)
	}
	return false
}

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
	case "gerrit":
		return parseGerritWebhook(body, repoGroup)
	}
	return "", nil, nil
}
