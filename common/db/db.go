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

var (
    DB *bbolt.DB
)

// Init initializes the BoltDB database
func Init(dbPath string) error {
    var err error
    DB, err = bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 30 * time.Second})
    if err != nil {
        return err
    }

    // Create all buckets if they don't exist
    return DB.Update(func(tx *bbolt.Tx) error {
        buckets := []string{
            BucketConfig,
            BucketRepos,
            BucketPRs,
            BucketLogs,
            BucketQueueItems,
            BucketUsers,
            BucketSyncHistory,
            BucketPRIndexByID,
            BucketPRIndexByRG,
            BucketWebhookRetries,
        }
        for _, bucket := range buckets {
            if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
                return err
            }
        }
        return nil
    })
}

// Close closes the database
func Close() error {
    if DB != nil {
        return DB.Close()
    }
    return nil
}

// BackupToFile creates a hot online backup of the bbolt database to the given path.
func BackupToFile(dest string) error {
    return DB.View(func(tx *bbolt.Tx) error {
        return tx.CopyFile(dest, 0600)
    })
}

// Ping checks if the database is accessible
func Ping() error {
    if DB == nil {
        return fmt.Errorf("database not initialized")
    }
    return DB.View(func(tx *bbolt.Tx) error {
        return nil
    })
}

// Update wraps bbolt Update
func Update(fn func(tx *bbolt.Tx) error) error {
    return DB.Update(fn)
}

// View wraps bbolt View
func View(fn func(tx *bbolt.Tx) error) error {
    return DB.View(fn)
}

// Put stores a key-value pair in the specified bucket
func Put(bucket, key string, value []byte) error {
    return DB.Update(func(tx *bbolt.Tx) error {
        b := tx.Bucket([]byte(bucket))
        if b == nil {
            return bbolt.ErrBucketNotFound
        }
        return b.Put([]byte(key), value)
    })
}

// Get retrieves a value by key from the specified bucket
func Get(bucket, key string) ([]byte, error) {
    var result []byte
    err := DB.View(func(tx *bbolt.Tx) error {
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

// Delete removes a key from the specified bucket
func Delete(bucket, key string) error {
    return DB.Update(func(tx *bbolt.Tx) error {
        b := tx.Bucket([]byte(bucket))
        if b == nil {
            return bbolt.ErrBucketNotFound
        }
        return b.Delete([]byte(key))
    })
}

// ForEach iterates over all key-value pairs in the specified bucket
func ForEach(bucket string, fn func(key, value []byte) error) error {
	return DB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return bbolt.ErrBucketNotFound
		}
		return b.ForEach(func(k, v []byte) error {
			return fn(k, v)
		})
	})
}

// ForEachPrefix iterates over key-value pairs in the index bucket with the given prefix,
// then fetches the actual value from the target bucket using the indexed key.
func ForEachPrefix(indexBucket, targetBucket, prefix string, fn func(key, value []byte) error) error {
	return DB.View(func(tx *bbolt.Tx) error {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BucketForEachPrefix iterates over key-value pairs in a bucket with the given key prefix.
// More efficient than ForEach when you only need a subset of keys.
func BucketForEachPrefix(bucket, prefix string, fn func(key, value []byte) error) error {
	return DB.View(func(tx *bbolt.Tx) error {
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

// PutPRWithIndex stores a PR and updates indices atomically
func PutPRWithIndex(key string, value []byte, prID, repoGroup string, prNumber int) error {
    return DB.Update(func(tx *bbolt.Tx) error {
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

// GetPRByIndex tries to find a PR using indices first, falling back to scan
func GetPRByIndex(prID, repoGroup string, prNumber int) ([]byte, error) {
    var result []byte

    err := DB.View(func(tx *bbolt.Tx) error {
        // Try index by ID
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

        // Try index by repo_group + number
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

// PutWebhookRetry stores a webhook retry entry
func PutWebhookRetry(retry *models.WebhookRetry) error {
    data, err := json.Marshal(retry)
    if err != nil {
        return err
    }
    return Put(BucketWebhookRetries, retry.ID, data)
}

// GetWebhookRetry retrieves a webhook retry by ID
func GetWebhookRetry(id string) (*models.WebhookRetry, error) {
    data, err := Get(BucketWebhookRetries, id)
    if err != nil || data == nil {
        return nil, err
    }
    var retry models.WebhookRetry
    if err := json.Unmarshal(data, &retry); err != nil {
        return nil, err
    }
    return &retry, nil
}

// DeleteWebhookRetry removes a webhook retry entry
func DeleteWebhookRetry(id string) error {
    return Delete(BucketWebhookRetries, id)
}

// ForEachWebhookRetry iterates over all webhook retry entries
func ForEachWebhookRetry(fn func(retry *models.WebhookRetry) error) error {
    return ForEach(BucketWebhookRetries, func(key, value []byte) error {
        var retry models.WebhookRetry
        if err := json.Unmarshal(value, &retry); err != nil {
            return nil // skip invalid entries
        }
        return fn(&retry)
    })
}

// GetDueWebhookRetries returns retries that are due for retry (NextRetry <= now)
func GetDueWebhookRetries(now time.Time) ([]*models.WebhookRetry, error) {
    var due []*models.WebhookRetry
    err := ForEachWebhookRetry(func(retry *models.WebhookRetry) error {
        if retry.NextRetry.IsZero() || retry.NextRetry.After(now) {
            return nil
        }
        due = append(due, retry)
        return nil
    })
    return due, err
}

// PutConfigSnapshot stores a config snapshot with a version timestamp key.
func PutConfigSnapshot(version int, data []byte) error {
    key := fmt.Sprintf("%06d", version)
    return Put(BucketConfigHistory, key, data)
}

// GetConfigSnapshot retrieves a config snapshot by version number.
func GetConfigSnapshot(version int) ([]byte, error) {
    key := fmt.Sprintf("%06d", version)
    return Get(BucketConfigHistory, key)
}

// ListConfigSnapshots returns all stored config snapshots (latest first).
// Returns slice of [version, data] pairs.
func ListConfigSnapshots(limit int) ([]struct {
    Version int
    Data    []byte
}, error) {
    var results []struct {
        Version int
        Data    []byte
    }
    err := DB.View(func(tx *bbolt.Tx) error {
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
            results = append(results, struct {
                Version int
                Data    []byte
            }{ver, val})
        }
        return nil
    })
    return results, err
}

// AppendAuditLog adds an audit log entry to the database
func AppendAuditLog(level, message string, ctx map[string]interface{}) error {
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
     // Use timestamp + random suffix to avoid key collisions
     var randBytes [4]byte
     rand.Read(randBytes[:])
     key := fmt.Sprintf("%d_%08x", log.Timestamp.UnixNano(), binary.BigEndian.Uint32(randBytes[:]))
     return Put(BucketLogs, key, data)
}
