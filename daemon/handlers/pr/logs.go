package pr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/db"
	"asika/common/models"
)

func GetLogs(c *gin.Context) {
	level := c.Query("level")
	category := c.Query("category")
	actor := c.Query("actor")
	repoGroup := c.Query("repo_group")
	prNumberStr := c.Query("pr_number")
	action := c.Query("action")
	since := c.Query("since")
	limit := 0
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	var prNumber int
	if prNumberStr != "" {
		fmt.Sscanf(prNumberStr, "%d", &prNumber)
	}

	var sinceTime time.Time
	if since != "" {
		if d, err := time.ParseDuration(since); err == nil {
			sinceTime = time.Now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, since); err == nil {
			sinceTime = t
		}
	}

	logs := make([]models.AuditLog, 0)

	primaryFilter := ""
	primaryPrefix := ""
	if prNumber > 0 && repoGroup != "" {
		primaryFilter = "pr"
		primaryPrefix = fmt.Sprintf("pr:%s:%d:", repoGroup, prNumber)
	} else if actor != "" {
		primaryFilter = "actor"
		primaryPrefix = "actor:" + actor + ":"
	} else if repoGroup != "" {
		primaryFilter = "repo_group"
		primaryPrefix = "repo_group:" + repoGroup + ":"
	} else if action != "" {
		primaryFilter = "action"
		primaryPrefix = "action:" + action + ":"
	} else if category != "" {
		primaryFilter = "category"
		primaryPrefix = "category:" + category + ":"
	}

	if primaryPrefix != "" {
		err := db.ForEachPrefix(db.BucketAuditLogIndex, db.BucketLogs, primaryPrefix, func(idxKey, value []byte) error {
			var log models.AuditLog
			if err := json.Unmarshal(value, &log); err != nil {
				return nil
			}
			if level != "" && log.Level != level {
				return nil
			}
			if category != "" && primaryFilter != "category" && log.Category != category {
				return nil
			}
			if actor != "" && primaryFilter != "actor" && log.Actor != actor {
				return nil
			}
			if repoGroup != "" && primaryFilter != "repo_group" && primaryFilter != "pr" && log.RepoGroup != repoGroup {
				return nil
			}
			if prNumber > 0 && primaryFilter != "pr" && log.PRNumber != prNumber {
				return nil
			}
			if action != "" && primaryFilter != "action" && log.Action != action {
				return nil
			}
			if !sinceTime.IsZero() && log.Timestamp.Before(sinceTime) {
				return nil
			}
			logs = append(logs, log)
			if limit > 0 && len(logs) >= limit {
				return errStopLogs
			}
			return nil
		})
		if err != nil && err != errStopLogs {
			c.JSON(http.StatusOK, logs)
			return
		}
	} else {
		err := db.ForEach(db.BucketLogs, func(key, value []byte) error {
			var log models.AuditLog
			if err := json.Unmarshal(value, &log); err != nil {
				return nil
			}
			if level != "" && log.Level != level {
				return nil
			}
			if category != "" && log.Category != category {
				return nil
			}
			if actor != "" && log.Actor != actor {
				return nil
			}
			if repoGroup != "" && log.RepoGroup != repoGroup {
				return nil
			}
			if prNumber > 0 && log.PRNumber != prNumber {
				return nil
			}
			if action != "" && log.Action != action {
				return nil
			}
			if !sinceTime.IsZero() && log.Timestamp.Before(sinceTime) {
				return nil
			}
			logs = append(logs, log)
			if limit > 0 && len(logs) >= limit {
				return errStopLogs
			}
			return nil
		})
		if err != nil && err != errStopLogs {
			c.JSON(http.StatusOK, logs)
			return
		}
	}

	c.JSON(http.StatusOK, logs)
}

var errStopLogs = fmt.Errorf("stop logs")

func ExportLogs(c *gin.Context) {
	format := c.Query("format")
	if format == "" {
		format = "json"
	}
	level := c.Query("level")
	category := c.Query("category")
	actor := c.Query("actor")
	repoGroup := c.Query("repo_group")
	action := c.Query("action")
	since := c.Query("since")

	var sinceTime time.Time
	if since != "" {
		if d, err := time.ParseDuration(since); err == nil {
			sinceTime = time.Now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, since); err == nil {
			sinceTime = t
		}
	}

	logs := make([]models.AuditLog, 0)
	err := db.ForEach(db.BucketLogs, func(key, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		if level != "" && log.Level != level {
			return nil
		}
		if category != "" && log.Category != category {
			return nil
		}
		if actor != "" && log.Actor != actor {
			return nil
		}
		if repoGroup != "" && log.RepoGroup != repoGroup {
			return nil
		}
		if action != "" && log.Action != action {
			return nil
		}
		if !sinceTime.IsZero() && log.Timestamp.Before(sinceTime) {
			return nil
		}
		logs = append(logs, log)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read logs"})
		return
	}

	switch format {
	case "json":
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", "attachment; filename=asika-audit-logs.json")
		c.JSON(http.StatusOK, logs)
	case "csv":
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=asika-audit-logs.csv")
		c.String(http.StatusOK, "timestamp,level,category,actor,repo_group,action,message\n")
		for _, l := range logs {
			c.String(http.StatusOK, "%s,%s,%s,%s,%s,%s,\"%s\"\n",
				l.Timestamp.Format(time.RFC3339), l.Level, l.Category, l.Actor, l.RepoGroup, l.Action, strings.ReplaceAll(l.Message, "\"", "\"\""))
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported format: " + format})
	}
}
