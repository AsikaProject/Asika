package testutil

import (
    "testing"

    "go.etcd.io/bbolt"
)

// NewTestDB creates a temporary bbolt database for testing
func NewTestDB(t *testing.T) *bbolt.DB {
    t.Helper()
    db, err := bbolt.Open(t.TempDir()+"/test.db", 0600, nil)
    if err != nil {
        t.Fatalf("failed to create test db: %v", err)
    }

    // Create buckets
    db.Update(func(tx *bbolt.Tx) error {
        buckets := []string{"config", "repos", "prs", "logs", "queue_items", "users", "sync_history", "pr_index_by_id", "pr_index_by_rg_num", "webhook_retries", "config_history"}
        for _, b := range buckets {
            tx.CreateBucketIfNotExists([]byte(b))
        }
        return nil
    })

    t.Cleanup(func() {
        db.Close()
    })

    return db
}
