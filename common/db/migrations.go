package db

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"asika/common/models"
	"go.etcd.io/bbolt"
)

const migrationVersionKey = "__migration_version__"

type migration struct {
	Version int
	Name    string
	Apply   func(tx *bbolt.Tx) error
}

var migrationRegistry = []migration{
	{
		Version: 1,
		Name:    "add_migration_tracking",
		Apply: func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(BucketConfig))
			if b == nil {
				return nil
			}
			if b.Get([]byte(migrationVersionKey)) == nil {
				return b.Put([]byte(migrationVersionKey), []byte("0"))
			}
			return nil
		},
	},
	{
		Version: 2,
		Name:    "ensure_pr_spam_flag_defaults",
		Apply: func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(BucketPRs))
			if b == nil {
				return nil
			}
			c := b.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var pr models.PRRecord
				if err := json.Unmarshal(v, &pr); err != nil {
					continue
				}
				changed := false
				if pr.State == "spam" && !pr.SpamFlag {
					pr.SpamFlag = true
					changed = true
				}
				if changed {
					data, err := json.Marshal(pr)
					if err != nil {
						continue
					}
					if err := b.Put(k, data); err != nil {
						return err
					}
				}
			}
			return nil
		},
	},
}

func (s *bboltStorage) runMigrations() error {
	currentVersion := 0
	err := s.db.View(func(tx *bbolt.Tx) error {
		currentVersion = getCurrentVersion(tx)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to check migration version: %w", err)
	}
	if currentVersion >= len(migrationRegistry) {
		return nil
	}
	slog.Info("running database migrations", "current_version", currentVersion, "target_version", len(migrationRegistry))
	for _, m := range migrationRegistry {
		if m.Version <= currentVersion {
			continue
		}
		slog.Info("applying migration", "version", m.Version, "name", m.Name)
		if err := s.db.Update(m.Apply); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Name, err)
		}
		if err := s.db.Update(func(tx *bbolt.Tx) error {
			return setVersion(tx, m.Version)
		}); err != nil {
			return fmt.Errorf("failed to update migration version: %w", err)
		}
	}
	slog.Info("database migrations complete", "version", len(migrationRegistry))
	return nil
}

func getCurrentVersion(tx *bbolt.Tx) int {
	b := tx.Bucket([]byte(BucketConfig))
	if b == nil {
		return 0
	}
	val := b.Get([]byte(migrationVersionKey))
	if val == nil {
		return 0
	}
	var version int
	fmt.Sscanf(string(val), "%d", &version)
	return version
}

func setVersion(tx *bbolt.Tx, version int) error {
	b := tx.Bucket([]byte(BucketConfig))
	if b == nil {
		return fmt.Errorf("config bucket not found")
	}
	return b.Put([]byte(migrationVersionKey), []byte(fmt.Sprintf("%d", version)))
}
