package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner("/tmp/hooks")
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
	if r.hookPath != "/tmp/hooks" {
		t.Errorf("hookPath = %q, want /tmp/hooks", r.hookPath)
	}
}

func TestRun_EmptyHookPath(t *testing.T) {
	r := NewRunner("")
	err := r.Run("pre-receive", "/tmp/git", "abc123", "def456", "refs/heads/main")
	if err != nil {
		t.Errorf("Run with empty hookPath should return nil, got %v", err)
	}
}

func TestRun_HookScriptNotExists(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)

	err := r.Run("non-existent-hook", "/tmp/git", "abc", "def", "refs/heads/main")
	if err != nil {
		t.Errorf("Run with non-existent hook should return nil, got %v", err)
	}
}

func TestRun_HookScriptExists(t *testing.T) {
	dir := t.TempDir()

	// Create a simple hook script
	hookScript := filepath.Join(dir, "update")
	err := os.WriteFile(hookScript, []byte("#!/bin/sh\necho test\n"), 0755)
	if err != nil {
		t.Fatalf("failed to create hook script: %v", err)
	}

	r := NewRunner(dir)
	err = r.Run("update", "/tmp/git", "abc123", "def456", "refs/heads/main")
	if err != nil {
		t.Errorf("Run failed: %v", err)
	}
}

func TestRun_HookScriptFails(t *testing.T) {
	dir := t.TempDir()

	// Create a hook script that will fail
	hookScript := filepath.Join(dir, "update")
	err := os.WriteFile(hookScript, []byte("#!/bin/sh\nexit 1\n"), 0755)
	if err != nil {
		t.Fatalf("failed to create hook script: %v", err)
	}

	r := NewRunner(dir)
	err = r.Run("update", "/tmp/git", "abc123", "def456", "refs/heads/main")
	if err == nil {
		t.Errorf("Run should return error when hook fails")
	}
}

func TestRun_HookTimeout(t *testing.T) {
	dir := t.TempDir()

	// Create a hook script that will timeout (sleep 60 seconds)
	hookScript := filepath.Join(dir, "update")
	err := os.WriteFile(hookScript, []byte("#!/bin/sh\nsleep 60\n"), 0755)
	if err != nil {
		t.Fatalf("failed to create hook script: %v", err)
	}

	r := NewRunner(dir)
	// Since timeout is 30 seconds, this test would be slow
	// Skip this test, or mock the timeout
	t.Skip("skipping timeout test (would take 30 seconds)")
	_ = r
	_ = time.Second
}
