package queue

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func TestNewManager(t *testing.T) {
	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	m := NewManager(cfg, clients)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestNewChecker(t *testing.T) {
	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	c := NewChecker(cfg, clients)
	if c == nil {
		t.Fatal("NewChecker returned nil")
	}
}

func TestAddToQueue(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name: "main",
				Mode: "multi",
			},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	m := NewManager(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-123",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}

	err := m.AddToQueue(pr)
	if err != nil {
		t.Fatalf("AddToQueue failed: %v", err)
	}
}

func TestAddToQueue_Duplicate(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	m := NewManager(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-123",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}

	// First add
	err := m.AddToQueue(pr)
	if err != nil {
		t.Fatalf("First AddToQueue failed: %v", err)
	}

	// Second add (should not error)
	err = m.AddToQueue(pr)
	if err != nil {
		t.Fatalf("Second AddToQueue should not error: %v", err)
	}
}

func TestGetQueueItems(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	m := NewManager(cfg, clients)

	// Add several PRs to queue
	prs := []*models.PRRecord{
		{ID: "pr-1", RepoGroup: "main", Platform: "github", PRNumber: 1, Title: "PR 1", State: "open"},
		{ID: "pr-2", RepoGroup: "main", Platform: "github", PRNumber: 2, Title: "PR 2", State: "open"},
		{ID: "pr-3", RepoGroup: "other", Platform: "gitlab", PRNumber: 3, Title: "PR 3", State: "open"},
	}

	for _, pr := range prs {
		m.AddToQueue(pr)
	}

	// Get queue items for main repo group
	items, err := m.GetQueueItems("main")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("GetQueueItems returned %d items, want 2", len(items))
	}
}

func TestGetQueueItems_Empty(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	m := NewManager(cfg, clients)

	items, err := m.GetQueueItems("main")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("GetQueueItems returned %d items, want 0", len(items))
	}
}

func TestShouldMerge(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:   "main",
				GitHub: "owner/repo",
				MergeQueue: models.MergeQueueConfig{
					RequiredApprovals: 1,
					CICheckRequired:   false,
					CoreContributors:  []string{"user1"},
				},
			},
		},
	}

	client := &testutil.MockPlatformClient{
		Approvals: []string{"user1"},
		CIStatus:  "success",
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	c := NewChecker(cfg, clients)

	// Add PR to database
	pr := &models.PRRecord{
		ID:        "pr-123",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "main#github#1", data)

	item := &models.QueueItem{
		PRID:      "pr-123",
		RepoGroup: "main",
		Status:    "waiting",
	}

	shouldMerge, err := c.ShouldMerge(item)
	if err != nil {
		t.Fatalf("ShouldMerge failed: %v", err)
	}

	if !shouldMerge {
		t.Error("ShouldMerge should return true for pr with enough approvals")
	}
}

func TestShouldMerge_NotEnoughApprovals(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:   "main",
				GitHub: "owner/repo",
				MergeQueue: models.MergeQueueConfig{
					RequiredApprovals: 2,
				},
			},
		},
	}

	client := &testutil.MockPlatformClient{
		Approvals: []string{"user1"}, // Only 1 approval
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	c := NewChecker(cfg, clients)

	// Add PR to database
	pr := &models.PRRecord{
		ID:        "pr-123",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "main#github#1", data)

	item := &models.QueueItem{
		PRID:      "pr-123",
		RepoGroup: "main",
		Status:    "waiting",
	}

	shouldMerge, err := c.ShouldMerge(item)
	if err != nil {
		t.Fatalf("ShouldMerge failed: %v", err)
	}

	if shouldMerge {
		t.Error("ShouldMerge should return false when not enough approvals")
	}
}

func TestFindPRByID(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	// Add PR to database
	pr := &models.PRRecord{
		ID:        "test-pr-id",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "main#github#1", data)

	// Find PR
	found, err := FindPRByID("test-pr-id")
	if err != nil {
		t.Fatalf("FindPRByID failed: %v", err)
	}

	if found == nil {
		t.Fatal("FindPRByID returned nil")
	}
	if found.ID != "test-pr-id" {
		t.Errorf("found.ID = %q, want test-pr-id", found.ID)
	}
}

func TestGetPRFromDB(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	// Add PR to database
	pr := &models.PRRecord{
		ID:        "test-pr-id",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "main#test-pr-id", data)

	// Get PR
	found, err := getPRFromDB("main", "test-pr-id")
	if err != nil {
		t.Fatalf("getPRFromDB failed: %v", err)
	}

	if found == nil {
		t.Fatal("getPRFromDB returned nil")
	}
	if found.ID != "test-pr-id" {
		t.Errorf("found.ID = %q, want test-pr-id", found.ID)
	}
}

func TestShouldMerge_CICheckRequired(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:       "main",
				GitHub:     "owner/repo",
				CIProvider: "github_actions",
				MergeQueue: models.MergeQueueConfig{
					RequiredApprovals: 1,
					CICheckRequired:   true,
					CoreContributors:  []string{"user1"},
				},
			},
		},
	}

	client := &testutil.MockPlatformClient{
		Approvals: []string{"user1"},
		CIStatus:  "success",
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	c := NewChecker(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-ci-1",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "CI test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "main#github#1", data)

	item := &models.QueueItem{
		PRID:      "pr-ci-1",
		RepoGroup: "main",
		Status:    "waiting",
	}

	shouldMerge, err := c.ShouldMerge(item)
	if err != nil {
		t.Fatalf("ShouldMerge failed: %v", err)
	}
	if !shouldMerge {
		t.Error("ShouldMerge should return true with passing CI")
	}
}

func TestShouldMerge_CIFailing(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:       "main",
				GitHub:     "owner/repo",
				CIProvider: "github_actions",
				MergeQueue: models.MergeQueueConfig{
					RequiredApprovals: 1,
					CICheckRequired:   true,
					CoreContributors:  []string{"user1"},
				},
			},
		},
	}

	client := &testutil.MockPlatformClient{
		Approvals: []string{"user1"},
		CIStatus:  "failure",
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	c := NewChecker(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-ci-fail-1",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Failing CI PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "main#github#1", data)

	item := &models.QueueItem{
		PRID:      "pr-ci-fail-1",
		RepoGroup: "main",
		Status:    "waiting",
	}

	shouldMerge, err := c.ShouldMerge(item)
	if err != nil {
		t.Fatalf("ShouldMerge failed: %v", err)
	}
	if shouldMerge {
		t.Error("ShouldMerge should return false when CI is failing")
	}
}

func TestShouldMerge_CoreContributorBypass(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:   "main",
				GitHub: "owner/repo",
				MergeQueue: models.MergeQueueConfig{
					RequiredApprovals: 1,
					CICheckRequired:   false,
					CoreContributors:  []string{"core-dev"},
				},
			},
		},
	}

	client := &testutil.MockPlatformClient{
		Approvals: []string{"core-dev"},
		CIStatus:  "success",
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	c := NewChecker(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-core-1",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Core dev PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "main#github#1", data)

	item := &models.QueueItem{
		PRID:      "pr-core-1",
		RepoGroup: "main",
		Status:    "waiting",
	}

	shouldMerge, err := c.ShouldMerge(item)
	if err != nil {
		t.Fatalf("ShouldMerge failed: %v", err)
	}
	if !shouldMerge {
		t.Error("ShouldMerge should return true for core contributor")
	}
}

func TestShouldMerge_PRNotFound(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewChecker(cfg, clients)

	item := &models.QueueItem{
		PRID:      "nonexistent-pr",
		RepoGroup: "main",
		Status:    "waiting",
	}

	_, err := c.ShouldMerge(item)
	if err == nil {
		t.Error("ShouldMerge should return error for nonexistent PR")
	}
}

func TestShouldMerge_GroupNotFound(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{},
	}

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	c := NewChecker(cfg, clients)

	pr := &models.PRRecord{
		ID:        "pr-nogroup-1",
		RepoGroup: "missing",
		Platform:  "github",
		PRNumber:  1,
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "missing#github#1", data)

	item := &models.QueueItem{
		PRID:      "pr-nogroup-1",
		RepoGroup: "missing",
		Status:    "waiting",
	}

	_, err := c.ShouldMerge(item)
	if err == nil {
		t.Error("ShouldMerge should return error for missing repo group")
	}
}

func TestGetQueueItems_MultipleGroups(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	m.AddToQueue(&models.PRRecord{ID: "pr-a-1", RepoGroup: "group-a", Platform: "github", PRNumber: 1, State: "open"})
	m.AddToQueue(&models.PRRecord{ID: "pr-a-2", RepoGroup: "group-a", Platform: "github", PRNumber: 2, State: "open"})
	m.AddToQueue(&models.PRRecord{ID: "pr-b-1", RepoGroup: "group-b", Platform: "gitlab", PRNumber: 1, State: "open"})
	m.AddToQueue(&models.PRRecord{ID: "pr-c-1", RepoGroup: "group-c", Platform: "gitea", PRNumber: 1, State: "open"})

	tests := []struct {
		repoGroup string
		wantCount int
	}{
		{"group-a", 2},
		{"group-b", 1},
		{"group-c", 1},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.repoGroup, func(t *testing.T) {
			items, err := m.GetQueueItems(tt.repoGroup)
			if err != nil {
				t.Fatalf("GetQueueItems(%q) failed: %v", tt.repoGroup, err)
			}
			if len(items) != tt.wantCount {
				t.Errorf("GetQueueItems(%q) returned %d items, want %d", tt.repoGroup, len(items), tt.wantCount)
			}
		})
	}
}

func TestAddToQueue_MultiplePRs(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	for i := 1; i <= 5; i++ {
		pr := &models.PRRecord{
			ID:        fmt.Sprintf("pr-multi-%d", i),
			RepoGroup: "main",
			Platform:  "github",
			PRNumber:  i,
			State:     "open",
		}
		if err := m.AddToQueue(pr); err != nil {
			t.Fatalf("AddToQueue(%d) failed: %v", i, err)
		}
	}

	items, err := m.GetQueueItems("main")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}
	if len(items) != 5 {
		t.Errorf("expected 5 queue items, got %d", len(items))
	}
}

func TestFindPRByID_NotFound(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	_, err := FindPRByID("nonexistent")
	if err == nil {
		t.Error("FindPRByID should return error for nonexistent PR")
	}
}

func TestGetPRFromDB_NotFound(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	_, err := getPRFromDB("main", "nonexistent")
	if err == nil {
		t.Error("getPRFromDB should return error for nonexistent PR")
	}
}

func TestQueueRecovery_ResetsStaleItems(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", GitHub: "owner/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	// Add a PR to DB
	pr := &models.PRRecord{
		ID:        "recovery-pr-1",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		State:     "open",
	}
	db.Put(db.BucketPRs, "main#github#1", mustMarshalPR(pr))

	// Add queue items in various states
	waitingItem := models.QueueItem{
		PRID:      "recovery-pr-1",
		RepoGroup: "main",
		Status:    "waiting",
		AddedAt:   time.Now(),
	}
	mergingItem := models.QueueItem{
		PRID:      "recovery-pr-1",
		RepoGroup: "main",
		Status:    "merging",
		AddedAt:   time.Now(),
	}
	checkingItem := models.QueueItem{
		PRID:      "recovery-pr-1",
		RepoGroup: "main",
		Status:    "checking",
		AddedAt:   time.Now(),
	}

	m.AddToQueue(pr)
	// Manually set states to simulate crash
	key := "main#recovery-pr-1"
	db.Put(db.BucketQueueItems, key, mustMarshalPR(waitingItem))
	db.Put(db.BucketQueueItems, key+"-merging", mustMarshalPR(mergingItem))
	db.Put(db.BucketQueueItems, key+"-checking", mustMarshalPR(checkingItem))

	// Run recovery
	m.Recover()

	// Verify merging and checking items were reset to waiting
	var resetCount, waitingCount int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		var item models.QueueItem
		json.Unmarshal(v, &item)
		if item.Status == "waiting" {
			waitingCount++
		}
		if item.Status == "waiting" && (string(k) == key+"-merging" || string(k) == key+"-checking") {
			resetCount++
		}
		return nil
	})

	if resetCount != 2 {
		t.Errorf("expected 2 reset items, got %d", resetCount)
	}
	if waitingCount < 2 {
		t.Errorf("expected at least 2 waiting items, got %d", waitingCount)
	}
}

func TestQueueRecovery_AlreadyMergedPR_Removed(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", GitHub: "owner/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	// Add a PR that's already merged
	pr := &models.PRRecord{
		ID:        "merged-pr-1",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  2,
		State:     "merged",
	}
	db.Put(db.BucketPRs, "main#github#2", mustMarshalPR(pr))

	// Add a merging queue item for this already-merged PR
	mergingItem := models.QueueItem{
		PRID:      "merged-pr-1",
		RepoGroup: "main",
		Status:    "merging",
		AddedAt:   time.Now(),
	}
	key := "main#merged-pr-1"
	db.Put(db.BucketQueueItems, key, mustMarshalPR(mergingItem))

	// Run recovery
	m.Recover()

	// Verify the item was removed (not reset to waiting)
	var count int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		count++
		return nil
	})
	if count != 0 {
		t.Errorf("expected 0 queue items (merged PR should be removed), got %d", count)
	}
}

func mustMarshalPR(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestQueueRecovery_MergingStateWithMergedPR(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "recover-boundary", GitHub: "owner/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	pr := &models.PRRecord{
		ID:        "boundary-merged-pr",
		RepoGroup: "recover-boundary",
		Platform:  "github",
		PRNumber:  10,
		State:     "merged",
	}
	db.Put(db.BucketPRs, "recover-boundary#github#10", mustMarshalPR(pr))

	mergingItem := models.QueueItem{
		PRID:      "boundary-merged-pr",
		RepoGroup: "recover-boundary",
		Status:    "merging",
		AddedAt:   time.Now(),
	}
	key := "recover-boundary#boundary-merged-pr"
	db.Put(db.BucketQueueItems, key, mustMarshalPR(mergingItem))

	m.Recover()

	var count int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		count++
		return nil
	})
	if count != 0 {
		t.Errorf("expected 0 items (merged PR should be removed), got %d", count)
	}
}

func TestQueueRecovery_CheckingStateWithMergedPR(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "recover-checking", GitHub: "owner/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	pr := &models.PRRecord{
		ID:        "checking-merged-pr",
		RepoGroup: "recover-checking",
		Platform:  "github",
		PRNumber:  11,
		State:     "merged",
	}
	db.Put(db.BucketPRs, "recover-checking#github#11", mustMarshalPR(pr))

	checkingItem := models.QueueItem{
		PRID:      "checking-merged-pr",
		RepoGroup: "recover-checking",
		Status:    "checking",
		AddedAt:   time.Now(),
	}
	key := "recover-checking#checking-merged-pr"
	db.Put(db.BucketQueueItems, key, mustMarshalPR(checkingItem))

	m.Recover()

	var count int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		count++
		return nil
	})
	if count != 0 {
		t.Errorf("expected 0 items (merged PR should be removed), got %d", count)
	}
}

func TestQueueRecovery_MultipleStaleItems(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "multi-stale", GitHub: "owner/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	for i := 0; i < 5; i++ {
		pr := &models.PRRecord{
			ID:        "multi-stale-pr-" + string(rune('A'+i)),
			RepoGroup: "multi-stale",
			Platform:  "github",
			PRNumber:  i + 20,
			State:     "open",
		}
		db.Put(db.BucketPRs, "multi-stale#github#"+string(rune('1'+i+20)), mustMarshalPR(pr))

		status := "merging"
		if i%2 == 0 {
			status = "checking"
		}
		item := models.QueueItem{
			PRID:      "multi-stale-pr-" + string(rune('A'+i)),
			RepoGroup: "multi-stale",
			Status:    status,
			AddedAt:   time.Now(),
		}
		key := "multi-stale#multi-stale-pr-" + string(rune('A'+i))
		db.Put(db.BucketQueueItems, key, mustMarshalPR(item))
	}

	m.Recover()

	var waitingCount int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		var item models.QueueItem
		json.Unmarshal(v, &item)
		if item.Status == "waiting" {
			waitingCount++
		}
		return nil
	})
	if waitingCount != 5 {
		t.Errorf("expected 5 waiting items after recovery, got %d", waitingCount)
	}
}

func TestQueueRecovery_MixedStates(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "mixed-state", GitHub: "owner/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	openPR := &models.PRRecord{
		ID:        "mixed-open-pr",
		RepoGroup: "mixed-state",
		Platform:  "github",
		PRNumber:  30,
		State:     "open",
	}
	mergedPR := &models.PRRecord{
		ID:        "mixed-merged-pr",
		RepoGroup: "mixed-state",
		Platform:  "github",
		PRNumber:  31,
		State:     "merged",
	}
	db.Put(db.BucketPRs, "mixed-state#github#30", mustMarshalPR(openPR))
	db.Put(db.BucketPRs, "mixed-state#github#31", mustMarshalPR(mergedPR))

	openItem := models.QueueItem{
		PRID:      "mixed-open-pr",
		RepoGroup: "mixed-state",
		Status:    "merging",
		AddedAt:   time.Now(),
	}
	mergedItem := models.QueueItem{
		PRID:      "mixed-merged-pr",
		RepoGroup: "mixed-state",
		Status:    "checking",
		AddedAt:   time.Now(),
	}
	db.Put(db.BucketQueueItems, "mixed-state#mixed-open-pr", mustMarshalPR(openItem))
	db.Put(db.BucketQueueItems, "mixed-state#mixed-merged-pr", mustMarshalPR(mergedItem))

	m.Recover()

	var waitingCount, totalCount int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		totalCount++
		var item models.QueueItem
		json.Unmarshal(v, &item)
		if item.Status == "waiting" {
			waitingCount++
		}
		return nil
	})

	if totalCount != 1 {
		t.Errorf("expected 1 total item (merged removed), got %d", totalCount)
	}
	if waitingCount != 1 {
		t.Errorf("expected 1 waiting item, got %d", waitingCount)
	}
}

func TestQueueRecovery_EmptyQueue(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	m.Recover()
}

func TestQueueRecovery_DoneItemsNotAffected(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	doneItem := models.QueueItem{
		PRID:      "done-pr",
		RepoGroup: "test",
		Status:    "done",
		AddedAt:   time.Now(),
	}
	failedItem := models.QueueItem{
		PRID:      "failed-pr",
		RepoGroup: "test",
		Status:    "failed",
		AddedAt:   time.Now(),
	}
	waitingItem := models.QueueItem{
		PRID:      "waiting-pr",
		RepoGroup: "test",
		Status:    "waiting",
		AddedAt:   time.Now(),
	}

	db.Put(db.BucketQueueItems, "test#done-pr", mustMarshalPR(doneItem))
	db.Put(db.BucketQueueItems, "test#failed-pr", mustMarshalPR(failedItem))
	db.Put(db.BucketQueueItems, "test#waiting-pr", mustMarshalPR(waitingItem))

	m.Recover()

	var doneCount, failedCount, waitingCount int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		var item models.QueueItem
		json.Unmarshal(v, &item)
		switch item.Status {
		case "done":
			doneCount++
		case "failed":
			failedCount++
		case "waiting":
			waitingCount++
		}
		return nil
	})

	if doneCount != 1 {
		t.Errorf("expected 1 done item, got %d", doneCount)
	}
	if failedCount != 1 {
		t.Errorf("expected 1 failed item, got %d", failedCount)
	}
	if waitingCount != 1 {
		t.Errorf("expected 1 waiting item, got %d", waitingCount)
	}
}

func TestQueueRecovery_PRNotFoundInDB(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "no-pr-group", GitHub: "owner/repo"},
		},
	}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	orphanItem := models.QueueItem{
		PRID:      "orphan-pr",
		RepoGroup: "no-pr-group",
		Status:    "merging",
		AddedAt:   time.Now(),
	}
	db.Put(db.BucketQueueItems, "no-pr-group#orphan-pr", mustMarshalPR(orphanItem))

	m.Recover()

	var count int
	db.ForEach(db.BucketQueueItems, func(k, v []byte) error {
		count++
		return nil
	})
	if count != 1 {
		t.Errorf("expected 1 item (orphan should be reset to waiting), got %d", count)
	}
}
