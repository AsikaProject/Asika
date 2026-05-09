package consumer

import (
	"encoding/json"
	"testing"

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
			Enabled:            true,
			Threshold:          3,
			TimeWindow:         "10m",
			TriggerOnAuthor:    true,
			TriggerOnTitleKw:   []string{"spam"},
			AutoCleanEnabled:   false,
			AutoCleanInterval:  "24h",
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
