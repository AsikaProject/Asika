package feed

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"asika/common/events"
	"asika/common/models"
)

func TestDefaultFeedConfig(t *testing.T) {
	cfg := DefaultFeedConfig()
	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.Title != "Asika PR Feed" {
		t.Errorf("expected Title 'Asika PR Feed', got %q", cfg.Title)
	}
	if cfg.MaxItems != 50 {
		t.Errorf("expected MaxItems 50, got %d", cfg.MaxItems)
	}
	if cfg.PublicFeed {
		t.Error("expected PublicFeed to be false")
	}
}

func TestNewFeed(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		cfg := models.FeedConfig{Enabled: true, Title: "Test", MaxItems: 10, PublicFeed: true}
		f := NewFeed(cfg)
		if f == nil {
			t.Fatal("expected non-nil Feed")
		}
		if f.maxItems != 10 {
			t.Errorf("maxItems = %d, want 10", f.maxItems)
		}
		if !f.enabled {
			t.Error("expected enabled")
		}
		if f.title != "Test" {
			t.Errorf("title = %q, want 'Test'", f.title)
		}
		if !f.publicFeed {
			t.Error("expected publicFeed")
		}
	})

	t.Run("with zero maxItems defaults to 50", func(t *testing.T) {
		cfg := models.FeedConfig{MaxItems: 0}
		f := NewFeed(cfg)
		if f.maxItems != 50 {
			t.Errorf("maxItems = %d, want 50", f.maxItems)
		}
	})

	t.Run("with negative maxItems defaults to 50", func(t *testing.T) {
		cfg := models.FeedConfig{MaxItems: -5}
		f := NewFeed(cfg)
		if f.maxItems != 50 {
			t.Errorf("maxItems = %d, want 50", f.maxItems)
		}
	})
}

func TestUpdateConfig(t *testing.T) {
	cfg := models.FeedConfig{Enabled: true, Title: "Old", MaxItems: 10, PublicFeed: false}
	f := NewFeed(cfg)

	newCfg := models.FeedConfig{Enabled: false, Title: "New", MaxItems: 20, PublicFeed: true}
	f.UpdateConfig(newCfg)

	if f.enabled {
		t.Error("expected disabled")
	}
	if f.title != "New" {
		t.Errorf("title = %q, want 'New'", f.title)
	}
	if f.maxItems != 20 {
		t.Errorf("maxItems = %d, want 20", f.maxItems)
	}
	if !f.publicFeed {
		t.Error("expected publicFeed")
	}
}

func TestAddEvent(t *testing.T) {
	f := NewFeed(DefaultFeedConfig())

	t.Run("nil PR does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("AddEvent panicked with nil PR: %v", r)
			}
		}()
		f.AddEvent(events.EventPROpened, nil)
		if len(f.items) != 0 {
			t.Errorf("expected 0 items, got %d", len(f.items))
		}
	})

	t.Run("valid PR opened event", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Platform:  "github",
			PRNumber:  1,
			Title:     "Test PR",
			Author:    "alice",
			HTMLURL:   "https://github.com/org/repo/pull/1",
		}
		f.AddEvent(events.EventPROpened, pr)
		if len(f.items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(f.items))
		}
		item := f.items[0]
		if item.PRNumber != 1 {
			t.Errorf("PRNumber = %d, want 1", item.PRNumber)
		}
		if item.Author != "alice" {
			t.Errorf("Author = %q, want 'alice'", item.Author)
		}
		if item.RepoGroup != "test-group" {
			t.Errorf("RepoGroup = %q, want 'test-group'", item.RepoGroup)
		}
		if !strings.Contains(item.Title, "OPENED") {
			t.Errorf("Title should contain 'OPENED', got %q", item.Title)
		}
	})

	t.Run("unknown event type is skipped", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Platform:  "github",
			PRNumber:  2,
			Title:     "Test PR 2",
			Author:    "bob",
			HTMLURL:   "https://github.com/org/repo/pull/2",
		}
		f.AddEvent(events.EventType("unknown"), pr)
		if len(f.items) != 1 {
			t.Errorf("expected 1 item (unknown event skipped), got %d", len(f.items))
		}
	})
}

func TestAddEventAllTypes(t *testing.T) {
	tests := []struct {
		eventType events.EventType
		label     string
	}{
		{events.EventPROpened, "opened"},
		{events.EventPRMerged, "merged"},
		{events.EventPRClosed, "closed"},
		{events.EventPRApproved, "approved"},
		{events.EventPRReopened, "reopened"},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			f := NewFeed(DefaultFeedConfig())
			pr := &models.PRRecord{
				RepoGroup: "grp",
				Platform:  "github",
				PRNumber:  1,
				Title:     "PR",
				Author:    "user",
				HTMLURL:   "https://example.com/pr/1",
			}
			f.AddEvent(tt.eventType, pr)
			if len(f.items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(f.items))
			}
			if !strings.Contains(f.items[0].Title, strings.ToUpper(tt.label)) {
				t.Errorf("expected title to contain %q, got %q", strings.ToUpper(tt.label), f.items[0].Title)
			}
		})
	}
}

func TestGetItemsSorted(t *testing.T) {
	f := NewFeed(models.FeedConfig{MaxItems: 10})

	for i := 0; i < 5; i++ {
		pr := &models.PRRecord{
			RepoGroup: "grp",
			Platform:  "github",
			PRNumber:  i + 1,
			Title:     "PR",
			Author:    "user",
			HTMLURL:   "https://example.com/pr/1",
		}
		f.AddEvent(events.EventPROpened, pr)
		time.Sleep(10 * time.Millisecond)
	}

	items := f.GetItems()
	if len(items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(items))
	}

	for i := 1; i < len(items); i++ {
		if items[i-1].PubDate.Before(items[i].PubDate) {
			t.Errorf("items not sorted descending: item[%d].PubDate (%v) < item[%d].PubDate (%v)",
				i-1, items[i-1].PubDate, i, items[i].PubDate)
		}
	}
}

func TestGetItemsForRepoGroup(t *testing.T) {
	f := NewFeed(DefaultFeedConfig())

	pr1 := &models.PRRecord{RepoGroup: "group-a", Platform: "github", PRNumber: 1, Title: "PR1", Author: "a", HTMLURL: "https://example.com/1"}
	pr2 := &models.PRRecord{RepoGroup: "group-b", Platform: "gitlab", PRNumber: 2, Title: "PR2", Author: "b", HTMLURL: "https://example.com/2"}
	pr3 := &models.PRRecord{RepoGroup: "group-a", Platform: "gitea", PRNumber: 3, Title: "PR3", Author: "c", HTMLURL: "https://example.com/3"}

	f.AddEvent(events.EventPROpened, pr1)
	f.AddEvent(events.EventPROpened, pr2)
	f.AddEvent(events.EventPROpened, pr3)

	items := f.GetItemsForRepoGroup("group-a")
	if len(items) != 2 {
		t.Fatalf("expected 2 items for group-a, got %d", len(items))
	}
	for _, item := range items {
		if item.RepoGroup != "group-a" {
			t.Errorf("expected RepoGroup 'group-a', got %q", item.RepoGroup)
		}
	}

	items = f.GetItemsForRepoGroup("nonexistent")
	if len(items) != 0 {
		t.Errorf("expected 0 items for nonexistent group, got %d", len(items))
	}
}

func TestRingBufferOverflow(t *testing.T) {
	f := NewFeed(models.FeedConfig{MaxItems: 3})

	for i := 0; i < 5; i++ {
		pr := &models.PRRecord{
			RepoGroup: "grp",
			Platform:  "github",
			PRNumber:  i + 1,
			Title:     "PR",
			Author:    "user",
			HTMLURL:   "https://example.com/pr/1",
		}
		f.AddEvent(events.EventPROpened, pr)
	}

	items := f.GetItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 items (maxItems), got %d", len(items))
	}

	if items[0].PRNumber != 3 {
		t.Errorf("expected oldest remaining PRNumber 3, got %d", items[0].PRNumber)
	}
	if items[2].PRNumber != 5 {
		t.Errorf("expected newest PRNumber 5, got %d", items[2].PRNumber)
	}
}

func TestGenerateRSS(t *testing.T) {
	f := NewFeed(models.FeedConfig{Title: "Test Feed", MaxItems: 10})
	pr := &models.PRRecord{
		RepoGroup: "grp",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Test PR",
		Author:    "alice",
		HTMLURL:   "https://github.com/org/repo/pull/42",
	}
	f.AddEvent(events.EventPROpened, pr)

	items := f.GetItems()
	data, err := f.GenerateRSS(items, "https://asika.example.com")
	if err != nil {
		t.Fatalf("GenerateRSS error: %v", err)
	}

	var rss RSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		t.Fatalf("RSS XML parse error: %v", err)
	}

	if rss.Version != "2.0" {
		t.Errorf("RSS version = %q, want '2.0'", rss.Version)
	}
	if rss.Channel.Title != "Test Feed" {
		t.Errorf("Channel title = %q, want 'Test Feed'", rss.Channel.Title)
	}
	if rss.Channel.Link != "https://asika.example.com" {
		t.Errorf("Channel link = %q, want 'https://asika.example.com'", rss.Channel.Link)
	}
	if len(rss.Channel.Items) != 1 {
		t.Fatalf("expected 1 channel item, got %d", len(rss.Channel.Items))
	}
	if !strings.Contains(rss.Channel.Items[0].Title, "OPENED") {
		t.Errorf("item title should contain 'OPENED', got %q", rss.Channel.Items[0].Title)
	}
}

func TestGenerateRSSWithNilPR(t *testing.T) {
	f := NewFeed(DefaultFeedConfig())
	data, err := f.GenerateRSS(nil, "https://example.com")
	if err != nil {
		t.Fatalf("GenerateRSS error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty RSS output")
	}
}

func TestGetConfig(t *testing.T) {
	cfg := models.FeedConfig{Enabled: true, Title: "My Feed", MaxItems: 25, PublicFeed: true}
	f := NewFeed(cfg)

	got := f.GetConfig()
	if got.Enabled != true {
		t.Error("expected Enabled true")
	}
	if got.Title != "My Feed" {
		t.Errorf("Title = %q, want 'My Feed'", got.Title)
	}
	if got.MaxItems != 25 {
		t.Errorf("MaxItems = %d, want 25", got.MaxItems)
	}
	if got.PublicFeed != true {
		t.Error("expected PublicFeed true")
	}
}

func TestInitAndGlobalFeed(t *testing.T) {
	cfg := DefaultFeedConfig()
	InitGlobalFeed(cfg)

	gf := GlobalFeed()
	if gf == nil {
		t.Fatal("expected non-nil global feed")
	}
}

func TestGlobalFeedSingleton(t *testing.T) {
	InitGlobalFeed(DefaultFeedConfig())
	gf1 := GlobalFeed()
	gf2 := GlobalFeed()
	if gf1 != gf2 {
		t.Error("GlobalFeed should return the same instance")
	}
}

func TestActionLabel(t *testing.T) {
	tests := []struct {
		eventType events.EventType
		expected  string
	}{
		{events.EventPROpened, "OPENED"},
		{events.EventPRMerged, "MERGED"},
		{events.EventPRClosed, "CLOSED"},
		{events.EventPRApproved, "APPROVED"},
		{events.EventPRReopened, "REOPENED"},
		{events.EventType("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := actionLabel(tt.eventType)
			if got != tt.expected {
				t.Errorf("actionLabel(%q) = %q, want %q", tt.eventType, got, tt.expected)
			}
		})
	}
}

func TestItemsSorted(t *testing.T) {
	items := []FeedItem{
		{Title: "first", PubDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Title: "third", PubDate: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)},
		{Title: "second", PubDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
	}

	sorted := itemsSorted(items)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 items, got %d", len(sorted))
	}
	if sorted[0].Title != "third" {
		t.Errorf("expected first item 'third', got %q", sorted[0].Title)
	}
	if sorted[1].Title != "second" {
		t.Errorf("expected second item 'second', got %q", sorted[1].Title)
	}
	if sorted[2].Title != "first" {
		t.Errorf("expected third item 'first', got %q", sorted[2].Title)
	}

	original := items[0]
	if original.Title != "first" {
		t.Error("itemsSorted should not modify the original slice")
	}
}
