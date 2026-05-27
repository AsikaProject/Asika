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
		mongoStore.Close()
		// Clean up test database
		mongoStore.(*mongoStorage).db.Drop(ctx)
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
		json.Unmarshal([]byte(expectedJSON), &expected)
		json.Unmarshal(data, &actual)

		// Compare key fields
		if actual["username"] != expected["username"] {
			t.Errorf("user %s: username mismatch: got %v, want %v", key, actual["username"], expected["username"])
		}
		if actual["role"] != expected["role"] {
			t.Errorf("user %s: role mismatch: got %v, want %v", key, actual["role"], expected["role"])
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
	json.Unmarshal(data, &configData)
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
	json.Unmarshal(data, &tokenData)
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
	mongoStore2, _ := NewMongoStorage(ctx, mongoURI, dbName)
	mongoStore2.(*mongoStorage).db.Drop(ctx)
	mongoStore2.Close()

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
		json.Unmarshal([]byte(expectedJSON), &expected)
		json.Unmarshal(data, &actual)

		if actual["username"] != expected["username"] {
			t.Errorf("user %s: username mismatch: got %v, want %v", key, actual["username"], expected["username"])
		}
		if actual["role"] != expected["role"] {
			t.Errorf("user %s: role mismatch: got %v, want %v", key, actual["role"], expected["role"])
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
	json.Unmarshal(data, &tokenData)
	if tokenData["username"] != "alice" {
		t.Errorf("token username mismatch: got %v, want alice", tokenData["username"])
	}
}

func TestMigrateDataConsistency(t *testing.T) {
	if os.Getenv("MONGO_TEST_URI") == "" {
		t.Skip("MONGO_TEST_URI not set, skipping MongoDB migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		mongoStore.Close()
		mongoStore.(*mongoStorage).db.Drop(ctx)
	}()

	mongoData, err := mongoStore.Get(BucketUsers, "alice")
	if err != nil {
		t.Fatalf("failed to get data from MongoDB: %v", err)
	}

	// Verify data is identical
	var original, migrated map[string]interface{}
	json.Unmarshal([]byte(complexData), &original)
	json.Unmarshal(mongoData, &migrated)

	if migrated["username"] != original["username"] {
		t.Errorf("username mismatch: got %v, want %v", migrated["username"], original["username"])
	}

	// Verify nested object
	origPerms := original["permissions"].(map[string]interface{})
	migPerms := migrated["permissions"].(map[string]interface{})
	if migPerms["can_approve"] != origPerms["can_approve"] {
		t.Errorf("permissions.can_approve mismatch: got %v, want %v", migPerms["can_approve"], origPerms["can_approve"])
	}

	// Verify array
	origGroups := original["allowed_repo_groups"].([]interface{})
	migGroups := migrated["allowed_repo_groups"].([]interface{})
	if len(migGroups) != len(origGroups) {
		t.Errorf("allowed_repo_groups length mismatch: got %d, want %d", len(migGroups), len(origGroups))
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
	json.Unmarshal(bboltData, &roundTrip)

	if roundTrip["username"] != original["username"] {
		t.Errorf("round-trip username mismatch: got %v, want %v", roundTrip["username"], original["username"])
	}
}
