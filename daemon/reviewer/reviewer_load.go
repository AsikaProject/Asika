package reviewer

import (
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"asika/common/db"
	"asika/common/models"
)

// GetReviewerLoad returns the load info for a reviewer
func GetReviewerLoad(username string) (*models.ReviewerLoad, error) {
	// Check if database is initialized
	if !db.IsInitialized() {
		return &models.ReviewerLoad{
			Username:     username,
			PendingCount: 0,
			LastReviewAt: time.Time{},
		}, nil
	}

	data, err := db.Get(db.BucketReviewerLoad, username)
	if err != nil || data == nil {
		return &models.ReviewerLoad{
			Username:     username,
			PendingCount: 0,
			LastReviewAt: time.Time{},
		}, nil
	}
	var load models.ReviewerLoad
	if err := json.Unmarshal(data, &load); err != nil {
		return nil, err
	}
	return &load, nil
}

// GetAllReviewerLoads returns load info for all reviewers
func GetAllReviewerLoads() ([]models.ReviewerLoad, error) {
	// Check if database is initialized
	if !db.IsInitialized() {
		return nil, nil
	}

	var loads []models.ReviewerLoad
	err := db.ForEach(db.BucketReviewerLoad, func(key, value []byte) error {
		var load models.ReviewerLoad
		if err := json.Unmarshal(value, &load); err != nil {
			return nil
		}
		loads = append(loads, load)
		return nil
	})
	return loads, err
}

// UpdateReviewerLoad updates a reviewer's load info
func UpdateReviewerLoad(username string, pendingDelta int, updateLastReview bool) error {
	// Check if database is initialized
	if !db.IsInitialized() {
		return nil
	}

	load, err := GetReviewerLoad(username)
	if err != nil {
		return err
	}

	load.PendingCount += pendingDelta
	if load.PendingCount < 0 {
		load.PendingCount = 0
	}
	if updateLastReview {
		load.LastReviewAt = time.Now()
	}

	data, err := json.Marshal(load)
	if err != nil {
		return err
	}
	return db.Put(db.BucketReviewerLoad, username, data)
}

// IncrementPendingLoad increments the pending count for reviewers
func IncrementPendingLoad(reviewers []string) {
	for _, username := range reviewers {
		if err := UpdateReviewerLoad(username, 1, false); err != nil {
			slog.Error("failed to increment reviewer load", "username", username, "error", err)
		}
	}
}

// DecrementPendingLoad decrements the pending count and updates last review time
func DecrementPendingLoad(username string) {
	if err := UpdateReviewerLoad(username, -1, true); err != nil {
		slog.Error("failed to decrement reviewer load", "username", username, "error", err)
	}
}

// FilterAndSortByLoad filters out inactive reviewers and sorts by load
func FilterAndSortByLoad(candidates []string, activeDays int, maxReviewers int) []string {
	if len(candidates) == 0 {
		return candidates
	}

	cfg := models.ReviewerLoadConfig{
		Enabled:           true,
		MaxReviewersPerPR: maxReviewers,
		ActiveDays:        activeDays,
	}

	return filterAndSortByLoadInternal(candidates, cfg)
}

func filterAndSortByLoadInternal(candidates []string, cfg models.ReviewerLoadConfig) []string {
	if !cfg.Enabled || len(candidates) == 0 {
		return candidates
	}

	activeDays := cfg.ActiveDays
	if activeDays <= 0 {
		activeDays = 14
	}
	cutoff := time.Now().AddDate(0, 0, -activeDays)

	type reviewerInfo struct {
		username     string
		pendingCount int
		lastReviewAt time.Time
		isActive     bool
	}

	var reviewers []reviewerInfo
	for _, username := range candidates {
		load, err := GetReviewerLoad(username)
		if err != nil {
			slog.Warn("failed to get reviewer load, treating as active", "username", username, "error", err)
			reviewers = append(reviewers, reviewerInfo{
				username:     username,
				pendingCount: 0,
				lastReviewAt: time.Time{},
				isActive:     true,
			})
			continue
		}

		isActive := !load.LastReviewAt.IsZero() && load.LastReviewAt.After(cutoff)
		reviewers = append(reviewers, reviewerInfo{
			username:     username,
			pendingCount: load.PendingCount,
			lastReviewAt: load.LastReviewAt,
			isActive:     isActive,
		})
	}

	// Filter: keep only active reviewers
	var activeReviewers []reviewerInfo
	for _, r := range reviewers {
		if r.isActive {
			activeReviewers = append(activeReviewers, r)
		}
	}

	// Fallback: if no active reviewers, use all candidates
	if len(activeReviewers) == 0 {
		slog.Info("no active reviewers found, falling back to all candidates")
		activeReviewers = reviewers
	}

	// Sort by pending count (ascending)
	sort.Slice(activeReviewers, func(i, j int) bool {
		return activeReviewers[i].pendingCount < activeReviewers[j].pendingCount
	})

	// Take top N
	maxReviewers := cfg.MaxReviewersPerPR
	if maxReviewers <= 0 {
		maxReviewers = 2
	}
	if len(activeReviewers) > maxReviewers {
		activeReviewers = activeReviewers[:maxReviewers]
	}

	result := make([]string, len(activeReviewers))
	for i, r := range activeReviewers {
		result[i] = r.username
	}
	return result
}
