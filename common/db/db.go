package db

import (
	"context"
	"fmt"
	"time"

	"asika/common/models"
)

// Storage defines the database operations used by asika.
// The default implementation is bboltStorage (wrapping go.etcd.io/bbolt).
// External implementations (e.g. MongoDB, PostgreSQL) can satisfy this
// interface and be injected via InitWithStorage.
type Storage interface {
	Close() error
	Ping() error
	Put(bucket, key string, value []byte) error
	Get(bucket, key string) ([]byte, error)
	Delete(bucket, key string) error
	ForEach(bucket string, fn func(key, value []byte) error) error
	ForEachPrefix(indexBucket, targetBucket, prefix string, fn func(key, value []byte) error) error
	BucketForEachPrefix(bucket, prefix string, fn func(key, value []byte) error) error
	PutPRWithIndex(key string, value []byte, prID, repoGroup string, prNumber int) error
	GetPRByIndex(prID, repoGroup string, prNumber int) ([]byte, error)
	PutWebhookRetry(retry *models.WebhookRetry) error
	GetWebhookRetry(id string) (*models.WebhookRetry, error)
	DeleteWebhookRetry(id string) error
	GetDueWebhookRetries(now time.Time) ([]*models.WebhookRetry, error)
	PutConfigSnapshot(version int, data []byte) error
	GetConfigSnapshot(version int) ([]byte, error)
	ListConfigSnapshots(limit int) ([]ConfigSnapshotEntry, error)
	AppendAuditLog(level, message string, ctx map[string]interface{}) error
	PutAPIKey(key *models.APIKey) error
	GetAPIKey(id string) (*models.APIKey, error)
	DeleteAPIKey(id string) error
	ListAPIKeys() ([]*models.APIKey, error)
}

// ConfigSnapshotEntry represents a stored config version.
type ConfigSnapshotEntry struct {
	Version int
	Data    []byte
}

var defaultStorage Storage

// Init initializes the default storage.
// Usage:
//
//	db.Init(cfg.Database)           // with DatabaseConfig (supports bbolt and mongo)
//	db.Init("path/to/file.db")      // shorthand for bbolt with just a file path
func Init(args ...interface{}) error {
	switch v := args[0].(type) {
	case models.DatabaseConfig:
		return initFromConfig(v)
	case string:
		return initFromConfig(models.DatabaseConfig{Type: "bbolt", Path: v})
	default:
		return fmt.Errorf("db.Init: unsupported argument type %T", args[0])
	}
}

func initFromConfig(cfg models.DatabaseConfig) error {
	switch cfg.Type {
	case "mongo":
		s, err := NewMongoStorage(context.Background(), cfg.Path, cfg.Name)
		if err != nil {
			return err
		}
		defaultStorage = s
		return nil
	default:
		s, err := newBboltStorage(cfg.Path)
		if err != nil {
			return err
		}
		defaultStorage = s
		return nil
	}
}

// InitWithStorage injects a custom Storage implementation.
func InitWithStorage(s Storage) {
	defaultStorage = s
}

func mustStorage() Storage {
	if defaultStorage == nil {
		panic("database not initialized: call db.Init() or db.InitWithStorage() first")
	}
	return defaultStorage
}

func Close() error                               { return mustStorage().Close() }
func Ping() error                                { return mustStorage().Ping() }
func Put(bucket, key string, value []byte) error { return mustStorage().Put(bucket, key, value) }
func Get(bucket, key string) ([]byte, error)     { return mustStorage().Get(bucket, key) }
func Delete(bucket, key string) error            { return mustStorage().Delete(bucket, key) }
func ForEach(bucket string, fn func(key, value []byte) error) error {
	return mustStorage().ForEach(bucket, fn)
}
func ForEachPrefix(indexBucket, targetBucket, prefix string, fn func(key, value []byte) error) error {
	return mustStorage().ForEachPrefix(indexBucket, targetBucket, prefix, fn)
}
func BucketForEachPrefix(bucket, prefix string, fn func(key, value []byte) error) error {
	return mustStorage().BucketForEachPrefix(bucket, prefix, fn)
}
func PutPRWithIndex(key string, value []byte, prID, repoGroup string, prNumber int) error {
	return mustStorage().PutPRWithIndex(key, value, prID, repoGroup, prNumber)
}
func GetPRByIndex(prID, repoGroup string, prNumber int) ([]byte, error) {
	return mustStorage().GetPRByIndex(prID, repoGroup, prNumber)
}
func PutWebhookRetry(retry *models.WebhookRetry) error { return mustStorage().PutWebhookRetry(retry) }
func GetWebhookRetry(id string) (*models.WebhookRetry, error) {
	return mustStorage().GetWebhookRetry(id)
}
func DeleteWebhookRetry(id string) error { return mustStorage().DeleteWebhookRetry(id) }
func GetDueWebhookRetries(now time.Time) ([]*models.WebhookRetry, error) {
	return mustStorage().GetDueWebhookRetries(now)
}
func PutConfigSnapshot(version int, data []byte) error {
	return mustStorage().PutConfigSnapshot(version, data)
}
func GetConfigSnapshot(version int) ([]byte, error) { return mustStorage().GetConfigSnapshot(version) }
func ListConfigSnapshots(limit int) ([]ConfigSnapshotEntry, error) {
	return mustStorage().ListConfigSnapshots(limit)
}
func AppendAuditLog(level, message string, ctx map[string]interface{}) error {
	return mustStorage().AppendAuditLog(level, message, ctx)
}
func PutAPIKey(key *models.APIKey) error          { return mustStorage().PutAPIKey(key) }
func GetAPIKey(id string) (*models.APIKey, error) { return mustStorage().GetAPIKey(id) }
func DeleteAPIKey(id string) error                { return mustStorage().DeleteAPIKey(id) }
func ListAPIKeys() ([]*models.APIKey, error)      { return mustStorage().ListAPIKeys() }
