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
	AppendAuditLogEx(entry models.AuditLog) error
	PutAPIKey(key *models.APIKey) error
	GetAPIKey(id string) (*models.APIKey, error)
	DeleteAPIKey(id string) error
	ListAPIKeys() ([]*models.APIKey, error)
	PutSpamAuthor(author *models.SpamAuthor) error
	GetSpamAuthor(author, platform string) (*models.SpamAuthor, error)
	ListSpamAuthors() ([]*models.SpamAuthor, error)
	PutWebhookHealth(repoGroup, platform string, ts time.Time) error
	GetWebhookHealth(repoGroup, platform string) (time.Time, error)
	ListWebhookHealth() (map[string]time.Time, error)
	PutReportHistory(id string, data []byte) error
	ListReportHistory(limit int) ([]ReportHistoryEntry, error)
	PutNotificationPrefs(username string, data []byte) error
	GetNotificationPrefs(username string) ([]byte, error)
	PutNotificationDedup(key string, data []byte) error
	GetNotificationDedup(key string) ([]byte, error)
	DeleteNotificationDedup(key string) error
	ListNotificationPrefs(usernames []string) ([]models.NotificationPreferences, error)
	PutTeamSpace(space *models.TeamSpace) error
	GetTeamSpace(name string) (*models.TeamSpace, error)
	ListTeamSpaces() ([]*models.TeamSpace, error)
	DeleteTeamSpace(name string) error
	PutSpaceMember(spaceName, username string, role string) error
	RemoveSpaceMember(spaceName, username string) error
	GetSpaceMembers(spaceName string) ([]models.SpaceMember, error)
	GetUserSpaces(username string) ([]string, error)
	PutSpaceSetting(spaceName, key string, value []byte) error
	GetSpaceSetting(spaceName, key string) ([]byte, error)
	PutIssuePRLink(link *models.IssuePRLink) error
	GetIssuePRLinksByIssue(issueID string) ([]*models.IssuePRLink, error)
	GetIssuePRLinksByPR(prID string) ([]*models.IssuePRLink, error)
	PutPRDependency(dep *models.PRDependency) error
	GetPRDependenciesByPR(prID string) ([]*models.PRDependency, error)
	GetPRDependentsByPR(prID string) ([]*models.PRDependency, error)
	PutPRTemplate(tpl *models.PRTemplate) error
	GetPRTemplate(repoGroup, platform string) (*models.PRTemplate, error)
	PutPRStack(stack *models.PRStack) error
	GetPRStack(id string) (*models.PRStack, error)
	ListPRStacks() ([]*models.PRStack, error)
	DeletePRStack(id string) error
}

// ConfigSnapshotEntry represents a stored config version.
type ConfigSnapshotEntry struct {
	Version int
	Data    []byte
}

// ReportHistoryEntry represents a stored generated report.
type ReportHistoryEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Period    int       `json:"period_days"`
	Content   string    `json:"content"`
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
func AppendAuditLogEx(entry models.AuditLog) error {
	return mustStorage().AppendAuditLogEx(entry)
}
func PutAPIKey(key *models.APIKey) error            { return mustStorage().PutAPIKey(key) }
func GetAPIKey(id string) (*models.APIKey, error)   { return mustStorage().GetAPIKey(id) }
func DeleteAPIKey(id string) error                  { return mustStorage().DeleteAPIKey(id) }
func ListAPIKeys() ([]*models.APIKey, error)        { return mustStorage().ListAPIKeys() }
func PutSpamAuthor(author *models.SpamAuthor) error { return mustStorage().PutSpamAuthor(author) }
func GetSpamAuthor(author, platform string) (*models.SpamAuthor, error) {
	return mustStorage().GetSpamAuthor(author, platform)
}
func ListSpamAuthors() ([]*models.SpamAuthor, error) { return mustStorage().ListSpamAuthors() }
func PutWebhookHealth(repoGroup, platform string, ts time.Time) error {
	return mustStorage().PutWebhookHealth(repoGroup, platform, ts)
}
func GetWebhookHealth(repoGroup, platform string) (time.Time, error) {
	return mustStorage().GetWebhookHealth(repoGroup, platform)
}
func ListWebhookHealth() (map[string]time.Time, error) {
	return mustStorage().ListWebhookHealth()
}
func PutReportHistory(id string, data []byte) error {
	return mustStorage().PutReportHistory(id, data)
}
func ListReportHistory(limit int) ([]ReportHistoryEntry, error) {
	return mustStorage().ListReportHistory(limit)
}
func PutNotificationPrefs(username string, data []byte) error {
	return mustStorage().PutNotificationPrefs(username, data)
}
func GetNotificationPrefs(username string) ([]byte, error) {
	return mustStorage().GetNotificationPrefs(username)
}
func PutNotificationDedup(key string, data []byte) error {
	return mustStorage().PutNotificationDedup(key, data)
}
func GetNotificationDedup(key string) ([]byte, error) {
	return mustStorage().GetNotificationDedup(key)
}
func DeleteNotificationDedup(key string) error {
	return mustStorage().DeleteNotificationDedup(key)
}
func ListNotificationPrefs(usernames []string) ([]models.NotificationPreferences, error) {
	return mustStorage().ListNotificationPrefs(usernames)
}
func PutTeamSpace(space *models.TeamSpace) error {
	return mustStorage().PutTeamSpace(space)
}
func GetTeamSpace(name string) (*models.TeamSpace, error) {
	return mustStorage().GetTeamSpace(name)
}
func ListTeamSpaces() ([]*models.TeamSpace, error) {
	return mustStorage().ListTeamSpaces()
}
func DeleteTeamSpace(name string) error {
	return mustStorage().DeleteTeamSpace(name)
}
func PutSpaceMember(spaceName, username, role string) error {
	return mustStorage().PutSpaceMember(spaceName, username, role)
}
func RemoveSpaceMember(spaceName, username string) error {
	return mustStorage().RemoveSpaceMember(spaceName, username)
}
func GetSpaceMembers(spaceName string) ([]models.SpaceMember, error) {
	return mustStorage().GetSpaceMembers(spaceName)
}
func GetUserSpaces(username string) ([]string, error) {
	return mustStorage().GetUserSpaces(username)
}
func PutSpaceSetting(spaceName, key string, value []byte) error {
	return mustStorage().PutSpaceSetting(spaceName, key, value)
}
func GetSpaceSetting(spaceName, key string) ([]byte, error) {
	return mustStorage().GetSpaceSetting(spaceName, key)
}
func PutIssuePRLink(link *models.IssuePRLink) error {
	return mustStorage().PutIssuePRLink(link)
}
func GetIssuePRLinksByIssue(issueID string) ([]*models.IssuePRLink, error) {
	return mustStorage().GetIssuePRLinksByIssue(issueID)
}
func GetIssuePRLinksByPR(prID string) ([]*models.IssuePRLink, error) {
	return mustStorage().GetIssuePRLinksByPR(prID)
}
func PutPRDependency(dep *models.PRDependency) error {
	return mustStorage().PutPRDependency(dep)
}
func GetPRDependenciesByPR(prID string) ([]*models.PRDependency, error) {
	return mustStorage().GetPRDependenciesByPR(prID)
}
func GetPRDependentsByPR(prID string) ([]*models.PRDependency, error) {
	return mustStorage().GetPRDependentsByPR(prID)
}
func PutPRTemplate(tpl *models.PRTemplate) error {
	return mustStorage().PutPRTemplate(tpl)
}
func GetPRTemplate(repoGroup, platform string) (*models.PRTemplate, error) {
	return mustStorage().GetPRTemplate(repoGroup, platform)
}
func PutPRStack(stack *models.PRStack) error {
	return mustStorage().PutPRStack(stack)
}
func GetPRStack(id string) (*models.PRStack, error) {
	return mustStorage().GetPRStack(id)
}
func ListPRStacks() ([]*models.PRStack, error) {
	return mustStorage().ListPRStacks()
}
func DeletePRStack(id string) error {
	return mustStorage().DeletePRStack(id)
}
