package db_test

import (
	"encoding/json"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupConformance(t *testing.T) {
	t.Helper()
	testutil.NewTestDB(t)
}

func TestConformance_PutGet(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	tests := []struct {
		name   string
		bucket string
		key    string
		value  string
	}{
		{"simple", db.BucketConfig, "key1", "value1"},
		{"empty_value", db.BucketConfig, "key2", ""},
		{"binary_data", db.BucketConfig, "key3", string([]byte{0x00, 0x01, 0xFF})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := db.Put(tt.bucket, tt.key, []byte(tt.value)); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
			got, err := db.Get(tt.bucket, tt.key)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}
			if string(got) != tt.value {
				t.Errorf("Get = %q, want %q", string(got), tt.value)
			}
		})
	}
}

func TestConformance_Delete(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	db.Put(db.BucketConfig, "del-key", []byte("to-be-deleted"))

	if err := db.Delete(db.BucketConfig, "del-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err := db.Get(db.BucketConfig, "del-key")
	if err != db.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestConformance_ForEach(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	prefix := "foreach-"
	data := map[string]string{
		prefix + "a": "val-a",
		prefix + "b": "val-b",
		prefix + "c": "val-c",
	}

	for k, v := range data {
		db.Put(db.BucketConfig, k, []byte(v))
	}

	found := make(map[string]string)
	db.ForEach(db.BucketConfig, func(key, value []byte) error {
		found[string(key)] = string(value)
		return nil
	})

	for k, v := range data {
		if found[k] != v {
			_ = prefix
			t.Errorf("ForEach key %s = %q, want %q", k, found[k], v)
		}
	}
}

func TestConformance_BucketForEachPrefix(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	db.Put(db.BucketConfig, "prefix-aaa", []byte("1"))
	db.Put(db.BucketConfig, "prefix-bbb", []byte("2"))
	db.Put(db.BucketConfig, "other-ccc", []byte("3"))

	var results []string
	db.BucketForEachPrefix(db.BucketConfig, "prefix-", func(key, value []byte) error {
		results = append(results, string(key))
		return nil
	})

	if len(results) != 2 {
		t.Errorf("expected 2 prefix matches, got %d: %v", len(results), results)
	}
}

func TestConformance_PutPRWithIndex(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	pr := &models.PRRecord{
		ID:        "pr-123",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	key := "test-group#github#42"

	if err := db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber); err != nil {
		t.Fatalf("PutPRWithIndex failed: %v", err)
	}

	got, err := db.GetPRByIndex(pr.ID, pr.RepoGroup, 0)
	if err != nil {
		t.Fatalf("GetPRByIndex by ID failed: %v", err)
	}
	var gotPR models.PRRecord
	json.Unmarshal(got, &gotPR)
	if gotPR.ID != pr.ID {
		t.Errorf("GetPRByIndex by ID: got %q, want %q", gotPR.ID, pr.ID)
	}

	got2, err := db.GetPRByIndex("", pr.RepoGroup, pr.PRNumber)
	if err != nil {
		t.Fatalf("GetPRByIndex by rg+num failed: %v", err)
	}
	var gotPR2 models.PRRecord
	json.Unmarshal(got2, &gotPR2)
	if gotPR2.ID != pr.ID {
		t.Errorf("GetPRByIndex by rg+num: got %q, want %q", gotPR2.ID, pr.ID)
	}
}

func TestConformance_PutPRWithIndex_UpdateExisting(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	pr := &models.PRRecord{
		ID:        "pr-update",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  10,
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	key := "test-group#github#10"

	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)

	pr.State = "merged"
	updatedData, _ := json.Marshal(pr)
	db.PutPRWithIndex(key, updatedData, pr.ID, pr.RepoGroup, pr.PRNumber)

	got, _ := db.GetPRByIndex(pr.ID, pr.RepoGroup, 0)
	var gotPR models.PRRecord
	json.Unmarshal(got, &gotPR)
	if gotPR.State != "merged" {
		t.Errorf("expected updated state 'merged', got %q", gotPR.State)
	}
}

func TestConformance_WebhookDedup(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	deliveryID := "del-abc-123"
	ts, _ := time.Now().MarshalBinary()

	if err := db.PutWebhookDedup(deliveryID, ts); err != nil {
		t.Fatalf("PutWebhookDedup failed: %v", err)
	}

	got, err := db.GetWebhookDedup(deliveryID)
	if err != nil {
		t.Fatalf("GetWebhookDedup failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetWebhookDedup returned nil")
	}

	if err := db.DeleteWebhookDedup(deliveryID); err != nil {
		t.Fatalf("DeleteWebhookDedup failed: %v", err)
	}
	_, err = db.GetWebhookDedup(deliveryID)
	if err != db.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestConformance_WebhookHealth(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	rg := "test-group"
	platform := "github"
	now := time.Now()

	if err := db.PutWebhookHealth(rg, platform, now); err != nil {
		t.Fatalf("PutWebhookHealth failed: %v", err)
	}

	got, err := db.GetWebhookHealth(rg, platform)
	if err != nil {
		t.Fatalf("GetWebhookHealth failed: %v", err)
	}
	if got.Sub(now).Abs() > time.Second {
		t.Errorf("GetWebhookHealth = %v, want %v", got, now)
	}

	allHealth, err := db.ListWebhookHealth()
	if err != nil {
		t.Fatalf("ListWebhookHealth failed: %v", err)
	}
	key := rg + ":" + platform
	if _, ok := allHealth[key]; !ok {
		t.Errorf("ListWebhookHealth missing key %q", key)
	}
}

func TestConformance_IssuePRLinks(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	link := &models.IssuePRLink{
		IssueID:   "org/repo#100",
		PRID:      "pr-456",
		RepoGroup: "test-group",
		Platform:  "github",
		LinkType:  "fixes",
	}

	if err := db.PutIssuePRLink(link); err != nil {
		t.Fatalf("PutIssuePRLink failed: %v", err)
	}

	byIssue, err := db.GetIssuePRLinksByIssue(link.IssueID)
	if err != nil {
		t.Fatalf("GetIssuePRLinksByIssue failed: %v", err)
	}
	if len(byIssue) != 1 {
		t.Fatalf("expected 1 link by issue, got %d", len(byIssue))
	}
	if byIssue[0].PRID != link.PRID {
		t.Errorf("link PRID = %q, want %q", byIssue[0].PRID, link.PRID)
	}

	byPR, err := db.GetIssuePRLinksByPR(link.PRID)
	if err != nil {
		t.Fatalf("GetIssuePRLinksByPR failed: %v", err)
	}
	if len(byPR) != 1 {
		t.Fatalf("expected 1 link by PR, got %d", len(byPR))
	}
}

func TestConformance_PRDependencies(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	dep := &models.PRDependency{
		PRID:          "pr-child",
		DependsOnPRID: "pr-parent:github:10",
		DependsOnURL:  "https://github.com/org/repo/pull/10",
		RepoGroup:     "test-group",
		Platform:      "github",
	}

	if err := db.PutPRDependency(dep); err != nil {
		t.Fatalf("PutPRDependency failed: %v", err)
	}

	deps, err := db.GetPRDependenciesByPR(dep.PRID)
	if err != nil {
		t.Fatalf("GetPRDependenciesByPR failed: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}

	dependents, err := db.GetPRDependentsByPR("pr-parent:github:10")
	if err != nil {
		t.Fatalf("GetPRDependentsByPR failed: %v", err)
	}
	if len(dependents) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(dependents))
	}
}

func TestConformance_PRTemplate(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	tpl := &models.PRTemplate{
		RepoGroup:    "test-group",
		Platform:     "github",
		Content:      "## Description\n- [ ] Tests pass",
		HasChecklist: true,
	}

	if err := db.PutPRTemplate(tpl); err != nil {
		t.Fatalf("PutPRTemplate failed: %v", err)
	}

	got, err := db.GetPRTemplate(tpl.RepoGroup, tpl.Platform)
	if err != nil {
		t.Fatalf("GetPRTemplate failed: %v", err)
	}
	if got.Content != tpl.Content {
		t.Errorf("template content = %q, want %q", got.Content, tpl.Content)
	}
	if got.HasChecklist != tpl.HasChecklist {
		t.Errorf("HasChecklist = %v, want %v", got.HasChecklist, tpl.HasChecklist)
	}
}

func TestConformance_PRStack(t *testing.T) {
	setupConformance(t)
	defer db.Close()

	stack := &models.PRStack{
		ID:      "stack-1",
		Name:    "Test Stack",
		Author:  "testuser",
		State:   "open",
		Members: []models.StackMember{},
	}

	if err := db.PutPRStack(stack); err != nil {
		t.Fatalf("PutPRStack failed: %v", err)
	}

	got, err := db.GetPRStack(stack.ID)
	if err != nil {
		t.Fatalf("GetPRStack failed: %v", err)
	}
	if got.Name != stack.Name {
		t.Errorf("stack name = %q, want %q", got.Name, stack.Name)
	}

	stacks, err := db.ListPRStacks()
	if err != nil {
		t.Fatalf("ListPRStacks failed: %v", err)
	}
	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}

	if err := db.DeletePRStack(stack.ID); err != nil {
		t.Fatalf("DeletePRStack failed: %v", err)
	}
	_, err = db.GetPRStack(stack.ID)
	if err != db.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}
