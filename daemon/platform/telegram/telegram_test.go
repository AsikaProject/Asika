package telegram

import (
	"testing"

	"asika/common/platforms"
	commonutil "asika/common/platformutil"
)

func TestBotCreation(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	if bot == nil {
		t.Fatal("bot should not be nil")
	}
	if len(bot.adminIDs) != 2 {
		t.Errorf("expected 2 admin IDs, got %d", len(bot.adminIDs))
	}
}

func TestIsAdmin_EmptyAdminIDs(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	bot.adminIDs = map[int64]bool{}
	if !bot.isAdmin(nil) {
		t.Error("with empty adminIDs, everyone should be admin")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 10, ""},
	}
	for _, tt := range tests {
		got := commonutil.Truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestGetClientForPlatform(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	if bot.clients == nil {
		t.Fatal("clients should not be nil")
	}
	if _, ok := bot.clients[platforms.PlatformGitHub]; !ok {
		t.Error("expected github client")
	}
	if _, ok := bot.clients["unknown"]; ok {
		t.Error("expected no unknown platform client")
	}
}

func TestBotStop(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	bot.Stop()
}
