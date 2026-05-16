package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	millisecond = time.Millisecond
	second      = time.Second
	minute      = time.Minute
	hour        = time.Hour
	day         = 24 * time.Hour
)

// Time constants for durations.
var (
	// Duration constants for common time spans.
	Duration = struct {
		Millisecond time.Duration
		Second      time.Duration
		Minute      time.Duration
		Hour        time.Duration
		Day         time.Duration
	}{
		Millisecond: millisecond,
		Second:      second,
		Minute:      minute,
		Hour:        hour,
		Day:         day,
	}
)

type cacheEntry struct {
	value     []byte
	expiresAt time.Time
}

// Cache is a simple in-memory cache with TTL.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
}

// New creates a new cache.
func New() *Cache {
	return &Cache{
		entries: make(map[string]*cacheEntry),
	}
}

// Get returns the cached value for the given key, or nil if expired/missing.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, false
	}
	return entry.value, true
}

// Set stores a value with the given TTL.
func (c *Cache) Set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	c.entries[key] = &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	c.mu.Unlock()
}

// Delete removes a key from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Cleanup removes all expired entries.
func (c *Cache) Cleanup() {
	now := time.Now()
	c.mu.Lock()
	for k, v := range c.entries {
		if now.After(v.expiresAt) {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

// Key generates a cache key from the given parts.
func Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ApprovalCache caches approval status for PRs.
type ApprovalCache struct {
	cache *Cache
	ttl   time.Duration
}

// NewApprovalCache creates a new approval cache with the given TTL.
func NewApprovalCache(ttl time.Duration) *ApprovalCache {
	return &ApprovalCache{
		cache: New(),
		ttl:   ttl,
	}
}

// Key generates a cache key for an approval status.
func (c *ApprovalCache) Key(owner, repo string, prNumber int) string {
	return Key("approval", owner, repo, fmt.Sprintf("%d", prNumber))
}

// Get returns cached approval status.
func (c *ApprovalCache) Get(owner, repo string, prNumber int) ([]byte, bool) {
	return c.cache.Get(c.Key(owner, repo, prNumber))
}

// Set caches approval status.
func (c *ApprovalCache) Set(owner, repo string, prNumber int, value []byte) {
	c.cache.Set(c.Key(owner, repo, prNumber), value, c.ttl)
}

// CIStatusCache caches CI status for commits.
type CIStatusCache struct {
	cache *Cache
	ttl   time.Duration
}

// NewCIStatusCache creates a new CI status cache with the given TTL.
func NewCIStatusCache(ttl time.Duration) *CIStatusCache {
	return &CIStatusCache{
		cache: New(),
		ttl:   ttl,
	}
}

// Key generates a cache key for a CI status.
func (c *CIStatusCache) Key(owner, repo, commitSHA string) string {
	return Key("ci", owner, repo, commitSHA)
}

// Get returns cached CI status.
func (c *CIStatusCache) Get(owner, repo, commitSHA string) ([]byte, bool) {
	return c.cache.Get(c.Key(owner, repo, commitSHA))
}

// Set caches CI status.
func (c *CIStatusCache) Set(owner, repo, commitSHA string, value []byte) {
	c.cache.Set(c.Key(owner, repo, commitSHA), value, c.ttl)
}
