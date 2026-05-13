package webhook

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"asika/common/db"
	"asika/common/models"
)

var notifyFn func(title, body string)

// SetNotifyFunc sets the notification function (called from handlers package).
func SetNotifyFunc(fn func(title, body string)) {
	notifyFn = fn
}

var (
	retryWorkerStop   = make(chan struct{})
	retryWorkerStopMu sync.Mutex
)

func StopWebhookRetryWorker() {
	retryWorkerStopMu.Lock()
	defer retryWorkerStopMu.Unlock()
	if retryWorkerStop != nil {
		select {
		case <-retryWorkerStop:
			// already closed
		default:
			close(retryWorkerStop)
		}
		retryWorkerStop = nil
	}
}

func StartWebhookRetryWorker() {
	StopWebhookRetryWorker()
	retryWorkerStopMu.Lock()
	retryWorkerStop = make(chan struct{})
	retryWorkerStopMu.Unlock()
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
			case <-retryWorkerStop:
				slog.Info("webhook retry worker stopped")
				return
			}
			retries, err := db.GetDueWebhookRetries(time.Now())
			if err != nil {
				slog.Error("failed to get due webhook retries", "error", err)
				continue
			}

			for _, retry := range retries {
				slog.Info("retrying webhook", "id", retry.ID, "repo_group", retry.RepoGroup, "platform", retry.Platform, "fail_count", retry.FailCount)

				_, _, err := ProcessWebhook(retry.Platform, retry.RepoGroup, retry.Body)
				if err != nil {
					slog.Warn("webhook retry failed", "id", retry.ID, "error", err, "fail_count", retry.FailCount)

					retry.FailCount++
					retry.LastError = err.Error()
					retry.LastFailed = time.Now()
					backoff := time.Duration(1<<uint(min(retry.FailCount, 10))) * time.Second
					if backoff > time.Hour {
						backoff = time.Hour
					}
					retry.NextRetry = time.Now().Add(backoff)

					if retry.FailCount >= 10 {
						slog.Error("webhook retry max attempts reached, giving up", "id", retry.ID)
						db.DeleteWebhookRetry(retry.ID)
						notifyWebhookPermanentFailure(retry)
						continue
					}

					db.PutWebhookRetry(retry)
					continue
				}

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
	if notifyFn != nil {
		notifyFn(title, body)
	}
}
