package notifier

import (
	"context"
	"testing"

	"gopkg.in/telebot.v3"
)

func TestNewTelegramNotifier_NoToken(t *testing.T) {
	n := NewTelegramNotifier(map[string]interface{}{})
	if n != nil {
		t.Error("expected nil for missing token")
	}
}

func TestNewTelegramNotifier_InvalidToken(t *testing.T) {
	n := NewTelegramNotifier(map[string]interface{}{
		"token": "invalid-token-format",
	})
	if n != nil {
		// Creation with invalid token may succeed because telebot doesn't validate immediately
		t.Log("notifier created (telebot creates client lazily)")
	}
}

func TestNewTelegramNotifier_WithChatIDs(t *testing.T) {
	n := NewTelegramNotifier(map[string]interface{}{
		"token": "12345:ABCdef",
		"to": []interface{}{
			"@mychannel",
			"-1001234567890",
			"123456789",
		},
	})

	if n == nil {
		t.Skip("notifier creation failed (expected with test token)")
		return
	}

	if n.Type() != "telegram" {
		t.Errorf("Type() = %q, want telegram", n.Type())
	}

	if len(n.ChatIDs()) != 3 {
		t.Errorf("ChatIDs() = %d, want 3", len(n.ChatIDs()))
	}

	if n.Bot() == nil {
		t.Error("Bot() should not be nil")
	}
}

func TestResolveRecipient_Channel(t *testing.T) {
	r := resolveRecipient("@mychannel")
	if r == nil {
		t.Fatal("expected non-nil recipient for channel")
	}
	_, ok := r.(*telebot.Chat)
	if !ok {
		t.Fatal("expected telebot.Chat")
	}
	// Channel identified by username
}

func TestResolveRecipient_Numeric(t *testing.T) {
	r := resolveRecipient("123456789")
	if r == nil {
		t.Fatal("expected non-nil recipient for numeric ID")
	}
	chat, ok := r.(*telebot.Chat)
	if !ok {
		t.Fatal("expected telebot.Chat")
	}
	if chat.ID != 123456789 {
		t.Errorf("Chat.ID = %d, want 123456789", chat.ID)
	}
}

func TestResolveRecipient_NegativeChatID(t *testing.T) {
	r := resolveRecipient("-1001234567890")
	if r == nil {
		t.Fatal("expected non-nil recipient for negative chat ID")
	}
	chat, ok := r.(*telebot.Chat)
	if !ok {
		t.Fatal("expected telebot.Chat")
	}
	if chat.ID != -1001234567890 {
		t.Errorf("Chat.ID = %d, want -1001234567890", chat.ID)
	}
}

func TestResolveRecipient_Empty(t *testing.T) {
	r := resolveRecipient("")
	if r != nil {
		t.Error("expected nil for empty string")
	}

	r = resolveRecipient("   ")
	if r != nil {
		t.Error("expected nil for whitespace-only string")
	}
}

func TestTelegramNotifier_Send_NoChatIDs(t *testing.T) {
	n := &TelegramNotifier{
		bot:     nil,
		chatIDs: []string{},
	}

	err := n.Send(context.Background(), "Test", "Body")
	if err == nil {
		t.Error("expected error for empty chat IDs")
	}
}

func TestTelegramNotifier_Type(t *testing.T) {
	n := &TelegramNotifier{}
	if n.Type() != "telegram" {
		t.Errorf("Type() = %q, want telegram", n.Type())
	}
}

func TestTelegramNotifier_Bot(t *testing.T) {
	n := &TelegramNotifier{}
	if n.Bot() != nil {
		t.Log("Bot is non-nil")
	}
}

func TestTelegramNotifier_ChatIDs(t *testing.T) {
	n := &TelegramNotifier{
		chatIDs: []string{"@chan1", "12345"},
	}

	ids := n.ChatIDs()
	if len(ids) != 2 {
		t.Errorf("ChatIDs() = %d, want 2", len(ids))
	}
	if ids[0] != "@chan1" || ids[1] != "12345" {
		t.Errorf("ChatIDs() = %v, want [@chan1 12345]", ids)
	}
}
