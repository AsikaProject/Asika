package db

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateBboltToMongo(t *testing.T) {
	if os.Getenv("MONGO_TEST_URI") == "" {
		t.Skip("MONGO_TEST_URI not set, skipping MongoDB migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create temp bbolt database
	tmpDir := t.TempDir()
	bboltPath := filepath.Join(tmpDir, "test.db")

	bboltStore, err := newBboltStorage(bboltPath)
	if err != nil {
		t.Fatalf("failed to create bbolt storage: %v", err)
	}

	// Insert test data into bbolt
	testData := map[string]map[string]string{
		BucketUsers: {
			"alice": `{"username":"alice","role":"admin","password_hash":"hash1"}`,
			"bob":   `{"username":"bob","role":"operator","password_hash":"hash2"}`,
		},
		BucketConfig: {
			"server": `{"listen":":8080","mode":"release"}`,
		},
		BucketPasswordResetTokens: {
			"token1": `{"token_hash":"hash1","username":"alice","expires_at":"2026-06-01T00:00:00Z"}`,
		},
	}

	for bucket, entries := range testData {
		for key, value := range entries {
			if err := bboltStore.Put(bucket, key, []byte(value)); err != nil {
				t.Fatalf("failed to insert test data into bbolt: %v", err)
			}
		}
	}
	bboltStore.Close()

	// Migrate to MongoDB
	mongoURI := os.Getenv("MONGO_TEST_URI")
	dbName := "asika_test_migrate_" + time.Now().Format("20060102150405")

	err = MigrateBboltToMongo(ctx, bboltPath, mongoURI, dbName)
	if err != nil {
		t.Fatalf("failed to migrate bbolt to mongo: %v", err)
	}

	// Connect to MongoDB and verify data
	mongoStore, err := NewMongoStorage(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("failed to connect to MongoDB: %v", err)
	}
	defer func() {
		if ms, ok := mongoStore.(*mongoStorage); ok {
			if dropErr := ms.db.Drop(ctx); dropErr != nil {
				t.Logf("warning: failed to drop test database %s: %v", dbName, dropErr)
			}
		}
		mongoStore.Close()
	}()

	// Verify users bucket
	for key, expectedJSON := range testData[BucketUsers] {
		data, err := mongoStore.Get(BucketUsers, key)
		if err != nil {
			t.Errorf("failed to get user %s from mongo: %v", key, err)
			continue
		}
		if data == nil {
			t.Errorf("user %s not found in mongo", key)
			continue
		}

		var expected, actual map[string]interface{}
		if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
			t.Fatalf("failed to unmarshal expected JSON for user %s: %v", key, err)
		}
		if err := json.Unmarshal(data, &actual); err != nil {
			t.Fatalf("failed to unmarshal actual JSON for user %s: %v", key, err)
		}

		// Compare key fields
		if actual["username"] != expected["username"] {
			t.Errorf("user %s: username mismatch: got %v, want %v", key, actual["username"], expected["username"])
		}
		if actual["role"] != expected["role"] {
			t.Errorf("user %s: role mismatch: got %v, want %v", key, actual["role"], expected["role"])
		}
		if actual["password_hash"] != expected["password_hash"] {
			t.Errorf("user %s: password_hash mismatch: got %v, want %v", key, actual["password_hash"], expected["password_hash"])
		}
	}

	// Verify config bucket
	data, err := mongoStore.Get(BucketConfig, "server")
	if err != nil {
		t.Fatalf("failed to get config from mongo: %v", err)
	}
	if data == nil {
		t.Fatal("config not found in mongo")
	}

	var configData map[string]interface{}
	if err := json.Unmarshal(data, &configData); err != nil {
		t.Fatalf("failed to unmarshal config data: %v", err)
	}
	if configData["listen"] != ":8080" {
		t.Errorf("config listen mismatch: got %v, want :8080", configData["listen"])
	}

	// Verify password reset tokens bucket
	data, err = mongoStore.Get(BucketPasswordResetTokens, "token1")
	if err != nil {
		t.Fatalf("failed to get password reset token from mongo: %v", err)
	}
	if data == nil {
		t.Fatal("password reset token not found in mongo")
	}

	var tokenData map[string]interface{}
	if err := json.Unmarshal(data, &tokenData); err != nil {
		t.Fatalf("failed to unmarshal token data: %v", err)
	}
	if tokenData["username"] != "alice" {
		t.Errorf("token username mismatch: got %v, want alice", tokenData["username"])
	}
}

func TestMigrateMongoToBbolt(t *testing.T) {
	if os.Getenv("MONGO_TEST_URI") == "" {
		t.Skip("MONGO_TEST_URI not set, skipping MongoDB migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGO_TEST_URI")
	dbName := "asika_test_migrate_back_" + time.Now().Format("20060102150405")

	// Create MongoDB storage and insert test data
	mongoStore, err := NewMongoStorage(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("failed to create MongoDB storage: %v", err)
	}

	testData := map[string]map[string]string{
		BucketUsers: {
			"alice": `{"username":"alice","role":"admin","password_hash":"hash1"}`,
			"bob":   `{"username":"bob","role":"operator","password_hash":"hash2"}`,
		},
		BucketPasswordResetTokens: {
			"token1": `{"token_hash":"hash1","username":"alice","expires_at":"2026-06-01T00:00:00Z"}`,
		},
	}

	for bucket, entries := range testData {
		for key, value := range entries {
			if err := mongoStore.Put(bucket, key, []byte(value)); err != nil {
				t.Fatalf("failed to insert test data into MongoDB: %v", err)
			}
		}
	}
	mongoStore.Close()

	// Migrate to bbolt
	tmpDir := t.TempDir()
	bboltPath := filepath.Join(tmpDir, "test.db")

	err = MigrateMongoToBbolt(ctx, mongoURI, dbName, bboltPath)
	if err != nil {
		t.Fatalf("failed to migrate mongo to bbolt: %v", err)
	}

	// Clean up MongoDB
	mongoStore2, err := NewMongoStorage(ctx, mongoURI, dbName)
	if err != nil {
		t.Logf("warning: failed to reconnect to MongoDB for cleanup: %v", err)
	} else {
		if ms, ok := mongoStore2.(*mongoStorage); ok {
			if dropErr := ms.db.Drop(ctx); dropErr != nil {
				t.Logf("warning: failed to drop test database %s: %v", dbName, dropErr)
			}
		}
		mongoStore2.Close()
	}

	// Open bbolt and verify data
	bboltStore, err := newBboltStorage(bboltPath)
	if err != nil {
		t.Fatalf("failed to open bbolt: %v", err)
	}
	defer bboltStore.Close()

	// Verify users bucket
	for key, expectedJSON := range testData[BucketUsers] {
		data, err := bboltStore.Get(BucketUsers, key)
		if err != nil {
			t.Errorf("failed to get user %s from bbolt: %v", key, err)
			continue
		}
		if data == nil {
			t.Errorf("user %s not found in bbolt", key)
			continue
		}

		var expected, actual map[string]interface{}
		if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
			t.Fatalf("failed to unmarshal expected JSON for user %s: %v", key, err)
		}
		if err := json.Unmarshal(data, &actual); err != nil {
			t.Fatalf("failed to unmarshal actual JSON for user %s: %v", key, err)
		}

		if actual["username"] != expected["username"] {
			t.Errorf("user %s: username mismatch: got %v, want %v", key, actual["username"], expected["username"])
		}
		if actual["role"] != expected["role"] {
			t.Errorf("user %s: role mismatch: got %v, want %v", key, actual["role"], expected["role"])
		}
		if actual["password_hash"] != expected["password_hash"] {
			t.Errorf("user %s: password_hash mismatch: got %v, want %v", key, actual["password_hash"], expected["password_hash"])
		}
	}

	// Verify password reset tokens bucket
	data, err := bboltStore.Get(BucketPasswordResetTokens, "token1")
	if err != nil {
		t.Fatalf("failed to get password reset token from bbolt: %v", err)
	}
	if data == nil {
		t.Fatal("password reset token not found in bbolt")
	}

	var tokenData map[string]interface{}
	if err := json.Unmarshal(data, &tokenData); err != nil {
		t.Fatalf("failed to unmarshal token data: %v", err)
	}
	if tokenData["username"] != "alice" {
		t.Errorf("token username mismatch: got %v, want alice", tokenData["username"])
	}
}

func TestMigrateDataConsistency(t *testing.T) {
	if os.Getenv("MONGO_TEST_URI") == "" {
		t.Skip("MONGO_TEST_URI not set, skipping MongoDB migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create temp bbolt database
	tmpDir := t.TempDir()
	bboltPath := filepath.Join(tmpDir, "test.db")

	bboltStore, err := newBboltStorage(bboltPath)
	if err != nil {
		t.Fatalf("failed to create bbolt storage: %v", err)
	}

	// Insert test data with nested JSON
	complexData := `{
		"username": "alice",
		"role": "admin",
		"permissions": {
			"can_approve": true,
			"can_merge": false
		},
		"allowed_repo_groups": ["frontend", "backend"],
		"created_at": "2026-01-01T00:00:00Z"
	}`
	if err := bboltStore.Put(BucketUsers, "alice", []byte(complexData)); err != nil {
		t.Fatalf("failed to insert complex data: %v", err)
	}
	bboltStore.Close()

	// Migrate to MongoDB
	mongoURI := os.Getenv("MONGO_TEST_URI")
	dbName := "asika_test_consistency_" + time.Now().Format("20060102150405")

	err = MigrateBboltToMongo(ctx, bboltPath, mongoURI, dbName)
	if err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Read from MongoDB
	mongoStore, err := NewMongoStorage(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("failed to connect to MongoDB: %v", err)
	}
	defer func() {
		if ms, ok := mongoStore.(*mongoStorage); ok {
			if dropErr := ms.db.Drop(ctx); dropErr != nil {
				t.Logf("warning: failed to drop test database %s: %v", dbName, dropErr)
			}
		}
		mongoStore.Close()
	}()

	mongoData, err := mongoStore.Get(BucketUsers, "alice")
	if err != nil {
		t.Fatalf("failed to get data from MongoDB: %v", err)
	}

	// Verify data is identical
	var original, migrated map[string]interface{}
	if err := json.Unmarshal([]byte(complexData), &original); err != nil {
		t.Fatalf("failed to unmarshal original JSON: %v", err)
	}
	if err := json.Unmarshal(mongoData, &migrated); err != nil {
		t.Fatalf("failed to unmarshal migrated JSON: %v", err)
	}

	if migrated["username"] != original["username"] {
		t.Errorf("username mismatch: got %v, want %v", migrated["username"], original["username"])
	}

	// Verify nested object
	origPerms, ok := original["permissions"].(map[string]interface{})
	if !ok {
		t.Fatal("original permissions is not a map")
	}
	migPerms, ok := migrated["permissions"].(map[string]interface{})
	if !ok {
		t.Fatal("migrated permissions is not a map")
	}
	if migPerms["can_approve"] != origPerms["can_approve"] {
		t.Errorf("permissions.can_approve mismatch: got %v, want %v", migPerms["can_approve"], origPerms["can_approve"])
	}

	// Verify array
	origGroups, ok := original["allowed_repo_groups"].([]interface{})
	if !ok {
		t.Fatal("original allowed_repo_groups is not a slice")
	}
	migGroups, ok := migrated["allowed_repo_groups"].([]interface{})
	if !ok {
		t.Fatal("migrated allowed_repo_groups is not a slice")
	}
	if len(migGroups) != len(origGroups) {
		t.Errorf("allowed_repo_groups length mismatch: got %d, want %d", len(migGroups), len(origGroups))
	} else {
		for i := range origGroups {
			if migGroups[i] != origGroups[i] {
				t.Errorf("allowed_repo_groups[%d] mismatch: got %v, want %v", i, migGroups[i], origGroups[i])
			}
		}
	}

	// Migrate back to bbolt
	bboltPath2 := filepath.Join(tmpDir, "test2.db")
	err = MigrateMongoToBbolt(ctx, mongoURI, dbName, bboltPath2)
	if err != nil {
		t.Fatalf("failed to migrate back: %v", err)
	}

	// Read from bbolt
	bboltStore2, err := newBboltStorage(bboltPath2)
	if err != nil {
		t.Fatalf("failed to open bbolt: %v", err)
	}
	defer bboltStore2.Close()

	bboltData, err := bboltStore2.Get(BucketUsers, "alice")
	if err != nil {
		t.Fatalf("failed to get data from bbolt: %v", err)
	}

	// Verify round-trip consistency
	var roundTrip map[string]interface{}
	if err := json.Unmarshal(bboltData, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal round-trip JSON: %v", err)
	}

	if roundTrip["username"] != original["username"] {
		t.Errorf("round-trip username mismatch: got %v, want %v", roundTrip["username"], original["username"])
	}
}

func TestMigrateBboltToMongo_InvalidBboltPath(t *testing.T) {
	ctx := context.Background()
	err := MigrateBboltToMongo(ctx, "/nonexistent/path/db.db", "mongodb://localhost:27017", "test")
	if err == nil {
		t.Fatal("expected error for invalid bbolt path")
	}
}

func TestMigrateBboltToMongo_InvalidMongoURI(t *testing.T) {
	tmpDir := t.TempDir()
	bboltPath := filepath.Join(tmpDir, "test.db")
	store, err := newBboltStorage(bboltPath)
	if err != nil {
		t.Fatalf("failed to create bbolt storage: %v", err)
	}
	store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = MigrateBboltToMongo(ctx, bboltPath, "mongodb://invalid:99999", "test")
	if err == nil {
		t.Fatal("expected error for invalid mongo URI")
	}
}

func TestMigrateMongoToBbolt_InvalidInputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := MigrateMongoToBbolt(ctx, "mongodb://invalid:99999", "test", "/tmp/test.db")
	if err == nil {
		t.Fatal("expected error for invalid mongo URI")
	}
}

func TestMigrateBboltToMongo_EmptyDatabase(t *testing.T) {
	if os.Getenv("MONGO_TEST_URI") == "" {
		t.Skip("MONGO_TEST_URI not set, skipping MongoDB migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	bboltPath := filepath.Join(tmpDir, "empty.db")
	store, err := newBboltStorage(bboltPath)
	if err != nil {
		t.Fatalf("failed to create bbolt storage: %v", err)
	}
	store.Close()

	mongoURI := os.Getenv("MONGO_TEST_URI")
	dbName := "asika_test_empty_" + time.Now().Format("20060102150405")

	err = MigrateBboltToMongo(ctx, bboltPath, mongoURI, dbName)
	if err != nil {
		t.Fatalf("empty migration should succeed: %v", err)
	}

	// Clean up
	mongoStore, err := NewMongoStorage(ctx, mongoURI, dbName)
	if err != nil {
		t.Logf("warning: failed to connect for cleanup: %v", err)
		return
	}
	if ms, ok := mongoStore.(*mongoStorage); ok {
		ms.db.Drop(ctx)
	}
	mongoStore.Close()
}
