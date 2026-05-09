package consumer

import (
	"encoding/json"
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
