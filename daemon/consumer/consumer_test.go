package consumer

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/daemon/syncer"
	"asika/testutil"
)

func TestNewConsumer(t *testing.T) {
	c := NewConsumer()
	if c == nil {
		t.Fatal("NewConsumer returned nil")
	}
}

func TestNewConsumerWithClients(t *testing.T) {
	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	c := NewConsumerWithClients(cfg, clients)
	if c == nil {
		t.Fatal("NewConsumerWithClients returned nil")
	}
	if c.labeler == nil {
		t.Error("labeler should be initialized")
	}
	if c.syncer == nil {
		t.Error("syncer should be initialized")
	}
	if c.spamDetector == nil {
		t.Error("spamDetector should be initialized")
	}
	if c.queue == nil {
		t.Error("queue should be initialized")
	}
}

func TestStartStop(t *testing.T) {
	events.Init()

	c := NewConsumer()

	c.Start()
	c.Stop()
}

func TestSetStaleManager(t *testing.T) {
	c := NewConsumer()
	c.SetStaleManager(nil)
}

func TestDispatch_NilPR(t *testing.T) {
	c := NewConsumer()

	event := events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "main",
		Platform:  "github",
		PR:        nil,
	}

	c.dispatch(event)
}

func TestDispatch_InvalidType(t *testing.T) {
	c := NewConsumer()

	event := events.Event{
		Type:      "invalid_type",
		RepoGroup: "main",
		Platform:  "github",
		PR:        nil,
	}

	c.dispatch(event)
}

func TestHandlePROpened(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	config.Store(cfg)

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-opened-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "New feature",
		Author:    "dev1",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	// Verify PR was stored in DB
	data, err := db.Get(db.BucketPRs, "test-group#github#1")
	if err != nil {
		t.Fatalf("PR not stored in DB: %v", err)
	}

	var stored models.PRRecord
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("failed to unmarshal PR: %v", err)
	}
	if stored.Title != "New feature" {
		t.Errorf("stored title = %q, want %q", stored.Title, "New feature")
	}
	if stored.State != "open" {
		t.Errorf("stored state = %q, want open", stored.State)
	}
}

func TestHandlePROpened_EmptyID(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  2,
		Title:     "Auto ID test",
		Author:    "dev2",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	// PR should have been assigned an ID
	if pr.ID == "" {
		t.Error("PR ID should have been auto-generated")
	}
}

func TestHandlePRClosed(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	// Store a PR first
	pr := &models.PRRecord{
		ID:        "pr-close-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  10,
		Title:     "To be closed",
		Author:    "dev1",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#10", data)

	c.handlePRClosed(events.Event{
		Type:      events.EventPRClosed,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	// Verify state updated
	stored, err := db.Get(db.BucketPRs, "test-group#github#10")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.State != "closed" {
		t.Errorf("state = %q, want closed", result.State)
	}
}

func TestHandlePRMerged(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-merge-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  20,
		Title:     "To be merged",
		Author:    "dev1",
		State:     "open",
	}

	c.handlePRMerged(events.Event{
		Type:      events.EventPRMerged,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	stored, err := db.Get(db.BucketPRs, "test-group#github#20")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.State != "merged" {
		t.Errorf("state = %q, want merged", result.State)
	}
}

func TestHandlePRApproved(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-approve-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  30,
		Title:     "To be approved",
		Author:    "dev1",
		State:     "open",
	}

	c.handlePRApproved(events.Event{
		Type:      events.EventPRApproved,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	// Verify PR was added to queue
	items, err := c.queue.GetQueueItems("test-group")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 queue item, got %d", len(items))
	}
	if items[0].PRID != "pr-approve-1" {
		t.Errorf("queue item PRID = %q, want pr-approve-1", items[0].PRID)
	}
}

func TestHandlePRReopened(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	// Store a spam PR
	pr := &models.PRRecord{
		ID:        "pr-reopen-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  40,
		Title:     "Spam recovery",
		Author:    "dev1",
		State:     "spam",
		SpamFlag:  true,
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#40", data)

	c.handlePRReopened(events.Event{
		Type:      events.EventPRReopened,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	stored, err := db.Get(db.BucketPRs, "test-group#github#40")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.State != "open" {
		t.Errorf("state = %q, want open", result.State)
	}
	if result.SpamFlag {
		t.Error("SpamFlag should be false after reopen")
	}
}

func TestHandleSpamDetected_NilDetector(t *testing.T) {
	c := NewConsumer()

	// Should not panic with nil spamDetector
	c.handleSpamDetected(events.Event{
		Type:      events.EventSpamDetected,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        nil,
	})
}

func TestHandleBranchDeleted(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	// Should not panic
	c.handleBranchDeleted(events.Event{
		Type:      events.EventBranchDeleted,
		RepoGroup: "test-group",
		Platform:  "github",
		Payload:   "feature-branch",
	})
}

func TestHandleBranchDeleted_MissingPayload(t *testing.T) {
	c := NewConsumer()

	// Should not panic with nil payload
	c.handleBranchDeleted(events.Event{
		Type:      events.EventBranchDeleted,
		RepoGroup: "test-group",
		Platform:  "github",
		Payload:   nil,
	})

	// Should not panic with non-string payload
	c.handleBranchDeleted(events.Event{
		Type:      events.EventBranchDeleted,
		RepoGroup: "test-group",
		Platform:  "github",
		Payload:   12345,
	})
}

func TestDispatch_AllTypes(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	eventTypes := []events.EventType{
		events.EventPRLabeled,
		events.EventSyncCompleted,
		events.EventSyncFailed,
	}

	for _, et := range eventTypes {
		t.Run(string(et), func(t *testing.T) {
			c.dispatch(events.Event{
				Type:      et,
				RepoGroup: "test-group",
				Platform:  "github",
				PR:        nil,
			})
		})
	}
}

func TestConsumerEventFlow_OpenedThenClosed(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "flow-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	// Simulate PR opened then closed
	pr := &models.PRRecord{
		ID:        "flow-pr-1",
		RepoGroup: "flow-group",
		Platform:  "github",
		PRNumber:  200,
		Title:     "Flow test PR",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "flow-group",
		Platform:  "github",
		PR:        pr,
	})

	// Verify PR was stored
	data, err := db.Get(db.BucketPRs, "flow-group#github#200")
	if err != nil {
		t.Fatalf("PR not stored: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)
	if stored.Title != "Flow test PR" {
		t.Errorf("title = %q, want Flow test PR", stored.Title)
	}

	// Now close the PR
	c.handlePRClosed(events.Event{
		Type:      events.EventPRClosed,
		RepoGroup: "flow-group",
		Platform:  "github",
		PR:        pr,
	})

	// Verify state changed
	data, err = db.Get(db.BucketPRs, "flow-group#github#200")
	if err != nil {
		t.Fatalf("PR not found after close: %v", err)
	}
	json.Unmarshal(data, &stored)
	if stored.State != "closed" {
		t.Errorf("state = %q, want closed", stored.State)
	}
}

func TestHandlePRApproved_NilQueue(t *testing.T) {
	c := NewConsumer()
	c.queue = nil

	pr := &models.PRRecord{
		ID:        "pr-nil-queue",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  50,
		Title:     "Nil queue test",
		State:     "open",
	}

	c.handlePRApproved(events.Event{
		Type:      events.EventPRApproved,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})
}

func TestHandleSpamDetected_WithDetector(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		Spam: models.SpamConfig{
			Enabled:           true,
			Threshold:         3,
			TimeWindow:        "10m",
			TriggerOnAuthor:   true,
			TriggerOnTitleKw:  []string{"spam"},
			AutoCleanEnabled:  false,
			AutoCleanInterval: "24h",
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)

	c := NewConsumer()
	c.spamDetector = sd

	pr := &models.PRRecord{
		ID:        "pr-spam-detected",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  60,
		Title:     "Spam PR",
		Author:    "spammer",
		State:     "open",
	}

	c.handleSpamDetected(events.Event{
		Type:      events.EventSpamDetected,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	stored, err := db.Get(db.BucketPRs, "test-group#github#60")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if !result.SpamFlag {
		t.Error("SpamFlag should be true after HandleSpam")
	}
}

func TestHandlePRComment_NoCommand(t *testing.T) {
	c := NewConsumer()

	pr := &models.PRRecord{
		ID:        "pr-comment-nocommand",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  70,
		Title:     "Comment test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload: &models.PRCommentPayload{
			CommentBody:   "This is a normal comment",
			CommentAuthor: "user1",
		},
	})
}

func TestHandlePRComment_NilPayload(t *testing.T) {
	c := NewConsumer()

	pr := &models.PRRecord{
		ID:        "pr-comment-nilpayload",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  71,
		Title:     "Nil payload test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload:   nil,
	})
}

func TestHandlePRComment_NilPR(t *testing.T) {
	c := NewConsumer()

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        nil,
		Payload: &models.PRCommentPayload{
			CommentBody:   "/approve",
			CommentAuthor: "user1",
		},
	})
}

func TestHandlePRComment_UnknownCommand(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-comment-unknown",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  72,
		Title:     "Unknown command test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload: &models.PRCommentPayload{
			CommentBody:   "/unknowncmd",
			CommentAuthor: "user1",
		},
	})
}

func TestHandlePRComment_WithQueue(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}
	c := NewConsumerWithClients(cfg, clients)
	c.clients = clients

	pr := &models.PRRecord{
		ID:        "pr-comment-queue",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  73,
		Title:     "Queue command test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload: &models.PRCommentPayload{
			CommentBody:   "/queue",
			CommentAuthor: "user1",
		},
	})

	items, err := c.queue.GetQueueItems("test-group")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 queue item, got %d", len(items))
	}
}

func TestHandlePRComment_WithRecheck(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-comment-recheck",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  74,
		Title:     "Recheck command test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload: &models.PRCommentPayload{
			CommentBody:   "/recheck",
			CommentAuthor: "user1",
		},
	})
}

func TestHandlePRComment_MissingClient(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-comment-noclient",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  75,
		Title:     "No client test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload: &models.PRCommentPayload{
			CommentBody:   "/approve",
			CommentAuthor: "user1",
		},
	})
}

func TestHandlePRComment_HelpCommand(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-comment-help",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  76,
		Title:     "Help command test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload: &models.PRCommentPayload{
			CommentBody:   "/help",
			CommentAuthor: "user1",
		},
	})
}

func TestDispatch_AllEventTypes(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "dispatch-all", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	allTypes := []events.EventType{
		events.EventPROpened,
		events.EventPRClosed,
		events.EventPRMerged,
		events.EventPRApproved,
		events.EventPRReopened,
		events.EventSpamDetected,
		events.EventPRComment,
		events.EventPRLabeled,
		events.EventBranchDeleted,
		events.EventSyncCompleted,
		events.EventSyncFailed,
	}

	for _, et := range allTypes {
		t.Run(string(et), func(t *testing.T) {
			c.dispatch(events.Event{
				Type:      et,
				RepoGroup: "dispatch-all",
				Platform:  "github",
				PR:        nil,
			})
		})
	}
}

func TestHandlePRComment_CherryPickNoArgs(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-comment-cp",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  77,
		Title:     "Cherry-pick test",
		State:     "open",
	}

	c.handlePRComment(events.Event{
		Type:      events.EventPRComment,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload: &models.PRCommentPayload{
			CommentBody:   "/cherry-pick",
			CommentAuthor: "user1",
		},
	})
}

func TestCmdRebase(t *testing.T) {
	c := NewConsumer()
	pr := &models.PRRecord{
		PRNumber: 80,
		Title:    "Rebase test",
	}

	result := c.cmdRebase(nil, nil, pr, "", "", "author1")
	if result == "" {
		t.Error("cmdRebase should return a message")
	}
}

func TestNewConsumerWithQueue(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "queue-test-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  90,
		Title:     "Queue test",
		State:     "open",
	}

	c.handlePRApproved(events.Event{
		Type:      events.EventPRApproved,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	items, err := c.queue.GetQueueItems("test-group")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 queue item, got %d", len(items))
	}
}

func TestQueueManager_AddToQueue(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "qtest", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	q := queue.NewManager(cfg, clients)

	pr := &models.PRRecord{
		ID:        "qm-test-1",
		RepoGroup: "qtest",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Queue manager test",
		State:     "open",
	}

	err := q.AddToQueue(pr)
	if err != nil {
		t.Fatalf("AddToQueue failed: %v", err)
	}

	items, err := q.GetQueueItems("qtest")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
	if items[0].PRID != "qm-test-1" {
		t.Errorf("PRID = %q, want qm-test-1", items[0].PRID)
	}
}

func TestHandlePRLabeled_WithStringPayload(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	c := NewConsumer()

	pr := &models.PRRecord{
		ID:        "pr-label-payload",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  300,
		Title:     "Label payload test",
		Author:    "tester",
		State:     "open",
		Labels:    []string{"existing"},
	}

	c.handlePRLabeled(events.Event{
		Type:      events.EventPRLabeled,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload:   "stale",
	})

	stored, err := db.Get(db.BucketPRs, "test-group#github#300")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)

	found := false
	for _, l := range result.Labels {
		if l == "stale" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'stale' in labels, got %v", result.Labels)
	}
	if len(result.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d: %v", len(result.Labels), result.Labels)
	}
}

func TestHandlePRLabeled_DuplicateLabelNotAdded(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	c := NewConsumer()

	pr := &models.PRRecord{
		ID:        "pr-label-dup",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  301,
		Title:     "Duplicate label test",
		Author:    "tester",
		State:     "open",
		Labels:    []string{"bug", "critical"},
	}

	c.handlePRLabeled(events.Event{
		Type:      events.EventPRLabeled,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload:   "bug",
	})

	stored, err := db.Get(db.BucketPRs, "test-group#github#301")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)

	if len(result.Labels) != 2 {
		t.Errorf("expected 2 labels (no duplicate), got %d: %v", len(result.Labels), result.Labels)
	}
}

func TestHandlePRLabeled_NilPayload(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	c := NewConsumer()

	pr := &models.PRRecord{
		ID:        "pr-label-nil",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  302,
		Title:     "Nil payload test",
		Author:    "tester",
		State:     "open",
		Labels:    []string{"existing"},
	}

	c.handlePRLabeled(events.Event{
		Type:      events.EventPRLabeled,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
		Payload:   nil,
	})

	stored, err := db.Get(db.BucketPRs, "test-group#github#302")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)

	if len(result.Labels) != 1 {
		t.Errorf("expected 1 label (unchanged), got %d: %v", len(result.Labels), result.Labels)
	}
}

func TestConsumerEventFlow_OpenedThenMerged(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "flow-merge", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "flow-merge-pr-1",
		RepoGroup: "flow-merge",
		Platform:  "github",
		PRNumber:  400,
		Title:     "Flow merge test PR",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "flow-merge",
		Platform:  "github",
		PR:        pr,
	})

	data, err := db.Get(db.BucketPRs, "flow-merge#github#400")
	if err != nil {
		t.Fatalf("PR not stored after open: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)
	if stored.State != "open" {
		t.Errorf("state after open = %q, want open", stored.State)
	}

	c.handlePRMerged(events.Event{
		Type:      events.EventPRMerged,
		RepoGroup: "flow-merge",
		Platform:  "github",
		PR:        pr,
	})

	data, err = db.Get(db.BucketPRs, "flow-merge#github#400")
	if err != nil {
		t.Fatalf("PR not found after merge: %v", err)
	}
	json.Unmarshal(data, &stored)
	if stored.State != "merged" {
		t.Errorf("state after merge = %q, want merged", stored.State)
	}
}

func TestConsumerEventFlow_OpenedThenApprovedThenClosed(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "flow-full", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "flow-full-pr-1",
		RepoGroup: "flow-full",
		Platform:  "github",
		PRNumber:  500,
		Title:     "Full flow test PR",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "flow-full",
		Platform:  "github",
		PR:        pr,
	})

	c.handlePRApproved(events.Event{
		Type:      events.EventPRApproved,
		RepoGroup: "flow-full",
		Platform:  "github",
		PR:        pr,
	})

	items, err := c.queue.GetQueueItems("flow-full")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 queue item after approve, got %d", len(items))
	}

	c.handlePRClosed(events.Event{
		Type:      events.EventPRClosed,
		RepoGroup: "flow-full",
		Platform:  "github",
		PR:        pr,
	})

	data, err := db.Get(db.BucketPRs, "flow-full#github#500")
	if err != nil {
		t.Fatalf("PR not found after close: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)
	if stored.State != "closed" {
		t.Errorf("state after close = %q, want closed", stored.State)
	}
}

func TestConsumerEventFlow_SpamDetection(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		Spam: models.SpamConfig{
			Enabled:           true,
			Threshold:         3,
			TimeWindow:        "10m",
			TriggerOnAuthor:   true,
			TriggerOnTitleKw:  []string{},
			AutoCleanEnabled:  false,
			AutoCleanInterval: "24h",
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "spam-flow", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)

	c := NewConsumer()
	c.spamDetector = sd

	pr := &models.PRRecord{
		ID:        "spam-flow-pr-1",
		RepoGroup: "spam-flow",
		Platform:  "github",
		PRNumber:  600,
		Title:     "Spam flow test",
		Author:    "spammer",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "spam-flow",
		Platform:  "github",
		PR:        pr,
	})

	c.handleSpamDetected(events.Event{
		Type:      events.EventSpamDetected,
		RepoGroup: "spam-flow",
		Platform:  "github",
		PR:        pr,
	})

	time.Sleep(100 * time.Millisecond)

	data, err := db.Get(db.BucketPRs, "spam-flow#github#600")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)
	if !stored.SpamFlag {
		t.Error("SpamFlag should be true after spam detection")
	}
}

func TestConsumerEventFlow_ReopenAfterSpam(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "reopen-flow", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "reopen-flow-pr-1",
		RepoGroup: "reopen-flow",
		Platform:  "github",
		PRNumber:  700,
		Title:     "Reopen flow test",
		Author:    "tester",
		State:     "spam",
		SpamFlag:  true,
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "reopen-flow",
		Platform:  "github",
		PR:        pr,
	})

	c.handleSpamDetected(events.Event{
		Type:      events.EventSpamDetected,
		RepoGroup: "reopen-flow",
		Platform:  "github",
		PR:        pr,
	})

	time.Sleep(100 * time.Millisecond)

	c.handlePRReopened(events.Event{
		Type:      events.EventPRReopened,
		RepoGroup: "reopen-flow",
		Platform:  "github",
		PR:        pr,
	})

	data, err := db.Get(db.BucketPRs, "reopen-flow#github#700")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)
	if stored.State != "open" {
		t.Errorf("state after reopen = %q, want open", stored.State)
	}
	if stored.SpamFlag {
		t.Error("SpamFlag should be false after reopen")
	}
}

func TestConsumerEventFlow_LabeledEvent(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "label-flow-pr-1",
		RepoGroup: "label-flow",
		Platform:  "github",
		PRNumber:  800,
		Title:     "Label flow test",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "label-flow",
		Platform:  "github",
		PR:        pr,
	})

	c.handlePRLabeled(events.Event{
		Type:      events.EventPRLabeled,
		RepoGroup: "label-flow",
		Platform:  "github",
		PR:        pr,
		Payload:   "needs-review",
	})

	data, err := db.Get(db.BucketPRs, "label-flow#github#800")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)

	found := false
	for _, l := range stored.Labels {
		if l == "needs-review" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'needs-review' in labels, got %v", stored.Labels)
	}
}

func TestConsumerEventFlow_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "lifecycle", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "lifecycle-pr-1",
		RepoGroup: "lifecycle",
		Platform:  "github",
		PRNumber:  900,
		Title:     "Full lifecycle test",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "lifecycle",
		Platform:  "github",
		PR:        pr,
	})

	c.handlePRLabeled(events.Event{
		Type:      events.EventPRLabeled,
		RepoGroup: "lifecycle",
		Platform:  "github",
		PR:        pr,
		Payload:   "approved",
	})

	c.handlePRApproved(events.Event{
		Type:      events.EventPRApproved,
		RepoGroup: "lifecycle",
		Platform:  "github",
		PR:        pr,
	})

	c.handlePRMerged(events.Event{
		Type:      events.EventPRMerged,
		RepoGroup: "lifecycle",
		Platform:  "github",
		PR:        pr,
	})

	data, err := db.Get(db.BucketPRs, "lifecycle#github#900")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)
	if stored.State != "merged" {
		t.Errorf("final state = %q, want merged", stored.State)
	}

	found := false
	for _, l := range stored.Labels {
		if l == "approved" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'approved' in labels, got %v", stored.Labels)
	}
}

func TestConsumerGoroutinePanicRecovery_NoSilentCrash(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "goroutine-panic-pr",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Goroutine panic test",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "test-group",
		Platform:  "github",
		PR:        pr,
	})

	time.Sleep(200 * time.Millisecond)

	data, err := db.Get(db.BucketPRs, "test-group#github#1")
	if err != nil {
		t.Fatalf("PR not stored: %v", err)
	}
	var stored models.PRRecord
	json.Unmarshal(data, &stored)
	if stored.Title != "Goroutine panic test" {
		t.Errorf("title = %q, want %q", stored.Title, "Goroutine panic test")
	}
}

func TestConsumerConcurrentEventDispatch(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pr := &models.PRRecord{
				ID:        "concurrent-pr-" + string(rune('A'+n)),
				RepoGroup: "concurrent-group",
				Platform:  "github",
				PRNumber:  n + 1,
				Title:     "Concurrent PR " + string(rune('A'+n)),
				Author:    "tester",
				State:     "open",
			}
			c.handlePROpened(events.Event{
				Type:      events.EventPROpened,
				RepoGroup: "concurrent-group",
				Platform:  "github",
				PR:        pr,
			})
		}(i)
	}
	wg.Wait()
}

func TestSyncPRLinks_WriterActorTiming(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(256)
	defer w.Stop()

	pr := &models.PRRecord{
		ID:        "timing-pr-1",
		RepoGroup: "timing-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "PR with Fixes: org/repo#42",
		Body:      "This PR Fixes: other/repo#99 and Resolves: org/repo#42",
	}

	syncPRLinks(w, pr)

	links, err := db.GetIssuePRLinksByPR("timing-pr-1")
	if err != nil {
		t.Fatalf("GetIssuePRLinksByPR failed: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}

	seen := make(map[string]bool)
	for _, l := range links {
		seen[l.IssueID] = true
	}
	if !seen["org/repo#42"] {
		t.Error("expected org/repo#42 in links")
	}
	if !seen["other/repo#99"] {
		t.Error("expected other/repo#99 in links")
	}
}

func TestSyncPRLinks_ConcurrentWithPRWrite(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "concurrent-link-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	pr := &models.PRRecord{
		ID:        "concurrent-link-pr",
		RepoGroup: "concurrent-link-group",
		Platform:  "github",
		PRNumber:  100,
		Title:     "Fixes: org/repo#50",
		Body:      "Resolves: org/repo#51\nFixes: org/repo#52",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "concurrent-link-group",
		Platform:  "github",
		PR:        pr,
	})

	time.Sleep(300 * time.Millisecond)

	prData, err := db.Get(db.BucketPRs, "concurrent-link-group#github#100")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	if prData == nil {
		t.Fatal("PR data is nil")
	}

	links, err := db.GetIssuePRLinksByPR("concurrent-link-pr")
	if err != nil {
		t.Fatalf("GetIssuePRLinksByPR failed: %v", err)
	}
	if len(links) != 3 {
		t.Errorf("expected 3 links, got %d", len(links))
	}

	seen := make(map[string]bool)
	for _, l := range links {
		seen[l.IssueID] = true
	}
	for _, expected := range []string{"org/repo#50", "org/repo#51", "org/repo#52"} {
		if !seen[expected] {
			t.Errorf("expected %s in links", expected)
		}
	}
}

func TestSyncPRLinks_DuplicateIssueNotLinked(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(256)
	defer w.Stop()

	pr := &models.PRRecord{
		ID:        "dup-link-pr",
		RepoGroup: "dup-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Fixes: org/repo#10",
		Body:      "Also Fixes: org/repo#10 and Resolves: org/repo#10",
	}

	syncPRLinks(w, pr)

	links, err := db.GetIssuePRLinksByPR("dup-link-pr")
	if err != nil {
		t.Fatalf("GetIssuePRLinksByPR failed: %v", err)
	}
	if len(links) != 1 {
		t.Errorf("expected 1 link (deduped), got %d", len(links))
	}
}

func TestConsumerStop_CancelsContext(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	if c.ctx == nil {
		t.Fatal("ctx should be initialized")
	}

	err := c.ctx.Err()
	if err != nil {
		t.Fatalf("ctx should not be cancelled before Stop(), got: %v", err)
	}

	c.Stop()

	select {
	case <-c.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context should be cancelled after Stop()")
	}
}

func TestConsumerStop_GoroutinesExit(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)
	c.Start()

	pr := &models.PRRecord{
		ID:        "stop-goroutine-pr",
		RepoGroup: "stop-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Stop goroutine test",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "stop-group",
		Platform:  "github",
		PR:        pr,
	})

	time.Sleep(100 * time.Millisecond)

	c.Stop()

	time.Sleep(200 * time.Millisecond)

	data, err := db.Get(db.BucketPRs, "stop-group#github#1")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	if data == nil {
		t.Fatal("PR data is nil")
	}
}

func TestConsumerStop_Idempotent(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)

	c.Stop()
	c.Stop()
	c.Stop()
}

func TestConsumerStop_ThenRestart(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	events.Init()

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewConsumerWithClients(cfg, clients)
	c.Start()

	pr1 := &models.PRRecord{
		ID:        "restart-pr-1",
		RepoGroup: "restart-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Before restart",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "restart-group",
		Platform:  "github",
		PR:        pr1,
	})

	time.Sleep(100 * time.Millisecond)
	c.Stop()
	time.Sleep(100 * time.Millisecond)

	c.Start()

	pr2 := &models.PRRecord{
		ID:        "restart-pr-2",
		RepoGroup: "restart-group",
		Platform:  "github",
		PRNumber:  2,
		Title:     "After restart",
		Author:    "tester",
		State:     "open",
	}

	c.handlePROpened(events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "restart-group",
		Platform:  "github",
		PR:        pr2,
	})

	time.Sleep(100 * time.Millisecond)
	c.Stop()

	data1, err := db.Get(db.BucketPRs, "restart-group#github#1")
	if err != nil {
		t.Fatalf("PR1 not found: %v", err)
	}
	if data1 == nil {
		t.Fatal("PR1 data is nil")
	}

	data2, err := db.Get(db.BucketPRs, "restart-group#github#2")
	if err != nil {
		t.Fatalf("PR2 not found: %v", err)
	}
	if data2 == nil {
		t.Fatal("PR2 data is nil")
	}
}
