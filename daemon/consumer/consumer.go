package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

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
	ctx          context.Context
	cancel       context.CancelFunc

	// Actor subsystems
	writer  *writerActor
	workers *workerPool
}

// NewConsumer creates a new event consumer (basic, no wiring)
func NewConsumer() *Consumer {
	c := &Consumer{
		stop:    make(chan struct{}),
		writer:  newWriterActor(256),
		workers: newWorkerPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"}),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c
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
	c := &Consumer{
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
	s.SetRecordWriter(&syncRecordWriter{writer: c.writer})
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c
}

// syncRecordWriter implements syncer.SyncRecordWriter through the writer actor.
type syncRecordWriter struct {
	writer *writerActor
}

func (w *syncRecordWriter) WriteSyncRecord(recordID string, data []byte) error {
	return w.writer.write(recordID, data, "", "", 0)
}

// Start starts consuming events and dispatching to subsystem goroutine pools.
// Safe to call multiple times: stops previous goroutines before restarting.
func (c *Consumer) Start() {
	c.Stop()
	c.stop = make(chan struct{})
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.writer = newWriterActor(256)
	if c.syncer != nil {
		c.syncer.SetRecordWriter(&syncRecordWriter{writer: c.writer})
	}
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
// Safe to call multiple times.
func (c *Consumer) Stop() {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
	if c.workers != nil {
		c.workers.Stop()
		c.workers = nil
	}
	if c.writer != nil {
		c.writer.Stop()
		c.writer = nil
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

// updatePR serializes the PR and writes it through the writer actor.
func (c *Consumer) updatePR(event events.Event, pr *models.PRRecord) {
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
