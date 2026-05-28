package db

import (
	"encoding/json"
	"testing"
)

func TestPutConfigSnapshot(t *testing.T) {
	initTestDB(t)

	data := []byte(`{"listen":":8080"}`)
	err := PutConfigSnapshot(1, data)
	if err != nil {
		t.Fatalf("PutConfigSnapshot failed: %v", err)
	}
}

func TestGetConfigSnapshot(t *testing.T) {
	initTestDB(t)

	data := []byte(`{"listen":":8080"}`)
	PutConfigSnapshot(5, data)

	got, err := GetConfigSnapshot(5)
	if err != nil {
		t.Fatalf("GetConfigSnapshot failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("GetConfigSnapshot = %q, want %q", string(got), string(data))
	}
}

func TestGetConfigSnapshot_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetConfigSnapshot(999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListConfigSnapshots(t *testing.T) {
	initTestDB(t)

	for i := 1; i <= 5; i++ {
		data, _ := json.Marshal(map[string]int{"version": i})
		PutConfigSnapshot(i, data)
	}

	entries, err := ListConfigSnapshots(10)
	if err != nil {
		t.Fatalf("ListConfigSnapshots failed: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	if entries[0].Version != 5 {
		t.Errorf("expected first entry version 5 (newest), got %d", entries[0].Version)
	}
}

func TestListConfigSnapshots_WithLimit(t *testing.T) {
	initTestDB(t)

	for i := 1; i <= 10; i++ {
		PutConfigSnapshot(i, []byte(`{}`))
	}

	entries, err := ListConfigSnapshots(3)
	if err != nil {
		t.Fatalf("ListConfigSnapshots failed: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries with limit, got %d", len(entries))
	}
}

func TestListConfigSnapshots_Empty(t *testing.T) {
	initTestDB(t)

	entries, err := ListConfigSnapshots(10)
	if err != nil {
		t.Fatalf("ListConfigSnapshots failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
