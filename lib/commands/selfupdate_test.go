package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewer(t *testing.T) {
	if !isNewer("2.0.0", "1.0.0") {
		t.Error("2.0.0 should be newer than 1.0.0")
	}
	if isNewer("1.0.0", "1.0.0") {
		t.Error("1.0.0 should not be newer than 1.0.0")
	}
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

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()

	srcPath := filepath.Join(dir, "src")
	srcData := []byte("test copy data")
	if err := os.WriteFile(srcPath, srcData, 0644); err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(dir, "dst")
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatal(err)
	}

	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(dstData) != string(srcData) {
		t.Error("copied data does not match source")
	}
}

func TestDetectBinary(t *testing.T) {
	name := detectBinary()
	if name == "" {
		t.Error("detectBinary returned empty string")
	}
}
