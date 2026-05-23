package init

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrompt(t *testing.T) {
	t.Run("with default value user enters empty", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n"))
		result := Prompt(reader, "Enter name", "default-name")
		if result != "default-name" {
			t.Errorf("expected 'default-name', got %q", result)
		}
	})

	t.Run("with default value user enters custom", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("custom-value\n"))
		result := Prompt(reader, "Enter name", "default-name")
		if result != "custom-value" {
			t.Errorf("expected 'custom-value', got %q", result)
		}
	})

	t.Run("without default user enters value", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("my-value\n"))
		result := Prompt(reader, "Enter name", "")
		if result != "my-value" {
			t.Errorf("expected 'my-value', got %q", result)
		}
	})

	t.Run("without default user enters empty", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n"))
		result := Prompt(reader, "Enter name", "")
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})
}

func TestPromptSecret(t *testing.T) {
	t.Run("returns trimmed input", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("  secret-token  \n"))
		result := PromptSecret(reader, "Enter token")
		if result != "secret-token" {
			t.Errorf("expected 'secret-token', got %q", result)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n"))
		result := PromptSecret(reader, "Enter token")
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})
}

func TestChoose(t *testing.T) {
	options := []string{"alpha", "beta", "gamma"}

	t.Run("default on empty input", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n"))
		result := Choose(reader, "Pick one", options, "beta")
		if result != "beta" {
			t.Errorf("expected 'beta', got %q", result)
		}
	})

	t.Run("valid index", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("1\n"))
		result := Choose(reader, "Pick one", options, "beta")
		if result != "alpha" {
			t.Errorf("expected 'alpha', got %q", result)
		}
	})

	t.Run("invalid index returns raw input", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("99\n"))
		result := Choose(reader, "Pick one", options, "beta")
		if result != "99" {
			t.Errorf("expected '99', got %q", result)
		}
	})

	t.Run("non-numeric input returns raw input", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("hello\n"))
		result := Choose(reader, "Pick one", options, "beta")
		if result != "hello" {
			t.Errorf("expected 'hello', got %q", result)
		}
	})
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"N\n", false},
		{"no\n", false},
		{"\n", false},
		{"maybe\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			result := Confirm(reader, "Continue?")
			if result != tt.expected {
				t.Errorf("Confirm(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a, b, c", []string{"a", "b", "c"}},
		{"  x  , y ,  z  ", []string{"x", "y", "z"}},
		{"", nil},
		{"single", []string{"single"}},
		{"a,,b", []string{"a", "b"}},
		{",,", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SplitAndTrim(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("SplitAndTrim(%q) = %v, want %v", tt.input, result, tt.expected)
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("SplitAndTrim(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestIndexOf(t *testing.T) {
	slice := []string{"apple", "banana", "cherry"}

	t.Run("found", func(t *testing.T) {
		result := indexOf(slice, "banana")
		if result != 1 {
			t.Errorf("expected 1, got %d", result)
		}
	})

	t.Run("not found returns 0", func(t *testing.T) {
		result := indexOf(slice, "grape")
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	t.Run("first element", func(t *testing.T) {
		result := indexOf(slice, "apple")
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})
}

func TestGetServer(t *testing.T) {
	makeCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("server", "", "")
		return cmd
	}

	t.Run("default fallback", func(t *testing.T) {
		os.Unsetenv("ASIKA_SERVER")
		result := getServer(makeCmd())
		if result != "http://localhost:8080" {
			t.Errorf("expected 'http://localhost:8080', got %q", result)
		}
	})

	t.Run("env var set", func(t *testing.T) {
		os.Setenv("ASIKA_SERVER", "http://custom:9090")
		defer os.Unsetenv("ASIKA_SERVER")
		result := getServer(makeCmd())
		if result != "http://custom:9090" {
			t.Errorf("expected 'http://custom:9090', got %q", result)
		}
	})

	t.Run("config file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configDir := filepath.Join(tmpDir, ".config", "asika")
		os.MkdirAll(configDir, 0755)
		configData := `{"server": "http://from-config:7070"}`
		os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configData), 0644)

		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		result := getServer(makeCmd())
		if result != "http://from-config:7070" {
			t.Errorf("expected 'http://from-config:7070', got %q", result)
		}
	})
}

func TestCheckServer(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		err := checkServer(ts.URL)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("failure", func(t *testing.T) {
		err := checkServer("http://127.0.0.1:1")
		if err == nil {
			t.Error("expected error for unreachable server")
		}
	})
}

func TestCheckInitialized(t *testing.T) {
	t.Run("initialized", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "already initialized"})
		}))
		defer ts.Close()

		initialized, err := checkInitialized(ts.URL)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if !initialized {
			t.Error("expected initialized to be true")
		}
	})

	t.Run("not initialized", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
		}))
		defer ts.Close()

		initialized, err := checkInitialized(ts.URL)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if initialized {
			t.Error("expected initialized to be false")
		}
	})

	t.Run("server unreachable", func(t *testing.T) {
		_, err := checkInitialized("http://127.0.0.1:1")
		if err == nil {
			t.Error("expected error for unreachable server")
		}
	})
}

func TestPrintSummary(t *testing.T) {
	t.Run("full config", func(t *testing.T) {
		cfg := map[string]interface{}{
			"mode": "multi",
			"database": map[string]string{
				"type": "bbolt",
				"path": "/var/lib/asika/asika.db",
			},
			"tokens": map[string]interface{}{
				"token": "ghp_abc123",
			},
			"repo_groups": []map[string]interface{}{
				{
					"name":           "default",
					"default_branch": "main",
					"github":         "org/repo",
				},
			},
			"notify": []map[string]interface{}{
				{"type": "telegram"},
			},
			"server": map[string]string{
				"listen": ":8080",
				"mode":   "release",
			},
			"updates": map[string]interface{}{
				"check":    true,
				"interval": "24h",
			},
		}
		PrintSummary(cfg)
	})

	t.Run("minimal config", func(t *testing.T) {
		cfg := map[string]interface{}{
			"mode": "single",
		}
		PrintSummary(cfg)
	})

	t.Run("no platforms configured", func(t *testing.T) {
		cfg := map[string]interface{}{
			"mode":   "multi",
			"tokens": map[string]interface{}{},
		}
		PrintSummary(cfg)
	})

	t.Run("no notifications", func(t *testing.T) {
		cfg := map[string]interface{}{
			"mode":   "multi",
			"notify": []map[string]interface{}{},
		}
		PrintSummary(cfg)
	})

	t.Run("updates disabled", func(t *testing.T) {
		cfg := map[string]interface{}{
			"mode": "multi",
			"updates": map[string]interface{}{
				"check": false,
			},
		}
		PrintSummary(cfg)
	})
}

func TestPrintHeader(t *testing.T) {
	PrintHeader("http://localhost:8080")
}

func TestPrintDisclaimer(t *testing.T) {
	PrintDisclaimer()
}

func TestConfigureMode(t *testing.T) {
	t.Run("default multi", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n"))
		result := ConfigureMode(reader)
		if result != "multi" {
			t.Errorf("expected 'multi', got %q", result)
		}
	})

	t.Run("choose single", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("2\n"))
		result := ConfigureMode(reader)
		if result != "single" {
			t.Errorf("expected 'single', got %q", result)
		}
	})
}

func TestConfigureDatabase(t *testing.T) {
	t.Run("default bbolt", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n\n"))
		result := ConfigureDatabase(reader)
		if result["type"] != "bbolt" {
			t.Errorf("expected type 'bbolt', got %q", result["type"])
		}
		if result["path"] != "/var/lib/asika/asika.db" {
			t.Errorf("expected default bbolt path, got %q", result["path"])
		}
	})

	t.Run("choose mongo", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("2\nmongodb://host:27017\nmydb\n"))
		result := ConfigureDatabase(reader)
		if result["type"] != "mongo" {
			t.Errorf("expected type 'mongo', got %q", result["type"])
		}
		if result["path"] != "mongodb://host:27017" {
			t.Errorf("expected mongo connection string, got %q", result["path"])
		}
		if result["name"] != "mydb" {
			t.Errorf("expected mongo db name 'mydb', got %q", result["name"])
		}
	})
}

func TestConfigureServer(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n\n\n"))
	serverMap, authMap := ConfigureServer(reader)
	if serverMap["listen"] != ":8080" {
		t.Errorf("expected listen ':8080', got %q", serverMap["listen"])
	}
	if serverMap["mode"] != "release" {
		t.Errorf("expected mode 'release', got %q", serverMap["mode"])
	}
	if authMap["token_expiry"] != "72h" {
		t.Errorf("expected token_expiry '72h', got %q", authMap["token_expiry"])
	}
}

func TestConfigureUpdates(t *testing.T) {
	t.Run("enable updates", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("y\n48h\ny\n"))
		result := ConfigureUpdates(reader)
		if result["check"] != true {
			t.Error("expected check true")
		}
		if result["interval"] != "48h" {
			t.Errorf("expected interval '48h', got %v", result["interval"])
		}
		if result["notify_on_new"] != true {
			t.Error("expected notify_on_new true")
		}
	})

	t.Run("disable updates", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("n\n"))
		result := ConfigureUpdates(reader)
		if result["check"] != false {
			t.Error("expected check false")
		}
	})
}

func TestConfigurePlatforms(t *testing.T) {
	t.Run("select github and gitlab", func(t *testing.T) {
		input := "1,2\nghp_github_token\nglpat_gitlab_token\nn\n"
		reader := bufio.NewReader(strings.NewReader(input))
		result := ConfigurePlatforms(reader)
		if result["token"] != "glpat_gitlab_token" {
			t.Errorf("expected gitlab token (last one set), got %v", result["token"])
		}
	})

	t.Run("skip all", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n"))
		result := ConfigurePlatforms(reader)
		if len(result) != 0 {
			t.Errorf("expected empty tokens, got %v", result)
		}
	})

	t.Run("configure gerrit", func(t *testing.T) {
		input := "\ny\nhttps://gerrit.example.com\nadmin\nsecret\n"
		reader := bufio.NewReader(strings.NewReader(input))
		result := ConfigurePlatforms(reader)
		gerrit, ok := result["gerrit"].(map[string]string)
		if !ok {
			t.Fatal("expected gerrit config map")
		}
		if gerrit["url"] != "https://gerrit.example.com" {
			t.Errorf("expected gerrit URL, got %q", gerrit["url"])
		}
		if gerrit["username"] != "admin" {
			t.Errorf("expected gerrit username 'admin', got %q", gerrit["username"])
		}
	})
}

func TestConfigureRepoGroup(t *testing.T) {
	t.Run("with platforms", func(t *testing.T) {
		input := "mygroup\nmain\n1,2\norg/github-repo\norg/gitlab-repo\n"
		reader := bufio.NewReader(strings.NewReader(input))
		result := ConfigureRepoGroup(reader, "multi")
		if len(result) != 1 {
			t.Fatalf("expected 1 group, got %d", len(result))
		}
		g := result[0]
		if g["name"] != "mygroup" {
			t.Errorf("expected name 'mygroup', got %v", g["name"])
		}
		if g["default_branch"] != "main" {
			t.Errorf("expected default_branch 'main', got %v", g["default_branch"])
		}
		if g["github"] != "org/github-repo" {
			t.Errorf("expected github repo, got %v", g["github"])
		}
		if g["gitlab"] != "org/gitlab-repo" {
			t.Errorf("expected gitlab repo, got %v", g["gitlab"])
		}
	})

	t.Run("without platforms", func(t *testing.T) {
		input := "mygroup\ndev\n\n"
		reader := bufio.NewReader(strings.NewReader(input))
		result := ConfigureRepoGroup(reader, "multi")
		if len(result) != 1 {
			t.Fatalf("expected 1 group, got %d", len(result))
		}
		g := result[0]
		if g["github"] != "" {
			t.Errorf("expected empty github, got %v", g["github"])
		}
	})

	t.Run("single mode asks mirror platform", func(t *testing.T) {
		input := "mygroup\nmain\n\n\n"
		reader := bufio.NewReader(strings.NewReader(input))
		result := ConfigureRepoGroup(reader, "single")
		if len(result) != 1 {
			t.Fatalf("expected 1 group, got %d", len(result))
		}
		g := result[0]
		if g["mirror_platform"] != "github" {
			t.Errorf("expected mirror_platform 'github', got %v", g["mirror_platform"])
		}
	})
}

func TestConfigureNotifications(t *testing.T) {
	t.Run("select telegram", func(t *testing.T) {
		input := "1\n12345:ABC\n-100123\nadmin1\n"
		reader := bufio.NewReader(strings.NewReader(input))
		result := ConfigureNotifications(reader)
		if len(result) != 1 {
			t.Fatalf("expected 1 notification channel, got %d", len(result))
		}
		if result[0]["type"] != "telegram" {
			t.Errorf("expected type 'telegram', got %v", result[0]["type"])
		}
	})

	t.Run("skip all", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("\n"))
		result := ConfigureNotifications(reader)
		if len(result) != 0 {
			t.Errorf("expected 0 notification channels, got %d", len(result))
		}
	})

	t.Run("select multiple channels", func(t *testing.T) {
		input := "1,3\n12345:ABC\n-100123\nadmin1\ndiscord-token\nchannel123\nadmin2\n"
		reader := bufio.NewReader(strings.NewReader(input))
		result := ConfigureNotifications(reader)
		if len(result) != 2 {
			t.Fatalf("expected 2 notification channels, got %d", len(result))
		}
		if result[0]["type"] != "telegram" {
			t.Errorf("expected first type 'telegram', got %v", result[0]["type"])
		}
		if result[1]["type"] != "discord" {
			t.Errorf("expected second type 'discord', got %v", result[1]["type"])
		}
	})
}
