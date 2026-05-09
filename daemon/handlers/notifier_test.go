package handlers

import (
	"testing"

	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/testutil"
)

func TestSetNotifyFunc(t *testing.T) {
	called := false
	var gotTitle, gotBody string

	SetNotifyFunc(func(title, body string) {
		called = true
		gotTitle = title
		gotBody = body
	})

	sendNotifications("test-title", "test-body")

	if !called {
		t.Error("expected notifyFunc to be called")
	}
	if gotTitle != "test-title" {
		t.Errorf("title = %q, want test-title", gotTitle)
	}
	if gotBody != "test-body" {
		t.Errorf("body = %q, want test-body", gotBody)
	}

	// Reset
	SetNotifyFunc(nil)
}

func TestSetNotifyFunc_NilFallback(t *testing.T) {
	SetNotifyFunc(nil)
	globalNotifiers = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("sendNotifications panicked with nil notifyFunc: %v", r)
		}
	}()

	sendNotifications("title", "body")
}

func TestInitNotifiers(t *testing.T) {
	globalNotifiers = nil

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}

	cfg := &models.Config{
		Notify: []models.NotifyConfig{
			{
				Type: "github_at",
				Config: map[string]interface{}{
					"to":    []interface{}{"user1"},
					"owner": "org",
					"repo":  "repo",
				},
			},
		},
	}

	InitNotifiers(cfg, clients)

	if len(globalNotifiers) == 0 {
		t.Error("expected notifiers to be initialized")
	}

	// Reset
	globalNotifiers = nil
}

func TestWirePlatformNotifiersInHandlers(t *testing.T) {
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}

	notifiers := []notifier.Notifier{
		notifier.NewGitHubAtNotifier(map[string]interface{}{
			"to":    []interface{}{"user1"},
			"owner": "org",
			"repo":  "repo",
		}),
	}

	notifier.WirePlatformNotifiers(notifiers, clients)

	pn, ok := notifiers[0].(*notifier.PlatformNotifier)
	if !ok {
		t.Fatal("expected PlatformNotifier")
	}
	if pn.Type() != "github_at" {
		t.Errorf("Type() = %q, want github_at", pn.Type())
	}
}

func TestCreateNotifierFromNotifyConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfgType  string
		wantNil  bool
		wantType string
	}{
		{"smtp", "smtp", false, "smtp"},
		{"wecom", "wecom", false, "wecom"},
		{"github_at", "github_at", false, "github_at"},
		{"gitlab_at", "gitlab_at", false, "gitlab_at"},
		{"gitea_at", "gitea_at", false, "gitea_at"},
		{"telegram", "telegram", true, ""},
		{"feishu", "feishu", false, "feishu"},
		{"discord", "discord", true, ""},
		{"dingtalk", "dingtalk", true, ""},
		{"unknown", "unknown_type", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := models.NotifyConfig{
				Type:   tt.cfgType,
				Config: map[string]interface{}{},
			}
			n := createNotifierFromNotifyConfig(nc)

			if tt.wantNil {
				if n != nil {
					t.Errorf("expected nil for type %q", tt.cfgType)
				}
				return
			}

			if n == nil {
				t.Fatalf("expected non-nil notifier for type %q", tt.cfgType)
			}
			if n.Type() != tt.wantType {
				t.Errorf("Type() = %q, want %q", n.Type(), tt.wantType)
			}
		})
	}
}

func TestSendNotifications_ViaGlobalNotifiers(t *testing.T) {
	SetNotifyFunc(nil)

	mockClient := testutil.NewMockPlatformClient()
	mockClient.PRs["org/repo#1"] = &models.PRRecord{
		ID:       "1",
		PRNumber: 1,
		State:    "open",
	}

	pn := notifier.NewGitHubAtNotifier(map[string]interface{}{
		"to":    []interface{}{"user1"},
		"owner": "org",
		"repo":  "repo",
	})
	pn.SetClient(mockClient)

	globalNotifiers = []notifier.Notifier{pn}

	// Should not panic
	sendNotifications("Test", "Body")

	// Reset
	globalNotifiers = nil
}
