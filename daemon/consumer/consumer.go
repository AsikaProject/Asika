package consumer

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "time"

    "github.com/google/uuid"

    "asika/common/db"
    "asika/common/events"
    "asika/common/models"
    "asika/common/platforms"
	"asika/daemon/labeler"
	"asika/daemon/queue"
	"asika/daemon/reviewer"
	"asika/daemon/stale"
	"asika/daemon/syncer"
)

// Consumer consumes events and processes them
type Consumer struct {
	cfg          *models.Config
	clients      map[platforms.PlatformType]platforms.PlatformClient
	labeler      *labeler.Labeler
	reviewer     *reviewer.Reviewer
	syncer       *syncer.Syncer
	spamDetector *syncer.SpamDetector
	queue        *queue.Manager
	staleMgr     *stale.Manager
	stop         chan struct{}
}

// NewConsumer creates a new event consumer (basic, no wiring)
func NewConsumer() *Consumer {
	return &Consumer{
		stop: make(chan struct{}),
	}
}

// NewConsumerWithClients creates a fully wired event consumer
func NewConsumerWithClients(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Consumer {
	l := labeler.NewLabeler(clients)
	r := reviewer.NewReviewer(clients)
	s := syncer.NewSyncer(cfg, clients)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)
	q := queue.NewManager(cfg, clients)
	return &Consumer{
		cfg:          cfg,
		clients:      clients,
		labeler:      l,
		reviewer:     r,
		syncer:       s,
		spamDetector: sd,
		queue:        q,
		stop:         make(chan struct{}),
	}
}

// Start starts consuming events
func (c *Consumer) Start() {
	ch := events.Subscribe()
	go func() {
		for {
			select {
			case event := <-ch:
				c.handleEvent(event)
			case <-c.stop:
				slog.Info("event consumer stopped")
				return
			}
		}
	}()
}

// Stop stops the consumer
func (c *Consumer) Stop() {
	close(c.stop)
}

// SetStaleManager sets the stale manager for activity detection
func (c *Consumer) SetStaleManager(mgr *stale.Manager) {
	c.staleMgr = mgr
}

func (c *Consumer) handleEvent(event events.Event) {
	slog.Info("received event", "type", event.Type, "repo_group", event.RepoGroup, "platform", event.Platform)

	switch event.Type {
	case events.EventPROpened:
		c.handlePROpened(event)
	case events.EventPRClosed:
		c.handlePRClosed(event)
	case events.EventPRMerged:
		c.handlePRMerged(event)
	case events.EventPRApproved:
		c.handlePRApproved(event)
	case events.EventPRReopened:
		c.handlePRReopened(event)
	case events.EventSpamDetected:
		c.handleSpamDetected(event)
	case events.EventPRLabeled:
		slog.Info("PR labeled", "repo_group", event.RepoGroup)
	case events.EventBranchDeleted:
		c.handleBranchDeleted(event)
	case events.EventSyncCompleted:
		slog.Info("sync completed", "repo_group", event.RepoGroup)
	case events.EventSyncFailed:
		slog.Error("sync failed", "repo_group", event.RepoGroup, "error", event.Payload)
	}
}

func (c *Consumer) handlePROpened(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR opened", "title", pr.Title, "author", pr.Author)

// 1. Store in bbolt
    if pr.ID == "" {
        pr.ID = uuid.New().String()
    }
    pr.CreatedAt = time.Now()
    pr.UpdatedAt = time.Now()
    key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
    data, _ := json.Marshal(pr)
    db.PutPRWithIndex(key, data, pr.ID, event.RepoGroup, pr.PRNumber)

	// 2. Trigger label rule engine
	if c.labeler != nil {
		c.labeler.HandlePROpened(pr, event.RepoGroup)
	}

	// 3. Trigger reviewer assignment
	if c.reviewer != nil {
		c.reviewer.HandlePROpened(pr, event.RepoGroup)
	}

	// 4. Check for stale activity (remove stale label on new activity)
	if c.staleMgr != nil {
		c.staleMgr.HandleActivity(pr, event.RepoGroup)
	}
}

func (c *Consumer) handlePRClosed(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR closed", "title", pr.Title)

// Update state in bbolt
    pr.State = "closed"
    pr.UpdatedAt = time.Now()
    key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
    data, _ := json.Marshal(pr)
    db.PutPRWithIndex(key, data, pr.ID, event.RepoGroup, pr.PRNumber)
}

func (c *Consumer) handlePRMerged(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR merged", "title", pr.Title)

// Update state in bbolt
    pr.State = "merged"
    pr.UpdatedAt = time.Now()
    key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
    data, _ := json.Marshal(pr)
    db.PutPRWithIndex(key, data, pr.ID, event.RepoGroup, pr.PRNumber)

	// Trigger code sync (multi mode only)
	if c.syncer != nil {
		ctx := context.Background()
		if err := c.syncer.SyncOnMerge(ctx, pr); err != nil {
			slog.Error("sync failed", "error", err, "repo_group", event.RepoGroup)
		}
	}
}

func (c *Consumer) handlePRApproved(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR approved", "title", pr.Title)

	// Add to merge queue if not already there
	if c.queue != nil {
		if err := c.queue.AddToQueue(pr); err != nil {
			slog.Error("failed to add PR to queue", "error", err, "pr_id", pr.ID)
		} else {
			slog.Info("PR added to merge queue", "pr_id", pr.ID, "repo_group", pr.RepoGroup)
		}
	}
}

func (c *Consumer) handleSpamDetected(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Warn("spam detected", "title", pr.Title, "author", pr.Author)

	if c.spamDetector != nil {
		c.spamDetector.HandleSpam(pr, event.RepoGroup)
	}
}

func (c *Consumer) handlePRReopened(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR reopened (spam recovery)", "title", pr.Title, "repo_group", pr.RepoGroup)

// Update state in bbolt - set to open, spam flag cleared
    pr.State = "open"
    pr.SpamFlag = false
    pr.UpdatedAt = time.Now()
    key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
    data, _ := json.Marshal(pr)
    db.PutPRWithIndex(key, data, pr.ID, event.RepoGroup, pr.PRNumber)

	// Check for stale activity (remove stale label on re-open)
	if c.staleMgr != nil {
		c.staleMgr.HandleActivity(pr, event.RepoGroup)
	}

	// Spam reopen: bypass queue, use git cherry-pick to push to target branches
	// This is per tasks.md 7.4: use common Git tools to cherry-pick PR commits
	if c.syncer != nil {
		ctx := context.Background()
		// For spam reopen, we cherry-pick directly without going through merge queue
		// The syncer.SyncOnMerge will handle the cherry-pick for single/multi mode
		if err := c.syncer.SyncOnMerge(ctx, pr); err != nil {
			slog.Error("failed to sync spam-reopened PR", "error", err, "pr_id", pr.ID)
		}
	}
}

func (c *Consumer) handleBranchDeleted(event events.Event) {
	// Payload should contain branch name
	branch, ok := event.Payload.(string)
	if !ok || branch == "" {
		slog.Warn("branch deleted event missing branch name")
		return
	}

	slog.Info("branch deleted", "branch", branch, "repo_group", event.RepoGroup)

	// Sync branch deletion to other platforms (multi mode only)
	if c.syncer != nil {
		c.syncer.SyncBranchDeletion(event.RepoGroup, event.Platform, branch)
	}
}