package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupToFile(t *testing.T) {
	initTestDB(t)

	Put(BucketConfig, "key1", []byte("value1"))
	Put(BucketConfig, "key2", []byte("value2"))

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	err := BackupToFile(backupPath)
	if err != nil {
		t.Fatalf("BackupToFile failed: %v", err)
	}

	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("backup file is empty")
	}

	backupStore, err := NewBboltStorage(backupPath)
	if err != nil {
		t.Fatalf("failed to open backup: %v", err)
	}
	defer backupStore.Close()

	val, err := backupStore.Get(BucketConfig, "key1")
	if err != nil {
		t.Fatalf("Get from backup failed: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("backup data mismatch: got %q, want %q", string(val), "value1")
	}
}
