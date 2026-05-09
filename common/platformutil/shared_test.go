package platformutil

import (
	"encoding/json"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupSharedTest(t *testing.T) {
	t.Helper()
	testutil.NewTestDB(t)
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"long string", "hello world", 8, "hello wo..."},
		{"empty string", "", 5, ""},
		{"maxLen 0", "hello", 0, "..."},
		{"maxLen 3", "hello", 3, "hel..."},
		{"unicode", "你好世界", 5, string([]byte("你好世界")[:5]) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestInactivityDays(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		lastActive time.Time
		minDays    int
		maxDays    int
	}{
		{"now", now, 0, 0},
		{"1 day ago", now.Add(-24 * time.Hour), 1, 1},
		{"3 days ago", now.Add(-72 * time.Hour), 3, 3},
		{"7 days ago", now.Add(-7 * 24 * time.Hour), 7, 7},
		{"future", now.Add(24 * time.Hour), -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InactivityDays(tt.lastActive)
			if got < tt.minDays || got > tt.maxDays {
				t.Errorf("InactivityDays() = %d, want between %d and %d", got, tt.minDays, tt.maxDays)
			}
		})
	}
}

func TestHasLabelStr(t *testing.T) {
	labels := []string{"bug", "feature", "urgent"}

	tests := []struct {
		name         string
		target       string
		defaultLabel string
		want         bool
	}{
		{"has label", "bug", "", true},
		{"missing label", "wontfix", "", false},
		{"empty target uses default", "", "feature", true},
		{"empty target default missing", "", "wontfix", false},
		{"target overrides default", "urgent", "bug", true},
		{"empty labels", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasLabelStr(labels, tt.target, tt.defaultLabel)
			if got != tt.want {
				t.Errorf("HasLabelStr(%v, %q, %q) = %v, want %v", labels, tt.target, tt.defaultLabel, got, tt.want)
			}
		})
	}
}

func TestHasLabelStr_EmptySlice(t *testing.T) {
	got := HasLabelStr(nil, "bug", "")
	if got {
		t.Error("HasLabelStr with nil labels should return false")
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"42", 42},
		{"0", 0},
		{"-1", -1},
		{"abc", 0},
		{"", 0},
		{"12abc", 12},
		{"999999", 999999},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseInt(tt.input)
			if got != tt.want {
				t.Errorf("ParseInt(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetPRByID(t *testing.T) {
	setupSharedTest(t)

	pr := models.PRRecord{
		ID:        "pr-123",
		RepoGroup: "mygroup",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("mygroup#github#42", data, "pr-123", "mygroup", 42)

	t.Run("find by ID string", func(t *testing.T) {
		found, err := GetPRByID("mygroup", "pr-123")
		if err != nil {
			t.Fatalf("GetPRByID failed: %v", err)
		}
		if found.ID != "pr-123" {
			t.Errorf("ID = %q, want pr-123", found.ID)
		}
		if found.PRNumber != 42 {
			t.Errorf("PRNumber = %d, want 42", found.PRNumber)
		}
	})

	t.Run("find by number", func(t *testing.T) {
		found, err := GetPRByID("mygroup", "42")
		if err != nil {
			t.Fatalf("GetPRByID failed: %v", err)
		}
		if found.PRNumber != 42 {
			t.Errorf("PRNumber = %d, want 42", found.PRNumber)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := GetPRByID("mygroup", "999")
		if err == nil {
			t.Error("expected error for non-existent PR")
		}
	})

	t.Run("wrong repo group", func(t *testing.T) {
		_, err := GetPRByID("othergroup", "pr-123")
		if err == nil {
			t.Error("expected error for wrong repo group")
		}
	})
}
