package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
	"asika/common/timeutil"
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
	eventCh <-chan events.Event

	// Debounce: random 1-10s delay per PR to avoid API rate limits
	debounceMu     sync.Mutex
	debounceTimers map[string]*time.Timer
}

// NewConsumer creates a new event consumer (basic, no wiring)
func NewConsumer() *Consumer {
	c := &Consumer{
		stop:           make(chan struct{}),
		writer:         newWriterActor(256),
		workers:        newWorkerPool(models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8, ScaleUpPct: 75, ScaleDownPct: 25, CooldownSecs: 30, StatsInterval: "30s"}),
		debounceTimers: make(map[string]*time.Timer),
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
		cfg:            cfg,
		clients:        clients,
		labeler:        l,
		reviewer:       r,
		syncer:         s,
		spamDetector:   sd,
		queue:          q,
		stop:           make(chan struct{}),
		writer:         newWriterActor(256),
		workers:        newWorkerPool(poolCfg),
		debounceTimers: make(map[string]*time.Timer),
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
	return w.writer.writeSyncRecord(recordID, data)
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
	c.eventCh = events.Subscribe()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("event consumer panic recovered", "error", r)
			}
		}()
		for {
			select {
			case event, ok := <-c.eventCh:
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
	if c.eventCh != nil {
		events.Unsubscribe(c.eventCh)
		c.eventCh = nil
	}
	c.debounceMu.Lock()
	for _, timer := range c.debounceTimers {
		timer.Stop()
	}
	c.debounceTimers = make(map[string]*time.Timer)
	c.debounceMu.Unlock()
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

// debounceKey generates a key for debouncing events per PR.
func (c *Consumer) debounceKey(event events.Event) string {
	if event.PR != nil {
		return fmt.Sprintf("%s/%s#%d", event.RepoGroup, event.Platform, event.PR.PRNumber)
	}
	return fmt.Sprintf("%s/%s", event.RepoGroup, event.Platform)
}

// debounce dispatches an event with a random 1-10s delay to avoid API rate limits.
// Multiple events for the same PR within the delay window are coalesced.
func (c *Consumer) debounce(event events.Event, fn func()) {
	key := c.debounceKey(event)

	c.debounceMu.Lock()
	if timer, ok := c.debounceTimers[key]; ok {
		timer.Stop()
	}
	delay := time.Duration(1+rand.Intn(10)) * timeutil.Second
	c.debounceTimers[key] = time.AfterFunc(delay, func() {
		c.debounceMu.Lock()
		delete(c.debounceTimers, key)
		c.debounceMu.Unlock()
		fn()
	})
	c.debounceMu.Unlock()
}

// dispatch routes events to subsystem goroutine pools with debounce.
func (c *Consumer) dispatch(event events.Event) {
	slog.Info("received event", "type", event.Type, "repo_group", event.RepoGroup, "platform", event.Platform)

	switch event.Type {
	case events.EventPROpened:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePROpened(event) }) })
	case events.EventPRClosed:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePRClosed(event) }) })
	case events.EventPRMerged:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePRMerged(event) }) })
	case events.EventPRApproved:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePRApproved(event) }) })
	case events.EventPRReopened:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePRReopened(event) }) })
	case events.EventPRReverted:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePRReverted(event) }) })
	case events.EventSpamDetected:
		c.debounce(event, func() { c.workers.Submit(func() { c.handleSpamDetected(event) }) })
	case events.EventPRComment:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePRComment(event) }) })
	case events.EventPRLabeled:
		c.debounce(event, func() { c.workers.Submit(func() { c.handlePRLabeled(event) }) })
	case events.EventBranchDeleted:
		c.debounce(event, func() { c.workers.Submit(func() { c.handleBranchDeleted(event) }) })
	case events.EventSyncCompleted:
		slog.Info("sync completed", "repo_group", event.RepoGroup)
	case events.EventSyncFailed:
		slog.Error("sync failed", "repo_group", event.RepoGroup, "error", event.Payload)
	}
}
