package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/common/utils"
)

// SpamDetector detects and handles spam PRs
type SpamDetector struct {
	cfg      *models.Config
	clients  map[platforms.PlatformType]platforms.PlatformClient
	stop     chan struct{}
	stopOnce sync.Once
}

// NewSpamDetector creates a new spam detector
func NewSpamDetector(cfg *models.Config) *SpamDetector {
	// Clients are set later via SetClients
	return &SpamDetector{
		cfg:  cfg,
		stop: make(chan struct{}),
	}
}

// NewSpamDetectorWithClients creates a new spam detector with platform clients
func NewSpamDetectorWithClients(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *SpamDetector {
	return &SpamDetector{
		cfg:     cfg,
		clients: clients,
		stop:    make(chan struct{}),
	}
}

// SetClients sets the platform clients
func (d *SpamDetector) SetClients(clients map[platforms.PlatformType]platforms.PlatformClient) {
	d.clients = clients
}

// Scan scans for spam PRs (called periodically)
func (d *SpamDetector) Scan() {
	if !d.cfg.Spam.Enabled {
		return
	}

	window := utils.ParseDuration(d.cfg.Spam.TimeWindow, 10*time.Minute)
	cutoff := time.Now().Add(-window)
	prs := d.getPRsAfter(cutoff)
	spamPRs := d.detectSpam(prs)

	for _, pr := range spamPRs {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		d.HandleSpamWithContext(ctx, pr, pr.RepoGroup)
		cancel()
		events.PublishPR(events.EventSpamDetected, pr.RepoGroup, pr.Platform, pr, nil)
	}

	if len(spamPRs) > 0 {
		slog.Warn("spam scan results", "total_prs", len(prs), "spam_detected", len(spamPRs))
	}
}

// getPRsAfter gets PRs created after a certain time from bbolt
func (d *SpamDetector) getPRsAfter(after time.Time) []*models.PRRecord {
	prs := make([]*models.PRRecord, 0)
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return err
		}
		if pr.CreatedAt.After(after) && !pr.SpamFlag {
			prs = append(prs, &pr)
		}
		return nil
	})
	return prs
}

// detectSpam detects spam based on configured rules
func (d *SpamDetector) detectSpam(prs []*models.PRRecord) []*models.PRRecord {
	spamMap := make(map[string]*models.PRRecord)

	// Check by author: too many PRs by same author in time window
	if d.cfg.Spam.TriggerOnAuthor {
		authorCount := make(map[string][]*models.PRRecord)
		for _, pr := range prs {
			authorCount[pr.Author] = append(authorCount[pr.Author], pr)
		}
		for _, prList := range authorCount {
			if len(prList) >= d.cfg.Spam.Threshold {
				for _, pr := range prList {
					spamMap[pr.ID] = pr
				}
			}
		}
	}

	// Check by keywords in title
	for _, pr := range prs {
		for _, keyword := range d.cfg.Spam.TriggerOnTitleKw {
			if strings.Contains(strings.ToLower(pr.Title), strings.ToLower(keyword)) {
				spamMap[pr.ID] = pr
				break
			}
		}
	}

	result := make([]*models.PRRecord, 0, len(spamMap))
	for _, pr := range spamMap {
		result = append(result, pr)
	}
	return result
}

// HandleSpam marks a PR as spam, closes it via platform API, and sends notifications.
// Uses context.Background() for backward compatibility.
func (d *SpamDetector) HandleSpam(pr *models.PRRecord, repoGroup string) {
	d.HandleSpamWithContext(context.Background(), pr, repoGroup)
}

// HandleSpamWithContext marks a PR as spam with a context for timeout/cancellation control.
func (d *SpamDetector) HandleSpamWithContext(ctx context.Context, pr *models.PRRecord, repoGroup string) {
	// Mark as spam in bbolt
	pr.SpamFlag = true
	pr.State = "spam"
	pr.UpdatedAt = time.Now()
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	data, marshalErr := json.Marshal(pr)
	if marshalErr != nil {
		slog.Error("failed to marshal spam PR", "error", marshalErr, "pr_id", pr.ID)
	} else {
		db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	}

	// Close PR via platform API
	if d.clients != nil {
		client := d.clients[platforms.PlatformType(pr.Platform)]
		if client != nil {
			group := config.GetRepoGroupByName(d.cfg, repoGroup)
			if group != nil {
				owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
				if owner != "" && repo != "" {
					if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
						slog.Error("failed to close spam PR", "error", err)
					} else {
						slog.Info("spam PR closed", "platform", pr.Platform, "pr_number", pr.PRNumber)
					}
					// Comment explaining spam detection
					body := fmt.Sprintf("This PR has been marked as **spam** by Asika. Author: %s. If this is a mistake, contact an admin.", pr.Author)
					if err := client.CommentPR(ctx, owner, repo, pr.PRNumber, body); err != nil {
						slog.Warn("failed to comment on spam PR", "error", err)
					}
				}
			}
		}
	}

	// Send notifications with timeout
	notifCtx, notifCancel := context.WithTimeout(ctx, 15*time.Second)
	defer notifCancel()
	d.sendSpamNotificationWithContext(notifCtx, pr)
	slog.Warn("spam handled", "pr_id", pr.ID, "author", pr.Author)
}

// sendSpamNotification sends notifications via all configured channels.
// Uses context.Background() for backward compatibility.
func (d *SpamDetector) sendSpamNotification(pr *models.PRRecord) {
	d.sendSpamNotificationWithContext(context.Background(), pr)
}

// sendSpamNotificationWithContext sends notifications with a context for timeout/cancellation control.
func (d *SpamDetector) sendSpamNotificationWithContext(ctx context.Context, pr *models.PRRecord) {
	for _, nc := range d.cfg.Notify {
		var n notifier.Notifier
		switch nc.Type {
		case "smtp":
			n = notifier.NewSMTPNotifier(nc.Config)
		case "wecom":
			n = notifier.NewWeComNotifier(nc.Config)
		case "github_at":
			n = notifier.NewGitHubAtNotifier(nc.Config)
		case "gitlab_at":
			n = notifier.NewGitLabAtNotifier(nc.Config)
		case "gitea_at":
			n = notifier.NewGiteaAtNotifier(nc.Config)
		case "dingtalk":
			n = notifier.NewDingTalkNotifier(nc.Config)
		default:
			continue
		}
		if n == nil {
			continue
		}

		title := fmt.Sprintf("[Spam Alert] PR #%d by %s", pr.PRNumber, pr.Author)
		body := fmt.Sprintf("PR #%d \"%s\" by %s marked as spam.\nRepo: %s | Platform: %s", pr.PRNumber, pr.Title, pr.Author, pr.RepoGroup, pr.Platform)

		if err := n.Send(ctx, title, body); err != nil {
			slog.Error("notification failed", "type", nc.Type, "error", err)
		}
	}
}

// Stop signals the periodic scan goroutine to stop.
func (d *SpamDetector) Stop() {
	d.stopOnce.Do(func() {
		close(d.stop)
	})
}

// StopChan returns the stop channel for external select loops.
func (d *SpamDetector) StopChan() <-chan struct{} {
	return d.stop
}
