package db

import (
	"encoding/json"
	"fmt"
	"testing"

	"asika/common/models"
	"go.etcd.io/bbolt"
)

func TestRunMigrations_FreshDB(t *testing.T) {
	dir := t.TempDir()
	err := Init(dir + "/test_migrations.db")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	err = RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	s, ok := defaultStorage.(*bboltStorage)
	if !ok {
		t.Skip("not bbolt storage")
	}

	var version int
	err = s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketConfig))
		if b == nil {
			return fmt.Errorf("config bucket not found")
		}
		val := b.Get([]byte(migrationVersionKey))
		if val == nil {
			return fmt.Errorf("migration version key not found")
		}
		_, err := fmt.Sscanf(string(val), "%d", &version)
		return err
	})
	if err != nil {
		t.Fatalf("failed to read migration version: %v", err)
	}

	if version != len(migrationRegistry) {
		t.Errorf("migration version = %d, want %d", version, len(migrationRegistry))
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	dir := t.TempDir()
	err := Init(dir + "/test_migrations_idem.db")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	err = RunMigrations()
	if err != nil {
		t.Fatalf("first RunMigrations failed: %v", err)
	}

	err = RunMigrations()
	if err != nil {
		t.Fatalf("second RunMigrations failed: %v", err)
	}

	s, ok := defaultStorage.(*bboltStorage)
	if !ok {
		t.Skip("not bbolt storage")
	}

	var version int
	err = s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketConfig))
		val := b.Get([]byte(migrationVersionKey))
		_, err := fmt.Sscanf(string(val), "%d", &version)
		return err
	})
	if err != nil {
		t.Fatalf("failed to read migration version: %v", err)
	}

	if version != len(migrationRegistry) {
		t.Errorf("migration version = %d, want %d", version, len(migrationRegistry))
	}
}

func TestMigrationV2_SpamFlagDefaults(t *testing.T) {
	dir := t.TempDir()
	err := Init(dir + "/test_spam_flag.db")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	err = RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	s, ok := defaultStorage.(*bboltStorage)
	if !ok {
		t.Skip("not bbolt storage")
	}

	pr := models.PRRecord{
		ID:        "spam-pr",
		RepoGroup: "test",
		Platform:  "github",
		PRNumber:  1,
		Title:     "spam",
		Author:    "spammer",
		State:     "spam",
		SpamFlag:  false,
	}
	data, _ := json.Marshal(pr)
	err = Put(BucketPRs, "test#1", data)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	err = s.db.Update(func(tx *bbolt.Tx) error {
		return setVersion(tx, 1)
	})
	if err != nil {
		t.Fatalf("setVersion failed: %v", err)
	}

	err = RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	stored, err := Get(BucketPRs, "test#1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	var storedPR models.PRRecord
	if err := json.Unmarshal(stored, &storedPR); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !storedPR.SpamFlag {
		t.Error("SpamFlag should be true for state=spam PR after migration v2")
	}
}

func TestMigrationV2_NonSpamUnchanged(t *testing.T) {
	dir := t.TempDir()
	err := Init(dir + "/test_nospam.db")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	err = RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	pr := models.PRRecord{
		ID:        "normal-pr",
		RepoGroup: "test",
		Platform:  "github",
		PRNumber:  2,
		Title:     "normal PR",
		Author:    "dev",
		State:     "open",
		SpamFlag:  false,
	}
	data, _ := json.Marshal(pr)
	err = Put(BucketPRs, "test#2", data)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	err = RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	stored, err := Get(BucketPRs, "test#2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	var storedPR models.PRRecord
	if err := json.Unmarshal(stored, &storedPR); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if storedPR.SpamFlag {
		t.Error("SpamFlag should remain false for non-spam PR")
	}
}

func TestGetCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	err := Init(dir + "/test_version.db")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	s, ok := defaultStorage.(*bboltStorage)
	if !ok {
		t.Skip("not bbolt storage")
	}

	var version int
	err = s.db.View(func(tx *bbolt.Tx) error {
		version = getCurrentVersion(tx)
		return nil
	})
	if err != nil {
		t.Fatalf("View failed: %v", err)
	}
	if version != 0 {
		t.Errorf("initial version = %d, want 0", version)
	}

	err = RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	err = s.db.View(func(tx *bbolt.Tx) error {
		version = getCurrentVersion(tx)
		return nil
	})
	if err != nil {
		t.Fatalf("View failed: %v", err)
	}
	if version != len(migrationRegistry) {
		t.Errorf("version after migration = %d, want %d", version, len(migrationRegistry))
	}
}
