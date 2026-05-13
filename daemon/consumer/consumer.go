package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"asika/common/config"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/handlers"
	"asika/daemon/labeler"
	"asika/daemon/queue"
	"asika/daemon/reviewer"
	"asika/daemon/stale"
	"asika/daemon/syncer"
)

// Consumer consumes events and dispatches them to subsystem goroutine pools.
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

	// Actor subsystems
	writer  *writerActor
	workers *workerPool
}

// NewConsumer creates a new event consumer (basic, no wiring)
func NewConsumer() *Consumer {
	return &Consumer{
		stop:    make(chan struct{}),
		writer:  newWriterActor(256),
		workers: newWorkerPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"}),
	}
}

// NewConsumerWithClients creates a fully wired event consumer
func NewConsumerWithClients(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Consumer {
	l := labeler.NewLabeler(clients)
	r := reviewer.NewReviewer(clients)
	s := syncer.NewSyncer(cfg, clients)
	s.SetNotifyFunc(handlers.SendNotificationSync)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)
	q := queue.NewManager(cfg, clients)
	poolCfg := cfg.WorkerPool
	if poolCfg.MinWorkers <= 0 {
		poolCfg = models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"}
	}
	return &Consumer{
		cfg:          cfg,
		clients:      clients,
		labeler:      l,
		reviewer:     r,
		syncer:       s,
		spamDetector: sd,
		queue:        q,
		stop:         make(chan struct{}),
		writer:       newWriterActor(256),
		workers:      newWorkerPool(poolCfg),
	}
}

// Start starts consuming events and dispatching to subsystem goroutine pools.
// Safe to call multiple times: stops previous goroutines before restarting.
func (c *Consumer) Start() {
	c.Stop()
	c.stop = make(chan struct{})
	c.writer = newWriterActor(256)
	poolCfg := models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"}
	if c.cfg != nil && c.cfg.WorkerPool.MinWorkers > 0 {
		poolCfg = c.cfg.WorkerPool
	}
	c.workers = newWorkerPool(poolCfg)
	ch := events.Subscribe()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("event consumer panic recovered", "error", r)
			}
		}()
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					slog.Info("event channel closed, consumer stopping")
					return
				}
				c.dispatch(event)
			case <-c.stop:
				slog.Info("event consumer stopped")
				return
			}
		}
	}()
}

// Stop stops the consumer and all subsystem goroutines.
// It waits for the dispatch goroutine to exit before returning.
func (c *Consumer) Stop() {
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
	if c.workers != nil {
		c.workers.Stop()
	}
	if c.writer != nil {
		c.writer.Stop()
	}
}

// UpdateWorkerPoolConfig updates the worker pool configuration at runtime
func (c *Consumer) UpdateWorkerPoolConfig(cfg models.WorkerPoolConfig) {
	if c.workers != nil {
		c.workers.UpdateConfig(cfg)
	}
}

// SetStaleManager sets the stale manager for activity detection
func (c *Consumer) SetStaleManager(mgr *stale.Manager) {
	c.staleMgr = mgr
}

// dispatch routes events to subsystem goroutine pools
func (c *Consumer) dispatch(event events.Event) {
	slog.Info("received event", "type", event.Type, "repo_group", event.RepoGroup, "platform", event.Platform)

	switch event.Type {
	case events.EventPROpened:
		c.workers.Submit(func() { c.handlePROpened(event) })
	case events.EventPRClosed:
		c.workers.Submit(func() { c.handlePRClosed(event) })
	case events.EventPRMerged:
		c.workers.Submit(func() { c.handlePRMerged(event) })
	case events.EventPRApproved:
		c.workers.Submit(func() { c.handlePRApproved(event) })
	case events.EventPRReopened:
		c.workers.Submit(func() { c.handlePRReopened(event) })
	case events.EventPRReverted:
		c.workers.Submit(func() { c.handlePRReverted(event) })
	case events.EventSpamDetected:
		c.workers.Submit(func() { c.handleSpamDetected(event) })
	case events.EventPRComment:
		c.workers.Submit(func() { c.handlePRComment(event) })
	case events.EventPRLabeled:
		c.workers.Submit(func() { c.handlePRLabeled(event) })
	case events.EventBranchDeleted:
		c.workers.Submit(func() { c.handleBranchDeleted(event) })
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

	if pr.ID == "" {
		pr.ID = uuid.New().String()
	}
	pr.CreatedAt = time.Now()
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "opened",
		Actor:     pr.Author,
	})
	key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
	data, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal PR", "error", err)
		return
	}

	// Use writer actor for bbolt writes
	if err := c.writer.write(key, data, pr.ID, event.RepoGroup, pr.PRNumber); err != nil {
		slog.Error("failed to store PR", "error", err)
		return
	}

	// These can run in parallel via separate goroutines
	if c.labeler != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("labeler panic recovered", "error", r, "pr_number", pr.PRNumber, "repo_group", event.RepoGroup)
				}
			}()
			c.labeler.HandlePROpened(pr, event.RepoGroup)
		}()
	}
	if c.reviewer != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("reviewer panic recovered", "error", r, "pr_number", pr.PRNumber, "repo_group", event.RepoGroup)
				}
			}()
			c.reviewer.HandlePROpened(pr, event.RepoGroup)
		}()
	}
	if c.staleMgr != nil {
		c.staleMgr.HandleActivity(pr, event.RepoGroup)
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("syncPRLinks panic recovered", "error", r, "pr_number", pr.PRNumber)
			}
		}()
		syncPRLinks(c.writer, pr)
	}()
}

func syncPRLinks(w *writerActor, pr *models.PRRecord) {
	links := parseIssueLinksFromPR(pr)
	for _, link := range links {
		if err := w.writeIssueLink(&link); err != nil {
			slog.Error("failed to store issue-PRLink", "error", err, "pr_id", pr.ID)
		}
	}
}

func (c *Consumer) handlePRClosed(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR closed", "title", pr.Title)

	pr.State = "closed"
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "closed",
		Actor:     "system",
	})
	key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
	data, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal PR", "error", err)
		return
	}
	if err := c.writer.write(key, data, pr.ID, event.RepoGroup, pr.PRNumber); err != nil {
		slog.Error("failed to update PR", "error", err)
	}
}

func (c *Consumer) handlePRMerged(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR merged", "title", pr.Title)

	pr.State = "merged"
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "merged",
		Actor:     "system",
	})
	key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
	data, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal PR", "error", err)
		return
	}
	if err := c.writer.write(key, data, pr.ID, event.RepoGroup, pr.PRNumber); err != nil {
		slog.Error("failed to update PR", "error", err)
	}

	// Trigger code sync in background
	if c.syncer != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := c.syncer.SyncOnMerge(ctx, pr); err != nil {
				slog.Error("sync failed", "error", err, "repo_group", event.RepoGroup)
			}
		}()
	}

	// Check cross-space dependencies
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("NotifyCrossSpaceDeps panic recovered", "error", r, "pr_number", pr.PRNumber)
			}
		}()
		handlers.NotifyCrossSpaceDeps(pr)
	}()

	// Update PR stack member state
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("UpdateStackMemberStateOnMerge panic recovered", "error", r, "pr_number", pr.PRNumber)
			}
		}()
		handlers.UpdateStackMemberStateOnMerge(pr)
	}()
}

func (c *Consumer) handlePRApproved(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR approved", "title", pr.Title)

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

	pr.SpamFlag = true
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "marked_spam",
		Actor:     "system",
	})
	key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
	data, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal PR", "error", err)
		return
	}
	if err := c.writer.write(key, data, pr.ID, event.RepoGroup, pr.PRNumber); err != nil {
		slog.Error("failed to update PR", "error", err)
	}

	if c.spamDetector != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			c.spamDetector.HandleSpamWithContext(ctx, pr, event.RepoGroup)
		}()
	}
}

func (c *Consumer) handlePRLabeled(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR labeled", "repo_group", event.RepoGroup, "pr_number", pr.PRNumber)

	if label, ok := event.Payload.(string); ok && label != "" {
		found := false
		for _, l := range pr.Labels {
			if l == label {
				found = true
				break
			}
		}
		if !found {
			pr.Labels = append(pr.Labels, label)
		}
	}

	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "labeled",
		Actor:     "system",
	})
	key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
	data, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal PR", "error", err)
		return
	}
	if err := c.writer.write(key, data, pr.ID, event.RepoGroup, pr.PRNumber); err != nil {
		slog.Error("failed to update PR", "error", err)
	}
}

func (c *Consumer) handlePRReopened(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR reopened (spam recovery)", "title", pr.Title, "repo_group", pr.RepoGroup)

	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "reopened",
		Actor:     "system",
	})
	key := fmt.Sprintf("%s#%s#%d", event.RepoGroup, event.Platform, pr.PRNumber)
	data, err := json.Marshal(pr)
	if err != nil {
		slog.Error("failed to marshal PR", "error", err)
		return
	}
	if err := c.writer.write(key, data, pr.ID, event.RepoGroup, pr.PRNumber); err != nil {
		slog.Error("failed to update PR", "error", err)
	}

	if c.staleMgr != nil {
		c.staleMgr.HandleActivity(pr, event.RepoGroup)
	}

	if c.syncer != nil {
		go func() {
			ctx := context.Background()
			if err := c.syncer.SyncOnMerge(ctx, pr); err != nil {
				slog.Error("failed to sync spam-reopened PR", "error", err, "pr_id", pr.ID)
			}
		}()
	}
}

func (c *Consumer) handlePRReverted(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}
	slog.Info("PR reverted", "title", pr.Title, "pr_number", pr.PRNumber, "repo_group", event.RepoGroup)
}

func (c *Consumer) handlePRComment(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	payload, ok := event.Payload.(*models.PRCommentPayload)
	if !ok || payload == nil {
		slog.Warn("pr_comment event missing payload", "repo_group", event.RepoGroup, "pr", pr.PRNumber)
		return
	}

	commentBody := strings.TrimSpace(payload.CommentBody)
	commentAuthor := payload.CommentAuthor

	slog.Info("PR comment received", "repo_group", event.RepoGroup, "pr", pr.PRNumber, "author", commentAuthor, "body", commentBody)

	if !strings.HasPrefix(commentBody, "/") {
		return
	}

	group := config.GetRepoGroupByName(c.cfg, pr.RepoGroup)
	if group == nil {
		slog.Warn("repo group not found for comment command", "repo_group", pr.RepoGroup)
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		slog.Warn("cannot resolve repo for comment command", "platform", pr.Platform, "repo_group", pr.RepoGroup)
		return
	}

	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		slog.Warn("no client for platform", "platform", pr.Platform)
		return
	}

	parts := strings.Fields(commentBody)
	if len(parts) == 0 {
		return
	}
	command := strings.ToLower(parts[0])
	args := parts[1:]

	ctx := context.Background()
	var result string

	switch command {
	case "/approve":
		result = c.cmdApprove(ctx, client, pr, owner, repo, commentAuthor)
	case "/close":
		result = c.cmdClose(ctx, client, pr, owner, repo, commentAuthor)
	case "/reopen":
		result = c.cmdReopen(ctx, client, pr, owner, repo, commentAuthor)
	case "/merge":
		result = c.cmdMerge(ctx, client, pr, owner, repo, group, commentAuthor)
	case "/spam":
		result = c.cmdSpam(ctx, client, pr, owner, repo, commentAuthor)
	case "/rebase":
		result = c.cmdRebase(ctx, client, pr, owner, repo, commentAuthor)
	case "/cherry-pick":
		result = c.cmdCherryPick(ctx, client, pr, owner, repo, args, commentAuthor)
	case "/queue":
		result = c.cmdQueue(pr, commentAuthor)
	case "/recheck":
		result = c.cmdRecheck(pr, commentAuthor)
	case "/help":
		result = "Available commands: /approve, /close, /reopen, /merge, /spam, /rebase, /cherry-pick, /queue, /recheck, /help"
	default:
		slog.Info("unknown comment command", "command", command, "pr", pr.PRNumber)
		return
	}

	if result != "" {
		reply := fmt.Sprintf("@%s %s", commentAuthor, result)
		if err := client.CommentPR(ctx, owner, repo, pr.PRNumber, reply); err != nil {
			slog.Error("failed to post command result comment", "error", err, "pr", pr.PRNumber)
		}
	}
}

func (c *Consumer) cmdApprove(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if err := client.ApprovePR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to approve: %v", err)
	}
	return fmt.Sprintf("PR #%d approved by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdClose(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to close: %v", err)
	}
	return fmt.Sprintf("PR #%d closed by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdReopen(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if err := client.ReopenPR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to reopen: %v", err)
	}
	return fmt.Sprintf("PR #%d reopened by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdMerge(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo string, group *models.RepoGroup, author string) string {
	method, err := client.GetDefaultMergeMethod(ctx, owner, repo)
	if err != nil {
		method = "merge"
	}
	if err := client.MergePR(ctx, owner, repo, pr.PRNumber, method); err != nil {
		return fmt.Sprintf("Failed to merge: %v", err)
	}
	return fmt.Sprintf("PR #%d merged by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdSpam(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if c.spamDetector != nil {
		c.spamDetector.HandleSpam(pr, pr.RepoGroup)
	}
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to mark as spam: %v", err)
	}
	return fmt.Sprintf("PR #%d marked as spam and closed by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdRebase(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	return "Rebase via comment is not yet supported. Please use the web UI or bot commands."
}

func (c *Consumer) cmdCherryPick(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo string, args []string, author string) string {
	if len(args) == 0 {
		return "Usage: /cherry-pick <target-branch>"
	}
	targetBranch := args[0]
	_ = targetBranch
	return fmt.Sprintf("Cherry-pick via comment is not yet supported. Please use the web UI or bot commands.")
}

func (c *Consumer) cmdQueue(pr *models.PRRecord, author string) string {
	if c.queue != nil {
		if err := c.queue.AddToQueue(pr); err != nil {
			return fmt.Sprintf("Failed to add to queue: %v", err)
		}
	}
	return fmt.Sprintf("PR #%d added to merge queue by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdRecheck(pr *models.PRRecord, author string) string {
	if c.queue != nil {
		c.queue.CheckQueue()
	}
	return fmt.Sprintf("Queue recheck triggered by %s via comment.", author)
}

func (c *Consumer) handleBranchDeleted(event events.Event) {
	branch, ok := event.Payload.(string)
	if !ok || branch == "" {
		slog.Warn("branch deleted event missing branch name")
		return
	}

	slog.Info("branch deleted", "branch", branch, "repo_group", event.RepoGroup)

	if c.syncer != nil {
		go c.syncer.SyncBranchDeletion(event.RepoGroup, event.Platform, branch)
	}
}

var issueRefPattern = regexp.MustCompile(`(?i)(?:fixes|closes|resolves|references?)\s*:?\s*(?:([a-zA-Z0-9_-]+)\/([a-zA-Z0-9_-]+))?#(\d+)`)

func parseIssueLinksFromPR(pr *models.PRRecord) []models.IssuePRLink {
	combined := pr.Title
	if pr.Body != "" {
		combined += "\n" + pr.Body
	}

	matches := issueRefPattern.FindAllStringSubmatch(combined, -1)
	if len(matches) == 0 {
		return nil
	}

	var links []models.IssuePRLink
	seen := make(map[string]bool)

	for _, m := range matches {
		linkType := strings.ToLower(strings.TrimSpace(m[0]))
		for _, kw := range []string{"fixes", "closes", "resolves", "references", "reference", "refs", "ref"} {
			if strings.HasPrefix(linkType, kw) {
				linkType = kw
				break
			}
		}
		if linkType == "reference" || linkType == "references" || linkType == "refs" || linkType == "ref" {
			linkType = "related"
		}

		var owner, repo string
		if m[1] != "" && m[2] != "" {
			owner = m[1]
			repo = m[2]
		} else {
			owner = "_"
			repo = "_"
		}

		issueNum := 0
		fmt.Sscanf(m[3], "%d", &issueNum)

		issueID := fmt.Sprintf("%s/%s#%d", owner, repo, issueNum)
		key := fmt.Sprintf("%s:%s", issueID, pr.ID)
		if seen[key] {
			continue
		}
		seen[key] = true

		links = append(links, models.IssuePRLink{
			IssueID:   issueID,
			PRID:      pr.ID,
			RepoGroup: pr.RepoGroup,
			Platform:  pr.Platform,
			LinkType:  linkType,
		})
	}

	return links
}
