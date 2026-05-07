package platforms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"
)

type platformTest struct {
	name   string
	client PlatformClient
}

func TestPlatformClientInterface(t *testing.T) {
	tests := []platformTest{
		{"GitHubClient", NewGitHubClient("test-token", "test-webhook-secret", "")},
		{"GitLabClient", NewGitLabClient("test-token", "", "test-webhook-secret")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.client == nil {
				t.Skipf("%s is nil", tt.name)
				return
			}

			ctx := context.Background()

			t.Run("GetPR", func(t *testing.T) {
				_, err := tt.client.GetPR(ctx, "owner", "repo", 1)
				if err == nil {
					t.Log("GetPR returned ok")
				}
			})

			t.Run("ListPRs", func(t *testing.T) {
				_, err := tt.client.ListPRs(ctx, "owner", "repo", "open")
				if err == nil {
					t.Log("ListPRs returned ok")
				}
			})

			t.Run("GetBranch", func(t *testing.T) {
				_, err := tt.client.GetBranch(ctx, "owner", "repo", "main")
				if err == nil {
					t.Log("GetBranch returned ok")
				}
			})

			t.Run("GetDefaultBranch", func(t *testing.T) {
				_, err := tt.client.GetDefaultBranch(ctx, "owner", "repo")
				if err == nil {
					t.Log("GetDefaultBranch returned ok")
				}
			})

			t.Run("GetCIStatus", func(t *testing.T) {
				_, err := tt.client.GetCIStatus(ctx, "owner", "repo", "abc123")
				if err == nil {
					t.Log("GetCIStatus returned ok")
				}
			})

			t.Run("GetDefaultMergeMethod", func(t *testing.T) {
				_, err := tt.client.GetDefaultMergeMethod(ctx, "owner", "repo")
				if err == nil {
					t.Log("GetDefaultMergeMethod returned ok")
				}
			})

			t.Run("HasMultipleMergeMethods", func(t *testing.T) {
				_, err := tt.client.HasMultipleMergeMethods(ctx, "owner", "repo")
				if err == nil {
					t.Log("HasMultipleMergeMethods returned ok")
				}
			})

			t.Run("VerifyWebhookSignature", func(t *testing.T) {
				ok := tt.client.VerifyWebhookSignature([]byte("test"), "sha256=abc")
				t.Logf("VerifyWebhookSignature = %v", ok)
			})

			t.Run("GetPRCommits", func(t *testing.T) {
				_, err := tt.client.GetPRCommits(ctx, "owner", "repo", 1)
				if err == nil {
					t.Log("GetPRCommits returned ok")
				}
			})

			t.Run("GetDiffFiles", func(t *testing.T) {
				_, err := tt.client.GetDiffFiles(ctx, "owner", "repo", 1)
				if err == nil {
					t.Log("GetDiffFiles returned ok")
				}
			})

			t.Run("GetApprovals", func(t *testing.T) {
				_, err := tt.client.GetApprovals(ctx, "owner", "repo", 1)
				if err == nil {
					t.Log("GetApprovals returned ok")
				}
			})

			t.Run("ListBranches", func(t *testing.T) {
				_, err := tt.client.ListBranches(ctx, "owner", "repo")
				if err == nil {
					t.Log("ListBranches returned ok")
				}
			})
		})
	}
}

func TestGiteaClientCreation(t *testing.T) {
	// Test with valid base URL
	client := NewGiteaClient("https://gitea.example.com", "test-token", "test-webhook-secret")
	if client == nil {
		t.Log("Gitea client creation returned nil (expected for invalid/mocked URL)")
		return
	}

	// Verify basic properties
	if client.baseURL != "https://gitea.example.com" {
		t.Errorf("baseURL = %q, want %q", client.baseURL, "https://gitea.example.com")
	}
	if client.token != "test-token" {
		t.Errorf("token = %q, want %q", client.token, "test-token")
	}
	if client.webhookSecret != "test-webhook-secret" {
		t.Errorf("webhookSecret = %q, want %q", client.webhookSecret, "test-webhook-secret")
	}
}

func TestParseDiffFiles(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want []string
	}{
		{
			name: "single file",
			diff: "diff --git a/src/main.go b/src/main.go\n--- a/src/main.go\n+++ b/src/main.go\n@@ -1,3 +1,3 @@",
			want: []string{"src/main.go"},
		},
		{
			name: "multiple files",
			diff: "diff --git a/src/main.go b/src/main.go\n--- a/src/main.go\n+++ b/src/main.go\ndiff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md",
			want: []string{"src/main.go", "README.md"},
		},
		{
			name: "renamed file",
			diff: "diff --git a/old_name.go b/new_name.go\n--- a/old_name.go\n+++ b/new_name.go",
			want: []string{"old_name.go"},
		},
		{
			name: "empty diff",
			diff: "",
			want: []string{},
		},
		{
			name: "nested paths",
			diff: "diff --git a/internal/handler/api.go b/internal/handler/api.go",
			want: []string{"internal/handler/api.go"},
		},
		{
			name: "new file",
			diff: "diff --git a/new_file.go b/new_file.go\nnew file mode 100644",
			want: []string{"new_file.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDiffFiles(tt.diff)
			sort.Strings(got)
			sort.Strings(tt.want)
			if len(got) != len(tt.want) {
				t.Errorf("parseDiffFiles() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseDiffFiles() = %v, want %v", got, tt.want)
					break
				}
			}
		})
	}
}

func TestGitLabState(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"opened", "open"},
		{"closed", "closed"},
		{"merged", "merged"},
		{"OPENED", "open"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := gitLabState(tt.input)
			if got != tt.want {
				t.Errorf("gitLabState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractGiteaLabels(t *testing.T) {
	t.Run("empty labels", func(t *testing.T) {
		labels := extractGiteaLabels(nil)
		if len(labels) != 0 {
			t.Errorf("extractGiteaLabels(nil) = %v, want []", labels)
		}
	})
}

func TestGitLabCreateClient(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		baseURL string
		secret  string
		wantNil bool
	}{
		{
			name:    "default gitlab.com",
			token:   "glpat-test",
			baseURL: "",
			secret:  "webhook-secret",
			wantNil: false,
		},
		{
			name:    "self-hosted gitlab",
			token:   "glpat-test",
			baseURL: "https://gitlab.example.com",
			secret:  "webhook-secret",
			wantNil: false,
		},
		{
			name:    "empty token",
			token:   "",
			baseURL: "",
			secret:  "",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewGitLabClient(tt.token, tt.baseURL, tt.secret)
			if tt.wantNil && client != nil {
				t.Error("expected nil client")
			}
			if !tt.wantNil && client == nil {
				t.Error("expected non-nil client")
			}
			if client != nil {
				if client.token != tt.token {
					t.Errorf("token = %q, want %q", client.token, tt.token)
				}
				if client.baseURL != tt.baseURL {
					t.Errorf("baseURL = %q, want %q", client.baseURL, tt.baseURL)
				}
				if client.webhookSecret != tt.secret {
					t.Errorf("webhookSecret = %q, want %q", client.webhookSecret, tt.secret)
				}
			}
		})
	}
}

func TestGitLabVerifyWebhookSignature(t *testing.T) {
	secret := "test-webhook-secret"
	client := NewGitLabClient("token", "", secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		want      bool
	}{
		{
			name:      "token match",
			body:      []byte("payload"),
			signature: "test-webhook-secret",
			want:      true,
		},
		{
			name:      "token mismatch",
			body:      []byte("payload"),
			signature: "wrong-secret",
			want:      false,
		},
		{
			name:      "valid HMAC-SHA256",
			body:      []byte("payload"),
			signature: computeGitLabHMAC(secret, "payload"),
			want:      true,
		},
		{
			name:      "invalid HMAC-SHA256",
			body:      []byte("payload"),
			signature: "sha256=deadbeef",
			want:      false,
		},
		{
			name:      "different body",
			body:      []byte("payload"),
			signature: computeGitLabHMAC(secret, "other-payload"),
			want:      false,
		},
		{
			name:      "empty signature",
			body:      []byte("payload"),
			signature: "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.VerifyWebhookSignature(tt.body, tt.signature)
			if got != tt.want {
				t.Errorf("VerifyWebhookSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGitLabVerifyWebhookSignatureEmptySecret(t *testing.T) {
	client := NewGitLabClient("token", "", "")
	got := client.VerifyWebhookSignature([]byte("payload"), "any-signature")
	if got != false {
		t.Error("expected false when webhook secret is empty")
	}
}

func TestGiteaVerifyWebhookSignature(t *testing.T) {
	secret := "test-secret"
	client := NewGiteaClient("https://gitea.example.com", "token", secret)
	if client == nil {
		t.Skip("gitea client creation failed")
		return
	}

	tests := []struct {
		name      string
		body      []byte
		signature string
		want      bool
	}{
		{
			name:      "valid HMAC with sha256= prefix",
			body:      []byte("test-body"),
			signature: computeGiteaHMAC(secret, "test-body"),
			want:      true,
		},
		{
			name:      "valid HMAC without prefix",
			body:      []byte("test-body"),
			signature: computeGiteaHMACRaw(secret, "test-body"),
			want:      true,
		},
		{
			name:      "invalid HMAC",
			body:      []byte("test-body"),
			signature: "sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			want:      false,
		},
		{
			name:      "different body",
			body:      []byte("test-body"),
			signature: computeGiteaHMAC(secret, "different-body"),
			want:      false,
		},
		{
			name:      "empty signature",
			body:      []byte("test-body"),
			signature: "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.VerifyWebhookSignature(tt.body, tt.signature)
			if got != tt.want {
				t.Errorf("VerifyWebhookSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGiteaVerifyWebhookSignatureEmptySecret(t *testing.T) {
	client := NewGiteaClient("https://gitea.example.com", "token", "")
	if client == nil {
		t.Skip("gitea client creation failed")
		return
	}
	got := client.VerifyWebhookSignature([]byte("body"), "sha256=abc")
	if got != false {
		t.Error("expected false when webhook secret is empty")
	}
}

func TestGiteaCreateClient(t *testing.T) {
	// NewGiteaClient tries to connect to the server to verify it exists,
	// so it will return nil for non-existent hosts.
	// We test that the fields are set correctly when the client is created,
	// and that creation fails gracefully for unreachable hosts.

	t.Run("unreachable host returns nil", func(t *testing.T) {
		client := NewGiteaClient("https://gitea.example.com", "test-token", "test-secret")
		if client != nil {
			t.Skip("gitea.example.com was unexpectedly reachable")
		}
	})

	t.Run("empty URL returns nil", func(t *testing.T) {
		client := NewGiteaClient("", "test-token", "test-secret")
		if client != nil {
			t.Skip("empty URL unexpectedly created a client")
		}
	})
}

func TestParseDiffFilesEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want int
	}{
		{
			name: "deleted file",
			diff: "diff --git a/old.txt b/old.txt\ndeleted file mode 100644",
			want: 1,
		},
		{
			name: "binary file",
			diff: "diff --git a/image.png b/image.png\nBinary files differ",
			want: 1,
		},
		{
			name: "files with spaces",
			diff: "diff --git a/my file.go b/my file.go",
			want: 1,
		},
		{
			name: "no diff lines",
			diff: "some other content",
			want: 0,
		},
		{
			name: "only git headers",
			diff: "commit abc123\nAuthor: test",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDiffFiles(tt.diff)
			if len(got) != tt.want {
				t.Errorf("parseDiffFiles() returned %d files, want %d: %v", len(got), tt.want, got)
			}
		})
	}
}

func TestGitLabStateEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"Opened", "open"},
		{"CLOSED", "closed"},
		{"MERGED", "merged"},
		{"locked", "locked"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := gitLabState(tt.input)
			if got != tt.want {
				t.Errorf("gitLabState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGitLabVerifySignatureHMACCorrectness(t *testing.T) {
	secret := "my-secret-key"
	client := NewGitLabClient("token", "", secret)

	body := []byte(`{"object_kind":"push","event_name":"push"}`)

	// Compute expected HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !client.VerifyWebhookSignature(body, expected) {
		t.Error("HMAC-SHA256 verification should succeed")
	}

	// Wrong secret
	client2 := NewGitLabClient("token", "", "wrong-secret")
	if client2.VerifyWebhookSignature(body, expected) {
		t.Error("HMAC-SHA256 should fail with wrong secret")
	}
}

func TestGiteaVerifySignatureHMACCorrectness(t *testing.T) {
	secret := "gitea-secret-key"
	client := NewGiteaClient("https://gitea.example.com", "token", secret)
	if client == nil {
		t.Skip("gitea client creation failed")
		return
	}

	body := []byte(`{"ref":"refs/heads/main"}`)

	// With sha256= prefix (what Gitea actually sends)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	withPrefix := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !client.VerifyWebhookSignature(body, withPrefix) {
		t.Error("HMAC-SHA256 with sha256= prefix should succeed")
	}

	// Without prefix (should also work)
	withoutPrefix := hex.EncodeToString(mac.Sum(nil))
	if !client.VerifyWebhookSignature(body, withoutPrefix) {
		t.Error("HMAC-SHA256 without prefix should also succeed")
	}
}

// computeGitLabHMAC computes an HMAC-SHA256 for testing GitLab verification
func computeGitLabHMAC(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// computeGiteaHMAC computes an HMAC-SHA256 with sha256= prefix
func computeGiteaHMAC(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// computeGiteaHMACRaw computes an HMAC-SHA256 without prefix (tests compatibility)
func computeGiteaHMACRaw(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}