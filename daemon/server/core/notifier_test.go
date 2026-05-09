package core

import (
	"context"
	"testing"
	"time"

	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
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
