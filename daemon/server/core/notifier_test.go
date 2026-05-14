package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/testutil"
)

func TestCreateNotifierFromConfig(t *testing.T) {
	tests := []struct {
		name         string
		notifierType string
		config       map[string]interface{}
		expectNil    bool
	}{
		{"smtp valid", "smtp", map[string]interface{}{"host": "smtp.test"}, false},
		{"wecom valid", "wecom", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"github_at valid", "github_at", map[string]interface{}{"owner": "org", "repo": "repo"}, false},
		{"gitlab_at valid", "gitlab_at", map[string]interface{}{"project": "123"}, false},
		{"gitea_at valid", "gitea_at", map[string]interface{}{"owner": "org"}, false},
		{"telegram invalid token", "telegram", map[string]interface{}{"token": "invalid"}, true},
		{"feishu valid", "feishu", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"discord invalid token", "discord", map[string]interface{}{"token": "invalid"}, true},
		{"dingtalk valid", "dingtalk", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"slack valid", "slack", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"webhook valid", "webhook", map[string]interface{}{"url": "http://test"}, false},
		{"unknown type", "unknown", map[string]interface{}{}, true},
		{"empty type", "", map[string]interface{}{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := models.NotifyConfig{Type: tt.notifierType, Config: tt.config}
			n := createNotifierFromConfig(nc)
			if tt.expectNil && n != nil {
				t.Errorf("expected nil for type %q, got %v", tt.notifierType, n)
			}
			if !tt.expectNil && n == nil {
				t.Errorf("expected non-nil for type %q", tt.notifierType)
			}
		})
	}
}

func TestInitNotifiers(t *testing.T) {
	cfg := &models.Config{
		Notify: []models.NotifyConfig{
			{Type: "webhook", Config: map[string]interface{}{"url": "http://test"}},
			{Type: "unknown", Config: map[string]interface{}{}},
			{Type: "slack", Config: map[string]interface{}{"webhook_url": "http://test"}},
		},
	}

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	InitNotifiers(cfg, clients)

	if len(globalNotifiers) != 2 {
		t.Errorf("expected 2 notifiers, got %d", len(globalNotifiers))
	}
}

func TestInitNotifiers_Empty(t *testing.T) {
	cfg := &models.Config{Notify: []models.NotifyConfig{}}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	InitNotifiers(cfg, clients)

	if len(globalNotifiers) != 0 {
		t.Errorf("expected 0 notifiers, got %d", len(globalNotifiers))
	}
}

func TestSendNotification_NoNotifiers(t *testing.T) {
	globalNotifiers = nil
	SendNotification(context.Background(), "title", "body")
}

func TestSendNotification_WithNotifiers(t *testing.T) {
	globalNotifiers = []notifier.Notifier{
		notifier.NewWebhookNotifier(map[string]interface{}{"url": "http://localhost:19999"}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	SendNotification(ctx, "test title", "test body")
}

func TestSendNotificationSync(t *testing.T) {
	globalNotifiers = nil
	SendNotificationSync("title", "body")
}

func TestSendNotification_WithEventType(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	globalNotifiers = []notifier.Notifier{
		notifier.NewWebhookNotifier(map[string]interface{}{"url": "http://localhost:19999"}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	SendNotificationWithContext(ctx, "test", "body", "pr_opened", "123", "webhook")

	globalNotifiers = nil
}

func TestSendNotification_QuietHoursBypass(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	globalNotifiers = []notifier.Notifier{
		notifier.NewWebhookNotifier(map[string]interface{}{"url": "http://localhost:19999"}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	SendNotificationUrgent(ctx, "urgent", "body")

	globalNotifiers = nil
}

func TestIsNotifierEnabledForAnyUser_NoPrefs(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	result := isNotifierEnabledForAnyUser("smtp", "pr_opened")
	if !result {
		t.Error("expected true when no prefs exist")
	}
}

func TestIsNotifierEnabledForAnyUser_AllDisabled(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()
	ResetNotifierPrefsCache()

	db.PutNotificationPrefs("alice", mustMarshalJSON(models.NotificationPreferences{
		Username: "alice",
		Enabled:  false,
	}))

	result := isNotifierEnabledForAnyUser("smtp", "pr_opened")
	if result {
		t.Error("expected false when all users disabled")
	}
}

func TestIsNotifierEnabledForAnyUser_Enabled(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()
	ResetNotifierPrefsCache()

	db.PutNotificationPrefs("alice", mustMarshalJSON(models.NotificationPreferences{
		Username: "alice",
		Enabled:  true,
	}))

	result := isNotifierEnabledForAnyUser("smtp", "pr_opened")
	if !result {
		t.Error("expected true when user has enabled prefs")
	}
}

func TestIsNotifierEnabledForAnyUser_NotifierFiltered(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()
	ResetNotifierPrefsCache()

	db.PutNotificationPrefs("alice", mustMarshalJSON(models.NotificationPreferences{
		Username:         "alice",
		Enabled:          true,
		EnabledNotifiers: []string{"telegram"},
	}))

	result := isNotifierEnabledForAnyUser("smtp", "pr_opened")
	if result {
		t.Error("expected false when user only enabled telegram, not smtp")
	}

	result = isNotifierEnabledForAnyUser("telegram", "pr_opened")
	if !result {
		t.Error("expected true when user enabled telegram")
	}
}

func TestIsNotifierEnabledForAnyUser_EventFiltered(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()
	ResetNotifierPrefsCache()

	db.PutNotificationPrefs("alice", mustMarshalJSON(models.NotificationPreferences{
		Username:   "alice",
		Enabled:    true,
		EventPrefs: map[string]bool{"pr_opened": false},
	}))

	result := isNotifierEnabledForAnyUser("smtp", "pr_opened")
	if result {
		t.Error("expected false when user disabled pr_opened event")
	}

	result = isNotifierEnabledForAnyUser("smtp", "pr_closed")
	if !result {
		t.Error("expected true when event not in user's disabled list")
	}
}

func TestIsNotifierEnabledForAnyUser_MultipleUsers(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()
	ResetNotifierPrefsCache()

	db.PutNotificationPrefs("alice", mustMarshalJSON(models.NotificationPreferences{
		Username: "alice",
		Enabled:  false,
	}))
	db.PutNotificationPrefs("bob", mustMarshalJSON(models.NotificationPreferences{
		Username: "bob",
		Enabled:  true,
	}))

	result := isNotifierEnabledForAnyUser("smtp", "pr_opened")
	if !result {
		t.Error("expected true when at least one user (bob) is enabled")
	}
}

func TestIsNotifierEnabledForAnyUser_NoEventType(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()
	ResetNotifierPrefsCache()

	db.PutNotificationPrefs("alice", mustMarshalJSON(models.NotificationPreferences{
		Username: "alice",
		Enabled:  true,
	}))

	result := isNotifierEnabledForAnyUser("smtp", "")
	if !result {
		t.Error("expected true when no event type specified")
	}
}

func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
