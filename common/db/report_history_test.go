package db

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestPutReportHistory(t *testing.T) {
	initTestDB(t)
	defer Close()

	entry := ReportHistoryEntry{
		ID:        "test-report-1",
		Timestamp: time.Now(),
		Period:    7,
		Content:   "Test report content",
	}
	data, _ := json.Marshal(entry)
	err := PutReportHistory(entry.ID, data)
	if err != nil {
		t.Fatalf("PutReportHistory failed: %v", err)
	}
}

func TestListReportHistory(t *testing.T) {
	initTestDB(t)
	defer Close()

	now := time.Now()
	for i := 0; i < 5; i++ {
		entry := ReportHistoryEntry{
			ID:        fmt.Sprintf("report-%d", i),
			Timestamp: now.Add(time.Duration(i) * time.Hour),
			Period:    7,
			Content:   fmt.Sprintf("Report %d", i),
		}
		data, _ := json.Marshal(entry)
		PutReportHistory(entry.ID, data)
	}

	entries, err := ListReportHistory(10)
	if err != nil {
		t.Fatalf("ListReportHistory failed: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	entries, err = ListReportHistory(3)
	if err != nil {
		t.Fatalf("ListReportHistory with limit failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries with limit, got %d", len(entries))
	}
}

func TestListReportHistory_Empty(t *testing.T) {
	initTestDB(t)
	defer Close()

	entries, err := ListReportHistory(10)
	if err != nil {
		t.Fatalf("ListReportHistory failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}
