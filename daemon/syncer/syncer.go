package syncer

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"asika/common/models"
	"asika/common/platforms"
)

const (
	syncMaxRetries     = 3
	syncRetryBaseDelay = 2 * time.Second
)

// SyncRecordWriter writes sync records, optionally through a serialized writer.
type SyncRecordWriter interface {
	WriteSyncRecord(recordID string, data []byte) error
}

// Syncer handles cross-platform synchronization
type Syncer struct {
	cfg          *models.Config
	clients      map[platforms.PlatformType]platforms.PlatformClient
	syncLocks    sync.Map
	notifyFn     func(title, body string)
	notifyFnMu   sync.RWMutex
	recordWriter SyncRecordWriter
	recordMu     sync.RWMutex
}

// NewSyncer creates a new syncer
func NewSyncer(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Syncer {
	return &Syncer{
		cfg:     cfg,
		clients: clients,
	}
}

// SetNotifyFunc sets the notification function for sync conflict alerts.
func (s *Syncer) SetNotifyFunc(fn func(title, body string)) {
	s.notifyFnMu.Lock()
	s.notifyFn = fn
	s.notifyFnMu.Unlock()
}

// SetRecordWriter sets the writer for sync records.
// If nil, recordSync falls back to direct db.Put.
func (s *Syncer) SetRecordWriter(w SyncRecordWriter) {
	s.recordMu.Lock()
	s.recordWriter = w
	s.recordMu.Unlock()
}

// getTargetPlatforms returns all configured target platforms for sync.
func (s *Syncer) getTargetPlatforms(group *models.RepoGroup, sourcePlatform string) []struct {
	name string
	repo string
} {
	targets := []struct {
		name string
		repo string
	}{
		{"github", group.GitHub},
		{"gitlab", group.GitLab},
		{"gitea", group.Gitea},
		{"forgejo", group.Forgejo},
		{"codeberg", group.Codeberg},
		{"bitbucket", group.Bitbucket},
		{"gerrit", group.Gerrit},
	}
	result := make([]struct {
		name string
		repo string
	}, 0, len(targets))
	for _, t := range targets {
		if t.name != sourcePlatform && t.repo != "" {
			result = append(result, t)
		}
	}
	return result
}

// getRepoURL returns the clone URL (with .git suffix) for a platform repo.
func (s *Syncer) getRepoURL(platform, repo string) (string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repo format %q: expected owner/repo", repo)
	}
	switch platforms.PlatformType(platform) {
	case platforms.PlatformGitHub:
		return fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1]), nil
	case platforms.PlatformGitLab:
		base := s.cfg.GitLabBaseURL
		if base == "" {
			base = "https://gitlab.com"
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s/%s.git", base, parts[0], parts[1]), nil
	case platforms.PlatformGitea:
		base := s.cfg.GiteaBaseURL
		if base == "" {
			base = "https://gitea.example.com"
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s/%s.git", base, parts[0], parts[1]), nil
	case platforms.PlatformForgejo:
		base := s.cfg.ForgejoBaseURL
		if base == "" {
			base = "https://forgejo.example.com"
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s/%s.git", base, parts[0], parts[1]), nil
	case platforms.PlatformCodeberg:
		return fmt.Sprintf("https://codeberg.org/%s/%s.git", parts[0], parts[1]), nil
	case platforms.PlatformBitbucket:
		return fmt.Sprintf("https://bitbucket.org/%s/%s.git", parts[0], parts[1]), nil
	case platforms.PlatformGerrit:
		base := s.cfg.Tokens.Gerrit.URL
		if base == "" {
			return "", fmt.Errorf("gerrit URL is not configured")
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s", base, repo), nil
	}
	return "", fmt.Errorf("unsupported platform: %s", platform)
}

func (s *Syncer) getOrCreateLock(repoGroup string) *sync.Mutex {
	actual, _ := s.syncLocks.LoadOrStore(repoGroup, &sync.Mutex{})
	return actual.(*sync.Mutex)
}
