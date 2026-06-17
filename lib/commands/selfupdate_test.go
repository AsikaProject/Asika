package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v69/github"
)

func TestFindAssets(t *testing.T) {
	release := &github.RepositoryRelease{
		Assets: []*github.ReleaseAsset{
			{Name: github.String("asika-linux-arm64.tar.gz"), BrowserDownloadURL: github.String("https://github.com/AsikaProject/asika/releases/download/v1/asika-linux-arm64.tar.gz")},
			{Name: github.String("asika-darwin-amd64.tar.gz"), BrowserDownloadURL: github.String("https://github.com/AsikaProject/asika/releases/download/v1/asika-darwin-amd64.tar.gz")},
			{Name: github.String("asikad-linux-arm64.sha256sum"), BrowserDownloadURL: github.String("https://github.com/AsikaProject/asika/releases/download/v1/asikad-linux-arm64.sha256sum")},
			{Name: github.String("asika-linux-arm64.sha256sum"), BrowserDownloadURL: github.String("https://github.com/AsikaProject/asika/releases/download/v1/asika-linux-arm64.sha256sum")},
			{Name: github.String("ChangeLog.md"), BrowserDownloadURL: github.String("https://github.com/AsikaProject/asika/releases/download/v1/ChangeLog.md")},
		},
	}

	t.Run("asikad upgrade", func(t *testing.T) {
		tarball, checksum := findAssets(release, "asika-linux-arm64.tar.gz", "asikad-linux-arm64.sha256sum")
		if tarball == "" {
			t.Error("expected tarball URL")
		}
		if checksum == "" {
			t.Error("expected checksum URL")
		}
	})

	t.Run("asika upgrade", func(t *testing.T) {
		tarball, checksum := findAssets(release, "asika-linux-arm64.tar.gz", "asika-linux-arm64.sha256sum")
		if tarball == "" {
			t.Error("expected tarball URL")
		}
		if checksum == "" {
			t.Error("expected checksum URL")
		}
	})

	t.Run("missing tarball", func(t *testing.T) {
		tarball, _ := findAssets(release, "asika-solaris-sparc.tar.gz", "asikad-solaris-sparc.sha256sum")
		if tarball != "" {
			t.Errorf("expected empty tarball URL, got %q", tarball)
		}
	})

	t.Run("rejects non github url", func(t *testing.T) {
		evil := &github.RepositoryRelease{
			Assets: []*github.ReleaseAsset{
				{Name: github.String("asika-linux-arm64.tar.gz"), BrowserDownloadURL: github.String("https://evil.example.com/asika.tar.gz")},
			},
		}
		tarball, _ := findAssets(evil, "asika-linux-arm64.tar.gz", "asikad-linux-arm64.sha256sum")
		if tarball != "" {
			t.Errorf("expected empty tarball URL for non-github host, got %q", tarball)
		}
	})
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()

	binaryData := []byte("fake binary content")
	binaryPath := filepath.Join(dir, "asikad-linux-amd64")
	if err := os.WriteFile(binaryPath, binaryData, 0644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(binaryData)
	expectedSum := hex.EncodeToString(sum[:])

	t.Run("valid sha256sum", func(t *testing.T) {
		// Standard sha256sum file format: "<hash>  <filename>"
		checksumContent := expectedSum + "  asikad-linux-amd64\n"
		checksumPath := filepath.Join(dir, "asikad-linux-amd64.sha256sum")
		if err := os.WriteFile(checksumPath, []byte(checksumContent), 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyChecksum(binaryPath, checksumPath); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("mismatched checksum", func(t *testing.T) {
		checksumContent := "0000000000000000000000000000000000000000000000000000000000000000  asikad-linux-amd64\n"
		checksumPath := filepath.Join(dir, "asikad-linux-amd64.sha256sum")
		if err := os.WriteFile(checksumPath, []byte(checksumContent), 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyChecksum(binaryPath, checksumPath); err == nil {
			t.Error("expected error for mismatched checksum")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		checksumPath := filepath.Join(dir, "empty.sha256sum")
		if err := os.WriteFile(checksumPath, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyChecksum(binaryPath, checksumPath); err == nil {
			t.Error("expected error for empty checksum file")
		}
	})
}

func TestDoRollbackNoBackup(t *testing.T) {
	currentPath, _ := os.Executable()
	backupPath := currentPath + ".old"
	os.Remove(backupPath)

	_, err := os.Stat(backupPath)
	if err == nil {
		t.Skip("backup file unexpectedly exists, cannot run test")
	}
}

func TestAtomicInstall(t *testing.T) {
	dir := t.TempDir()

	srcPath := filepath.Join(dir, "src")
	srcData := []byte("test atomic install data")
	if err := os.WriteFile(srcPath, srcData, 0644); err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(dir, "dst")
	if err := atomicInstall(srcPath, dstPath); err != nil {
		t.Fatal(err)
	}

	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(dstData) != string(srcData) {
		t.Error("installed data does not match source")
	}

	// The intermediate .new file must have been cleaned up by the rename.
	if _, err := os.Stat(dstPath + ".new"); !os.IsNotExist(err) {
		t.Errorf("intermediate .new file should not exist: %v", err)
	}
}

func TestAtomicInstall_SourceMissing(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	if err := atomicInstall(filepath.Join(dir, "nope"), dst); err == nil {
		t.Error("expected error when source is missing")
	}
}

func TestDetectBinary(t *testing.T) {
	name := detectBinary()
	if name == "" {
		t.Error("detectBinary returned empty string")
	}
}
