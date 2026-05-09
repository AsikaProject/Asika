package testutil

import (
	"testing"

	"asika/common/db"
)

// NewTestDB creates a temporary bbolt database for testing and injects it
// as the default db.Storage via db.InitWithStorage.
func NewTestDB(t *testing.T) db.Storage {
	t.Helper()
	s, err := db.NewBboltStorage(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	db.InitWithStorage(s)
	t.Cleanup(func() {
		s.Close()
	})
	return s
}
