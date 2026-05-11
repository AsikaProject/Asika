package reviewer

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"asika/common/platforms"
)

// CodeOwners represents a parsed CODEOWNERS file.
// Rules are ordered by specificity (last matching rule wins, like GitHub).
type CodeOwners struct {
	entries []codeOwnersEntry
}

type codeOwnersEntry struct {
	pattern  string
	owners   []string
	priority int
}

var (
	codeOwnersCache     = make(map[string]*CodeOwners)
	codeOwnersCacheMu   sync.RWMutex
	codeOwnersCacheTTL  = 5 * time.Minute
	codeOwnersCacheTime = make(map[string]time.Time)
)

// ParseCodeOwners parses a CODEOWNERS file content into a CodeOwners struct.
func ParseCodeOwners(content string) *CodeOwners {
	co := &CodeOwners{}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		pattern := parts[0]
		owners := make([]string, 0, len(parts)-1)
		for _, p := range parts[1:] {
			p = strings.TrimSpace(p)
			if p != "" {
				owners = append(owners, p)
			}
		}
		if len(owners) == 0 {
			continue
		}
		priority := len(strings.ReplaceAll(pattern, "/", ""))
		co.entries = append(co.entries, codeOwnersEntry{
			pattern:  pattern,
			owners:   owners,
			priority: priority,
		})
	}
	sort.Slice(co.entries, func(i, j int) bool {
		return co.entries[i].priority < co.entries[j].priority
	})
	return co
}

// Match returns the owners for a given file path based on CODEOWNERS rules.
// Last matching rule wins (GitHub semantics).
func (co *CodeOwners) Match(filePath string) []string {
	var owners []string
	for _, entry := range co.entries {
		if matchCodeOwnersPattern(entry.pattern, filePath) {
			owners = entry.owners
		}
	}
	return owners
}

// MatchFiles returns a deduplicated set of owners for a list of changed files.
func (co *CodeOwners) MatchFiles(files []string) []string {
	seen := make(map[string]bool)
	var owners []string
	for _, f := range files {
		for _, o := range co.Match(f) {
			if !seen[o] {
				seen[o] = true
				owners = append(owners, o)
			}
		}
	}
	return owners
}

func matchCodeOwnersPattern(pattern, filePath string) bool {
	if strings.HasPrefix(pattern, "/") {
		pattern = pattern[1:]
		match, _ := path.Match(pattern, filePath)
		return match
	}
	match, _ := path.Match(pattern, filePath)
	if match {
		return match
	}
	for i := 0; i <= len(filePath); i++ {
		suffix := filePath[i:]
		match, _ := path.Match(pattern, suffix)
		if match {
			return true
		}
		if i < len(filePath) && filePath[i] == '/' {
			rest := filePath[i+1:]
			match, _ := path.Match(pattern, rest)
			if match {
				return true
			}
		}
	}
	return false
}

// GetCodeOwnersForRepo fetches and parses CODEOWNERS from a repository.
// Uses in-memory caching with TTL.
func GetCodeOwnersForRepo(ctx context.Context, client platforms.PlatformClient, owner, repo string) (*CodeOwners, error) {
	cacheKey := fmt.Sprintf("%s/%s", owner, repo)

	codeOwnersCacheMu.RLock()
	if co, ok := codeOwnersCache[cacheKey]; ok {
		if ts, ok := codeOwnersCacheTime[cacheKey]; ok && time.Since(ts) < codeOwnersCacheTTL {
			codeOwnersCacheMu.RUnlock()
			return co, nil
		}
	}
	codeOwnersCacheMu.RUnlock()

	locations := []string{"CODEOWNERS", ".github/CODEOWNERS", ".gitlab/CODEOWNERS", "docs/CODEOWNERS"}
	for _, loc := range locations {
		content, err := client.GetFileContent(ctx, owner, repo, loc)
		if err == nil && content != "" {
			co := ParseCodeOwners(content)
			codeOwnersCacheMu.Lock()
			codeOwnersCache[cacheKey] = co
			codeOwnersCacheTime[cacheKey] = time.Now()
			codeOwnersCacheMu.Unlock()
			slog.Info("CODEOWNERS loaded", "repo", cacheKey, "location", loc, "rules", len(co.entries))
			return co, nil
		}
	}

	slog.Debug("no CODEOWNERS found", "repo", cacheKey)
	return nil, nil
}

// ClearCodeOwnersCache clears the CODEOWNERS cache (useful for testing).
func ClearCodeOwnersCache() {
	codeOwnersCacheMu.Lock()
	defer codeOwnersCacheMu.Unlock()
	codeOwnersCache = make(map[string]*CodeOwners)
	codeOwnersCacheTime = make(map[string]time.Time)
}
