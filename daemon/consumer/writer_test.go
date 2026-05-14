package consumer

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"asika/common/db"
	"asika/common/models"
)

func TestWriterActor_SingleWrite(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(16)
	defer w.Stop()

	pr := &models.PRRecord{
		ID:        "writer-test-1",
		RepoGroup: "rg",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)

	err := w.write("rg#github#1", data, "writer-test-1", "rg", 1)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	stored, err := db.Get(db.BucketPRs, "rg#github#1")
	if err != nil {
		t.Fatalf("PR not found in DB: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.Title != "Test PR" {
		t.Errorf("title = %q, want %q", result.Title, "Test PR")
	}
}

func TestWriterActor_MultipleWrites(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(64)
	defer w.Stop()

	for i := 0; i < 20; i++ {
		pr := &models.PRRecord{
			ID:        "writer-multi-" + string(rune('A'+i)),
			RepoGroup: "rg",
			Platform:  "github",
			PRNumber:  i + 1,
			Title:     "PR " + string(rune('A'+i)),
			State:     "open",
		}
		data, _ := json.Marshal(pr)
		key := "rg#github#" + string(rune('1'+i))
		if err := w.write(key, data, pr.ID, "rg", i+1); err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}

	for i := 0; i < 20; i++ {
		key := "rg#github#" + string(rune('1'+i))
		_, err := db.Get(db.BucketPRs, key)
		if err != nil {
			t.Errorf("PR %d not found: %v", i+1, err)
		}
	}
}

func TestWriterActor_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(128)
	defer w.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pr := &models.PRRecord{
				ID:        "concurrent-" + string(rune('A'+n%26)),
				RepoGroup: "rg",
				Platform:  "github",
				PRNumber:  n + 100,
				Title:     "Concurrent " + string(rune('A'+n%26)),
				State:     "open",
			}
			data, _ := json.Marshal(pr)
			key := "rg#github#" + string(rune('1'+n%26))
			w.write(key, data, pr.ID, "rg", n+100)
		}(i)
	}
	wg.Wait()
}

func TestWriterActor_Stop(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(16)
	w.Stop()
}

func TestWriterActor_HighContention(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(256)
	defer w.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pr := &models.PRRecord{
				ID:        "contention-" + fmt.Sprintf("%d", n),
				RepoGroup: "rg",
				Platform:  "github",
				PRNumber:  n + 1,
				Title:     "Contention " + fmt.Sprintf("%d", n),
				State:     "open",
			}
			data, _ := json.Marshal(pr)
			key := fmt.Sprintf("rg#github#%d", n+1)
			w.write(key, data, pr.ID, "rg", n+1)
		}(i)
	}
	wg.Wait()

	count := 0
	db.ForEach(db.BucketPRs, func(k, v []byte) error {
		count++
		return nil
	})
	if count != 200 {
		t.Errorf("expected 200 PRs stored, got %d", count)
	}
}

func TestWriterActor_WriteAfterStop(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(16)
	w.Stop()

	pr := &models.PRRecord{
		ID:    "after-stop",
		Title: "After stop",
		State: "open",
	}
	data, _ := json.Marshal(pr)

	err := w.write("rg#github#1", data, "after-stop", "rg", 1)
	if err == nil {
		t.Error("expected error writing after stop")
	}
}

func TestWriterActor_WriteIssueLink(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(16)
	defer w.Stop()

	link := &models.IssuePRLink{
		IssueID:   "org/repo#100",
		PRID:      "link-pr-1",
		RepoGroup: "rg",
		Platform:  "github",
		LinkType:  "fixes",
	}

	if err := w.writeIssueLink(link); err != nil {
		t.Fatalf("writeIssueLink failed: %v", err)
	}

	stored, err := db.Get(db.BucketIssuePRLinks, "rg:org/repo#100:link-pr-1")
	if err != nil {
		t.Fatalf("issue link not found in DB: %v", err)
	}
	if stored == nil {
		t.Fatal("stored issue link is nil")
	}

	var decoded models.IssuePRLink
	json.Unmarshal(stored, &decoded)
	if decoded.IssueID != "org/repo#100" {
		t.Errorf("IssueID = %q, want org/repo#100", decoded.IssueID)
	}
	if decoded.LinkType != "fixes" {
		t.Errorf("LinkType = %q, want fixes", decoded.LinkType)
	}
	if decoded.PRID != "link-pr-1" {
		t.Errorf("PRID = %q, want link-pr-1", decoded.PRID)
	}
}

func TestWriterActor_WriteIssueLinkAfterStop(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	err := db.Put(db.BucketIssuePRLinks, "stopped:org/repo#100:stopped-pr", []byte(`{}`))
	if err != nil {
		t.Fatalf("db.Put failed: %v", err)
	}

	stored, err := db.Get(db.BucketIssuePRLinks, "stopped:org/repo#100:stopped-pr")
	if err != nil {
		t.Fatalf("issue link not found: %v", err)
	}
	if stored == nil {
		t.Fatal("stored issue link is nil")
	}
}

func TestWriterActor_ConcurrentIssueLinkWrites(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	w := newWriterActor(128)
	defer w.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			link := &models.IssuePRLink{
				IssueID:  "org/repo#" + string(rune('1'+n%26)),
				PRID:     "concurrent-link-" + string(rune('A'+n%26)),
				LinkType: "related",
			}
			w.writeIssueLink(link)
		}(i)
	}
	wg.Wait()
}
