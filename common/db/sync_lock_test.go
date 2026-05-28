package db

import (
	"testing"
	"time"
)

func TestAcquireSyncLock(t *testing.T) {
	initTestDB(t)

	acquired, err := AcquireSyncLock("frontend", "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("AcquireSyncLock failed: %v", err)
	}
	if !acquired {
		t.Error("expected lock to be acquired")
	}
}

func TestAcquireSyncLock_AlreadyHeld(t *testing.T) {
	initTestDB(t)

	acquired1, _ := AcquireSyncLock("frontend", "holder-1", 30*time.Second)
	if !acquired1 {
		t.Fatal("first acquisition should succeed")
	}

	acquired2, _ := AcquireSyncLock("frontend", "holder-2", 30*time.Second)
	if acquired2 {
		t.Error("second acquisition by different holder should fail")
	}
}

func TestAcquireSyncLock_SameHolder(t *testing.T) {
	initTestDB(t)

	acquired1, _ := AcquireSyncLock("frontend", "holder-1", 30*time.Second)
	if !acquired1 {
		t.Fatal("first acquisition should succeed")
	}

	acquired2, _ := AcquireSyncLock("frontend", "holder-1", 30*time.Second)
	if !acquired2 {
		t.Error("re-acquisition by same holder should succeed")
	}
}

func TestAcquireSyncLock_Expired(t *testing.T) {
	initTestDB(t)

	acquired1, _ := AcquireSyncLock("frontend", "holder-1", 1*time.Millisecond)
	if !acquired1 {
		t.Fatal("first acquisition should succeed")
	}

	time.Sleep(10 * time.Millisecond)

	acquired2, _ := AcquireSyncLock("frontend", "holder-2", 30*time.Second)
	if !acquired2 {
		t.Error("acquisition after expiry should succeed")
	}
}

func TestReleaseSyncLock(t *testing.T) {
	initTestDB(t)

	AcquireSyncLock("frontend", "holder-1", 30*time.Second)

	err := ReleaseSyncLock("frontend", "holder-1")
	if err != nil {
		t.Fatalf("ReleaseSyncLock failed: %v", err)
	}

	acquired, _ := AcquireSyncLock("frontend", "holder-2", 30*time.Second)
	if !acquired {
		t.Error("lock should be acquirable after release")
	}
}

func TestReleaseSyncLock_WrongHolder(t *testing.T) {
	initTestDB(t)

	AcquireSyncLock("frontend", "holder-1", 30*time.Second)

	err := ReleaseSyncLock("frontend", "holder-2")
	if err != nil {
		t.Fatalf("ReleaseSyncLock failed: %v", err)
	}

	acquired, _ := AcquireSyncLock("frontend", "holder-3", 30*time.Second)
	if acquired {
		t.Error("lock should still be held by holder-1")
	}
}

func TestReleaseSyncLock_NotHeld(t *testing.T) {
	initTestDB(t)

	err := ReleaseSyncLock("frontend", "holder-1")
	if err != nil {
		t.Fatalf("ReleaseSyncLock on non-held lock should not error: %v", err)
	}
}
