package db

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"asika/common/models"
	"go.etcd.io/bbolt"
)

type bboltStorage struct {
	db *bbolt.DB
}

// NewBboltStorage creates a bboltStorage from a db file path.
// Used by testutil and external packages that need a db.Storage implementation.
func NewBboltStorage(dbPath string) (Storage, error) {
	return newBboltStorage(dbPath)
}

func newBboltStorage(dbPath string) (*bboltStorage, error) {
	d, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	err = d.Update(func(tx *bbolt.Tx) error {
		buckets := []string{
		BucketConfig, BucketRepos, BucketPRs, BucketLogs, BucketQueueItems,
		BucketUsers, BucketSyncHistory, BucketPRIndexByID, BucketPRIndexByRG,
		BucketWebhookRetries, BucketConfigHistory, BucketAPIKeys, BucketSpamAuthors,
		BucketWebhookHealth, BucketReportHistory, BucketNotificationPrefs,
		BucketNotificationDedup, BucketTeamSpaces, BucketSpaceMembers,
		BucketSpaceSettings,
		}
		for _, b := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(b)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		d.Close()
		return nil, err
	}
	return &bboltStorage{db: d}, nil
}

func (s *bboltStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *bboltStorage) Ping() error {
	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}
	return s.db.View(func(tx *bbolt.Tx) error { return nil })
}

func (s *bboltStorage) Put(bucket, key string, value []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		return b.Put([]byte(key), value)
	})
}

func (s *bboltStorage) Get(bucket, key string) ([]byte, error) {
	var result []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		val := b.Get([]byte(key))
		if val != nil {
			result = make([]byte, len(val))
			copy(result, val)
		}
		return nil
	})
	return result, err
}

func (s *bboltStorage) Delete(bucket, key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		return b.Delete([]byte(key))
	})
}

func (s *bboltStorage) ForEach(bucket string, fn func(key, value []byte) error) error {
	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		return b.ForEach(func(k, v []byte) error { return fn(k, v) })
	})
}

func (s *bboltStorage) ForEachPrefix(indexBucket, targetBucket, prefix string, fn func(key, value []byte) error) error {
	return s.db.View(func(tx *bbolt.Tx) error {
		idxB := tx.Bucket([]byte(indexBucket))
		if idxB == nil {
			return bbolt.ErrBucketNotFound
		}
		targetB := tx.Bucket([]byte(targetBucket))
		if targetB == nil {
			return bbolt.ErrBucketNotFound
		}
		c := idxB.Cursor()
		for k, v := c.Seek([]byte(prefix)); k != nil && string(k[:min(len(k), len(prefix))]) == prefix; k, v = c.Next() {
			if v == nil {
				continue
			}
			val := targetB.Get(v)
			if val != nil {
				if err := fn(k, val); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *bboltStorage) BucketForEachPrefix(bucket, prefix string, fn func(key, value []byte) error) error {
	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		c := b.Cursor()
		p := []byte(prefix)
		for k, v := c.Seek(p); k != nil && string(k[:min(len(k), len(p))]) == prefix; k, v = c.Next() {
			if err := fn(k, v); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *bboltStorage) PutPRWithIndex(key string, value []byte, prID, repoGroup string, prNumber int) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketPRs))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		if err := b.Put([]byte(key), value); err != nil {
			return err
		}
		if prID != "" {
			idxB := tx.Bucket([]byte(BucketPRIndexByID))
			if idxB != nil {
				idxB.Put([]byte(prID), []byte(key))
			}
		}
		if repoGroup != "" {
			idxB := tx.Bucket([]byte(BucketPRIndexByRG))
			if idxB != nil {
				rgKey := fmt.Sprintf("%s:%d", repoGroup, prNumber)
				idxB.Put([]byte(rgKey), []byte(key))
			}
		}
		return nil
	})
}

func (s *bboltStorage) GetPRByIndex(prID, repoGroup string, prNumber int) ([]byte, error) {
	var result []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		if prID != "" {
			idxB := tx.Bucket([]byte(BucketPRIndexByID))
			if idxB != nil {
				if key := idxB.Get([]byte(prID)); key != nil {
					b := tx.Bucket([]byte(BucketPRs))
					if b != nil {
						if val := b.Get(key); val != nil {
							result = make([]byte, len(val))
							copy(result, val)
							return nil
						}
					}
				}
			}
		}
		if repoGroup != "" && prNumber > 0 {
			idxB := tx.Bucket([]byte(BucketPRIndexByRG))
			if idxB != nil {
				rgKey := fmt.Sprintf("%s:%d", repoGroup, prNumber)
				if key := idxB.Get([]byte(rgKey)); key != nil {
					b := tx.Bucket([]byte(BucketPRs))
					if b != nil {
						if val := b.Get(key); val != nil {
							result = make([]byte, len(val))
							copy(result, val)
							return nil
						}
					}
				}
			}
		}
		return nil
	})
	return result, err
}

func (s *bboltStorage) PutWebhookRetry(retry *models.WebhookRetry) error {
	data, err := json.Marshal(retry)
	if err != nil {
		return err
	}
	return s.Put(BucketWebhookRetries, retry.ID, data)
}

func (s *bboltStorage) GetWebhookRetry(id string) (*models.WebhookRetry, error) {
	data, err := s.Get(BucketWebhookRetries, id)
	if err != nil || data == nil {
		return nil, err
	}
	var retry models.WebhookRetry
	if err := json.Unmarshal(data, &retry); err != nil {
		return nil, err
	}
	return &retry, nil
}

func (s *bboltStorage) DeleteWebhookRetry(id string) error {
	return s.Delete(BucketWebhookRetries, id)
}

func (s *bboltStorage) GetDueWebhookRetries(now time.Time) ([]*models.WebhookRetry, error) {
	var due []*models.WebhookRetry
	err := s.ForEach(BucketWebhookRetries, func(key, value []byte) error {
		var retry models.WebhookRetry
		if err := json.Unmarshal(value, &retry); err != nil {
			return nil
		}
		if retry.NextRetry.IsZero() || retry.NextRetry.After(now) {
			return nil
		}
		due = append(due, &retry)
		return nil
	})
	return due, err
}

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

func (s *bboltStorage) AppendAuditLogEx(entry models.AuditLog) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	var randBytes [4]byte
	rand.Read(randBytes[:])
	key := fmt.Sprintf("%d_%08x", entry.Timestamp.UnixNano(), binary.BigEndian.Uint32(randBytes[:]))
	return s.Put(BucketLogs, key, data)
}

func (s *bboltStorage) AppendAuditLog(level, message string, ctx map[string]interface{}) error {
	log := models.AuditLog{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Context:   ctx,
	}
	data, err := json.Marshal(log)
	if err != nil {
		return err
	}
	var randBytes [4]byte
	rand.Read(randBytes[:])
	key := fmt.Sprintf("%d_%08x", log.Timestamp.UnixNano(), binary.BigEndian.Uint32(randBytes[:]))
	return s.Put(BucketLogs, key, data)
}

func (s *bboltStorage) PutAPIKey(key *models.APIKey) error {
	data, err := json.Marshal(key)
	if err != nil {
		return err
	}
	return s.Put(BucketAPIKeys, key.ID, data)
}

func (s *bboltStorage) GetAPIKey(id string) (*models.APIKey, error) {
	data, err := s.Get(BucketAPIKeys, id)
	if err != nil {
		return nil, err
	}
	var key models.APIKey
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *bboltStorage) DeleteAPIKey(id string) error {
	return s.Delete(BucketAPIKeys, id)
}

func (s *bboltStorage) ListAPIKeys() ([]*models.APIKey, error) {
	var keys []*models.APIKey
	err := s.ForEach(BucketAPIKeys, func(key, value []byte) error {
		var k models.APIKey
		if err := json.Unmarshal(value, &k); err != nil {
			return nil
		}
		keys = append(keys, &k)
		return nil
	})
	return keys, err
}

func (s *bboltStorage) PutNotificationPrefs(username string, data []byte) error {
	return s.Put(BucketNotificationPrefs, username, data)
}

func (s *bboltStorage) GetNotificationPrefs(username string) ([]byte, error) {
	return s.Get(BucketNotificationPrefs, username)
}

func (s *bboltStorage) PutNotificationDedup(key string, data []byte) error {
	return s.Put(BucketNotificationDedup, key, data)
}

func (s *bboltStorage) GetNotificationDedup(key string) ([]byte, error) {
	return s.Get(BucketNotificationDedup, key)
}

func (s *bboltStorage) PutTeamSpace(space *models.TeamSpace) error {
	data, err := json.Marshal(space)
	if err != nil {
		return err
	}
	return s.Put(BucketTeamSpaces, space.Name, data)
}

func (s *bboltStorage) GetTeamSpace(name string) (*models.TeamSpace, error) {
	data, err := s.Get(BucketTeamSpaces, name)
	if err != nil || data == nil {
		return nil, err
	}
	var space models.TeamSpace
	if err := json.Unmarshal(data, &space); err != nil {
		return nil, err
	}
	return &space, nil
}

func (s *bboltStorage) ListTeamSpaces() ([]*models.TeamSpace, error) {
	var spaces []*models.TeamSpace
	err := s.ForEach(BucketTeamSpaces, func(key, value []byte) error {
		var space models.TeamSpace
		if err := json.Unmarshal(value, &space); err != nil {
			return nil
		}
		spaces = append(spaces, &space)
		return nil
	})
	return spaces, err
}

func (s *bboltStorage) DeleteTeamSpace(name string) error {
	return s.Delete(BucketTeamSpaces, name)
}

func (s *bboltStorage) PutSpaceMember(spaceName, username, role string) error {
	key := fmt.Sprintf("%s:%s", spaceName, username)
	member := models.SpaceMember{Username: username, Role: role, JoinedAt: time.Now()}
	data, err := json.Marshal(member)
	if err != nil {
		return err
	}
	return s.Put(BucketSpaceMembers, key, data)
}

func (s *bboltStorage) RemoveSpaceMember(spaceName, username string) error {
	key := fmt.Sprintf("%s:%s", spaceName, username)
	return s.Delete(BucketSpaceMembers, key)
}

func (s *bboltStorage) GetSpaceMembers(spaceName string) ([]models.SpaceMember, error) {
	var members []models.SpaceMember
	prefix := spaceName + ":"
	err := s.BucketForEachPrefix(BucketSpaceMembers, prefix, func(key, value []byte) error {
		var m models.SpaceMember
		if err := json.Unmarshal(value, &m); err != nil {
			return nil
		}
		members = append(members, m)
		return nil
	})
	return members, err
}

func (s *bboltStorage) GetUserSpaces(username string) ([]string, error) {
	var spaces []string
	err := s.ForEach(BucketSpaceMembers, func(key, value []byte) error {
		parts := strings.SplitN(string(key), ":", 2)
		if len(parts) == 2 && parts[1] == username {
			spaces = append(spaces, parts[0])
		}
		return nil
	})
	return spaces, err
}

func (s *bboltStorage) PutSpaceSetting(spaceName, key string, value []byte) error {
	fullKey := fmt.Sprintf("%s:%s", spaceName, key)
	return s.Put(BucketSpaceSettings, fullKey, value)
}

func (s *bboltStorage) GetSpaceSetting(spaceName, key string) ([]byte, error) {
	fullKey := fmt.Sprintf("%s:%s", spaceName, key)
	return s.Get(BucketSpaceSettings, fullKey)
}

func (s *bboltStorage) DeleteNotificationDedup(key string) error {
	return s.Delete(BucketNotificationDedup, key)
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

func (s *bboltStorage) PutWebhookHealth(repoGroup, platform string, ts time.Time) error {
	key := fmt.Sprintf("%s:%s", repoGroup, platform)
	return s.Put(BucketWebhookHealth, key, []byte(ts.Format(time.RFC3339)))
}

func (s *bboltStorage) GetWebhookHealth(repoGroup, platform string) (time.Time, error) {
	key := fmt.Sprintf("%s:%s", repoGroup, platform)
	data, err := s.Get(BucketWebhookHealth, key)
	if err != nil || data == nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, string(data))
}

func (s *bboltStorage) ListWebhookHealth() (map[string]time.Time, error) {
	result := make(map[string]time.Time)
	err := s.ForEach(BucketWebhookHealth, func(key, value []byte) error {
		ts, err := time.Parse(time.RFC3339, string(value))
		if err != nil {
			return nil
		}
		result[string(key)] = ts
		return nil
	})
	return result, err
}

func (s *bboltStorage) PutSpamAuthor(author *models.SpamAuthor) error {
	data, err := json.Marshal(author)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%s", author.Author, author.Platform)
	return s.Put(BucketSpamAuthors, key, data)
}

func (s *bboltStorage) GetSpamAuthor(author, platform string) (*models.SpamAuthor, error) {
	key := fmt.Sprintf("%s:%s", author, platform)
	data, err := s.Get(BucketSpamAuthors, key)
	if err != nil || data == nil {
		return nil, err
	}
	var sa models.SpamAuthor
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

func (s *bboltStorage) ListSpamAuthors() ([]*models.SpamAuthor, error) {
	var authors []*models.SpamAuthor
	err := s.ForEach(BucketSpamAuthors, func(key, value []byte) error {
		var sa models.SpamAuthor
		if err := json.Unmarshal(value, &sa); err != nil {
			return nil
		}
		authors = append(authors, &sa)
		return nil
	})
	return authors, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BackupToFile creates a hot online backup (bbolt-specific, not on Storage interface).
func BackupToFile(dest string) error {
	s, ok := defaultStorage.(*bboltStorage)
	if !ok {
		return fmt.Errorf("backup only supported on bbolt storage")
	}
	return s.db.View(func(tx *bbolt.Tx) error {
		return tx.CopyFile(dest, 0600)
	})
}

// RunMigrations runs bbolt-specific migrations.
func RunMigrations() error {
	s, ok := defaultStorage.(*bboltStorage)
	if !ok {
		return nil
	}
	return s.runMigrations()
}
