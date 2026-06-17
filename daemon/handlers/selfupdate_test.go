package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"asika/common/archive"
)

func TestParseSha256sumFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "valid single entry",
			content: "abc123def456  asikad-linux-amd64\n",
			want:    "abc123def456",
		},
		{
			name:    "valid with extra spaces",
			content: "abc123  file.tar.gz\n",
			want:    "abc123",
		},
		{
			name:    "empty lines skipped",
			content: "\n\nabc123  file\n",
			want:    "abc123",
		},
		{
			name:    "empty file",
			content: "",
			wantErr: true,
		},
		{
			name:    "only whitespace",
			content: "   \n  \n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := writeTempFile(t, tt.content)
			got, err := parseSha256sumFile(tmpFile)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseSha256sumFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVerifyWebChecksum(t *testing.T) {
	t.Run("valid checksum", func(t *testing.T) {
		binaryFile := writeTempFile(t, "hello world")
		data, _ := os.ReadFile(binaryFile)
		hash := sha256.Sum256(data)
		expectedHash := hex.EncodeToString(hash[:])
		checksumContent := expectedHash + "  asikad\n"
		checksumFile := writeTempFile(t, checksumContent)

		err := verifyWebChecksum(binaryFile, checksumFile)
		if err != nil {
			t.Errorf("verifyWebChecksum() error = %v", err)
		}
	})

	t.Run("mismatch checksum", func(t *testing.T) {
		binaryFile := writeTempFile(t, "hello world")
		checksumFile := writeTempFile(t, "0000000000000000000000000000000000000000000000000000000000000000  asikad\n")

		err := verifyWebChecksum(binaryFile, checksumFile)
		if err == nil {
			t.Error("expected error for mismatched checksum")
		}
	})
}

func TestIsValidGitHubDownloadURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://github.com/AsikaProject/asika/releases/download/v1.0/asikad", true},
		{"https://objects.githubusercontent.com/...", true},
		{"https://evil.com/malware", false},
		{"http://github.com/asika/asika/releases/download/v1.0/asikad", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := archive.IsValidGitHubDownloadURL(tt.url)
			if got != tt.want {
				t.Errorf("IsValidGitHubDownloadURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestWebUpdateGuard(t *testing.T) {
	// Verify initial state is "not in progress".
	webUpdateMu.Lock()
	inProgress := webUpdateInProgress
	webUpdateMu.Unlock()
	if inProgress {
		t.Fatal("webUpdateInProgress should be false at start")
	}

	// Simulate claiming the update lock.
	webUpdateMu.Lock()
	if webUpdateInProgress {
		webUpdateMu.Unlock()
		t.Fatal("webUpdateInProgress should be false")
	}
	webUpdateInProgress = true
	webUpdateMu.Unlock()

	// A second claim should observe in-progress == true.
	webUpdateMu.Lock()
	second := webUpdateInProgress
	webUpdateMu.Unlock()
	if !second {
		t.Error("expected webUpdateInProgress to be true while held")
	}

	// Release.
	webUpdateMu.Lock()
	webUpdateInProgress = false
	webUpdateMu.Unlock()
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "test-*")
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		f.WriteString(content)
	}
	f.Close()
	return f.Name()
}
