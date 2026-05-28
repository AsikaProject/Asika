package db

import (
	"encoding/json"
	"testing"
	"time"

	"asika/common/models"
)

func TestAppendAuditLog(t *testing.T) {
	initTestDB(t)

	err := AppendAuditLog("info", "PR approved", map[string]interface{}{
		"actor":      "alice",
		"repo_group": "frontend",
		"pr_number":  123,
	})
	if err != nil {
		t.Fatalf("AppendAuditLog failed: %v", err)
	}

	var logs []models.AuditLog
	err = ForEach(BucketLogs, func(key, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		logs = append(logs, log)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 audit log, got %d", len(logs))
	}
	if logs[0].Message != "PR approved" {
		t.Errorf("expected message 'PR approved', got %q", logs[0].Message)
	}
}

func TestAppendAuditLogEx(t *testing.T) {
	initTestDB(t)

	entry := models.AuditLog{
		Level:     "warn",
		Message:   "Config changed",
		Actor:     "admin",
		RepoGroup: "backend",
		Action:    "update",
		Category:  "config",
	}

	err := AppendAuditLogEx(entry)
	if err != nil {
		t.Fatalf("AppendAuditLogEx failed: %v", err)
	}

	var logs []models.AuditLog
	err = ForEach(BucketLogs, func(key, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		logs = append(logs, log)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 audit log, got %d", len(logs))
	}
	if logs[0].Actor != "admin" {
		t.Errorf("expected actor 'admin', got %q", logs[0].Actor)
	}
}

func TestAppendAuditLogEx_WithTimestamp(t *testing.T) {
	initTestDB(t)

	customTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	entry := models.AuditLog{
		Timestamp: customTime,
		Level:     "info",
		Message:   "Custom timestamp",
	}

	err := AppendAuditLogEx(entry)
	if err != nil {
		t.Fatalf("AppendAuditLogEx failed: %v", err)
	}

	var logs []models.AuditLog
	ForEach(BucketLogs, func(key, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		logs = append(logs, log)
		return nil
	})

	if len(logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(logs))
	}
	if !logs[0].Timestamp.Equal(customTime) {
		t.Errorf("expected timestamp %v, got %v", customTime, logs[0].Timestamp)
	}
}
