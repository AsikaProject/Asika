package cache

import (
	"testing"
	"time"
)

func TestCacheGetSet(t *testing.T) {
	c := New()

	c.Set("key1", []byte("value1"), time.Minute)

	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if string(val) != "value1" {
		t.Errorf("got %q, want %q", string(val), "value1")
	}

	_, ok = c.Get("nonexistent")
	if ok {
		t.Error("expected nonexistent key to not exist")
	}
}

func TestCacheExpiration(t *testing.T) {
	c := New()

	c.Set("key1", []byte("value1"), 50*time.Millisecond)

	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist before expiration")
	}
	if string(val) != "value1" {
		t.Errorf("got %q, want %q", string(val), "value1")
	}

	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get("key1")
	if ok {
		t.Error("expected key1 to be expired")
	}
}

func TestCacheDelete(t *testing.T) {
	c := New()

	c.Set("key1", []byte("value1"), time.Minute)
	c.Delete("key1")

	_, ok := c.Get("key1")
	if ok {
		t.Error("expected key1 to be deleted")
	}
}

func TestCacheCleanup(t *testing.T) {
	c := New()

	c.Set("key1", []byte("value1"), 50*time.Millisecond)
	c.Set("key2", []byte("value2"), time.Minute)

	time.Sleep(100 * time.Millisecond)
	c.Cleanup()

	_, ok := c.Get("key1")
	if ok {
		t.Error("expected key1 to be cleaned up")
	}

	val, ok := c.Get("key2")
	if !ok {
		t.Fatal("expected key2 to still exist")
	}
	if string(val) != "value2" {
		t.Errorf("got %q, want %q", string(val), "value2")
	}
}

func TestKey(t *testing.T) {
	k1 := Key("a", "b", "c")
	k2 := Key("a", "b", "c")
	k3 := Key("a", "b", "d")

	if k1 != k2 {
		t.Error("same inputs should produce same key")
	}
	if k1 == k3 {
		t.Error("different inputs should produce different keys")
	}
}

func TestApprovalCache(t *testing.T) {
	c := NewApprovalCache(time.Minute)

	c.Set("owner", "repo", 42, []byte(`{"approvers":["alice"]}`))

	val, ok := c.Get("owner", "repo", 42)
	if !ok {
		t.Fatal("expected approval to be cached")
	}
	if string(val) != `{"approvers":["alice"]}` {
		t.Errorf("got %q", string(val))
	}

	_, ok = c.Get("owner", "repo", 99)
	if ok {
		t.Error("expected miss for different PR")
	}
}

func TestCIStatusCache(t *testing.T) {
	c := NewCIStatusCache(time.Minute)

	c.Set("owner", "repo", "abc123", []byte("success"))

	val, ok := c.Get("owner", "repo", "abc123")
	if !ok {
		t.Fatal("expected CI status to be cached")
	}
	if string(val) != "success" {
		t.Errorf("got %q, want %q", string(val), "success")
	}

	_, ok = c.Get("owner", "repo", "def456")
	if ok {
		t.Error("expected miss for different commit")
	}
}
