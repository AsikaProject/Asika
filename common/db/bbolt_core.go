package db

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"asika/common/models"
	"go.etcd.io/bbolt"
)

type bboltStorage struct {
	db *bbolt.DB
}

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
			BucketIssuePRLinks, BucketPRDependencies, BucketPRTemplates,
			BucketSerialQueue, BucketCrossSpaceDeps, BucketEscalationRules,
			BucketPRStacks, BucketAuditLogIndex,
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
	err = s.Put(BucketLogs, key, data)
	if err != nil {
		return err
	}
	return s.writeAuditLogIndex(key, entry)
}

func (s *bboltStorage) writeAuditLogIndex(logKey string, entry models.AuditLog) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAuditLogIndex))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		if entry.Actor != "" {
			idxKey := fmt.Sprintf("actor:%s:%s", entry.Actor, logKey)
			if err := b.Put([]byte(idxKey), []byte(logKey)); err != nil {
				return err
			}
		}
		if entry.RepoGroup != "" {
			idxKey := fmt.Sprintf("repo_group:%s:%s", entry.RepoGroup, logKey)
			if err := b.Put([]byte(idxKey), []byte(logKey)); err != nil {
				return err
			}
		}
		if entry.Action != "" {
			idxKey := fmt.Sprintf("action:%s:%s", entry.Action, logKey)
			if err := b.Put([]byte(idxKey), []byte(logKey)); err != nil {
				return err
			}
		}
		if entry.Category != "" {
			idxKey := fmt.Sprintf("category:%s:%s", entry.Category, logKey)
			if err := b.Put([]byte(idxKey), []byte(logKey)); err != nil {
				return err
			}
		}
		if entry.RepoGroup != "" && entry.PRNumber > 0 {
			idxKey := fmt.Sprintf("pr:%s:%d:%s", entry.RepoGroup, entry.PRNumber, logKey)
			if err := b.Put([]byte(idxKey), []byte(logKey)); err != nil {
				return err
			}
		}
		return nil
	})
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *bboltStorage) PutIssuePRLink(link *models.IssuePRLink) error {
	data, err := json.Marshal(link)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%s", link.IssueID, link.PRID)
	return s.Put(BucketIssuePRLinks, key, data)
}

func (s *bboltStorage) GetIssuePRLinksByIssue(issueID string) ([]*models.IssuePRLink, error) {
	var links []*models.IssuePRLink
	prefix := issueID + ":"
	err := s.BucketForEachPrefix(BucketIssuePRLinks, prefix, func(key, value []byte) error {
		var link models.IssuePRLink
		if err := json.Unmarshal(value, &link); err != nil {
			return nil
		}
		links = append(links, &link)
		return nil
	})
	return links, err
}

func (s *bboltStorage) GetIssuePRLinksByPR(prID string) ([]*models.IssuePRLink, error) {
	var links []*models.IssuePRLink
	err := s.ForEach(BucketIssuePRLinks, func(key, value []byte) error {
		var link models.IssuePRLink
		if err := json.Unmarshal(value, &link); err != nil {
			return nil
		}
		if link.PRID == prID {
			links = append(links, &link)
		}
		return nil
	})
	return links, err
}

func (s *bboltStorage) PutPRDependency(dep *models.PRDependency) error {
	data, err := json.Marshal(dep)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%s", dep.PRID, dep.DependsOnPRID)
	return s.Put(BucketPRDependencies, key, data)
}

func (s *bboltStorage) GetPRDependenciesByPR(prID string) ([]*models.PRDependency, error) {
	var deps []*models.PRDependency
	prefix := prID + ":"
	err := s.BucketForEachPrefix(BucketPRDependencies, prefix, func(key, value []byte) error {
		var dep models.PRDependency
		if err := json.Unmarshal(value, &dep); err != nil {
			return nil
		}
		deps = append(deps, &dep)
		return nil
	})
	return deps, err
}

func (s *bboltStorage) GetPRDependentsByPR(prID string) ([]*models.PRDependency, error) {
	var deps []*models.PRDependency
	err := s.ForEach(BucketPRDependencies, func(key, value []byte) error {
		var dep models.PRDependency
		if err := json.Unmarshal(value, &dep); err != nil {
			return nil
		}
		if dep.DependsOnPRID == prID {
			deps = append(deps, &dep)
		}
		return nil
	})
	return deps, err
}

func (s *bboltStorage) PutPRTemplate(tpl *models.PRTemplate) error {
	data, err := json.Marshal(tpl)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%s", tpl.RepoGroup, tpl.Platform)
	return s.Put(BucketPRTemplates, key, data)
}

func (s *bboltStorage) GetPRTemplate(repoGroup, platform string) (*models.PRTemplate, error) {
	key := fmt.Sprintf("%s:%s", repoGroup, platform)
	data, err := s.Get(BucketPRTemplates, key)
	if err != nil || data == nil {
		return nil, err
	}
	var tpl models.PRTemplate
	if err := json.Unmarshal(data, &tpl); err != nil {
		return nil, err
	}
	return &tpl, nil
}

func (s *bboltStorage) PutPRStack(stack *models.PRStack) error {
	data, err := json.Marshal(stack)
	if err != nil {
		return err
	}
	return s.Put(BucketPRStacks, stack.ID, data)
}

func (s *bboltStorage) GetPRStack(id string) (*models.PRStack, error) {
	data, err := s.Get(BucketPRStacks, id)
	if err != nil || data == nil {
		return nil, err
	}
	var stack models.PRStack
	if err := json.Unmarshal(data, &stack); err != nil {
		return nil, err
	}
	return &stack, nil
}

func (s *bboltStorage) ListPRStacks() ([]*models.PRStack, error) {
	var stacks []*models.PRStack
	err := s.ForEach(BucketPRStacks, func(key, value []byte) error {
		var stack models.PRStack
		if err := json.Unmarshal(value, &stack); err != nil {
			return nil
		}
		stacks = append(stacks, &stack)
		return nil
	})
	return stacks, err
}

func (s *bboltStorage) DeletePRStack(id string) error {
	return s.Delete(BucketPRStacks, id)
}
