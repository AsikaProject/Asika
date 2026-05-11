package db

import (
	"encoding/json"
	"fmt"

	"go.etcd.io/bbolt"
)

func (s *bboltStorage) PutConfigSnapshot(version int, data []byte) error {
	key := fmt.Sprintf("%06d", version)
	return s.Put(BucketConfigHistory, key, data)
}

func (s *bboltStorage) GetConfigSnapshot(version int) ([]byte, error) {
	key := fmt.Sprintf("%06d", version)
	return s.Get(BucketConfigHistory, key)
}

func (s *bboltStorage) ListConfigSnapshots(limit int) ([]ConfigSnapshotEntry, error) {
	var results []ConfigSnapshotEntry
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketConfigHistory))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			if limit > 0 && len(results) >= limit {
				break
			}
			var ver int
			fmt.Sscanf(string(k), "%d", &ver)
			val := make([]byte, len(v))
			copy(val, v)
			results = append(results, ConfigSnapshotEntry{ver, val})
		}
		return nil
	})
	return results, err
}

func (s *bboltStorage) PutReportHistory(id string, data []byte) error {
	return s.Put(BucketReportHistory, id, data)
}

func (s *bboltStorage) ListReportHistory(limit int) ([]ReportHistoryEntry, error) {
	var results []ReportHistoryEntry
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketReportHistory))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			if limit > 0 && len(results) >= limit {
				break
			}
			var entry ReportHistoryEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				continue
			}
			entry.ID = string(k)
			results = append(results, entry)
		}
		return nil
	})
	return results, err
}
