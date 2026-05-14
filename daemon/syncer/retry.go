package syncer

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"asika/common/gitutil"
	"asika/common/models"
)

// pushRef pushes a specific ref (branch or tag) to a target remote with retry.
func (s *Syncer) pushRef(gitRepo *git.Repository, remoteName, refSpec, token, targetName string) error {
	var lastErr error
	for attempt := 0; attempt < syncMaxRetries; attempt++ {
		if attempt > 0 {
			delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			time.Sleep(delay)
		}
		opts := &git.PushOptions{
			RefSpecs: []gogitconfig.RefSpec{gogitconfig.RefSpec(refSpec)},
			Force:    true,
		}
		if token != "" {
			opts.Auth = &http.BasicAuth{Username: "git", Password: token}
		}
		if err := gitRepo.Push(opts); err != nil {
			lastErr = err
			if isTransientError(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("push %s to %s failed after %d retries: %w", refSpec, targetName, syncMaxRetries, lastErr)
}

// cherryPickWithRetry attempts cherry-pick with exponential backoff on conflict.
func (s *Syncer) cherryPickWithRetry(gitRepo *git.Repository, commitSHA string, pr *models.PRRecord, branch, sourceToken string) error {
	var lastErr error
	for attempt := 0; attempt < syncMaxRetries; attempt++ {
		if attempt > 0 {
			delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			slog.Info("retrying cherry-pick", "attempt", attempt+1, "max", syncMaxRetries, "delay", delay, "commit", commitSHA)
			time.Sleep(delay)
			if err := gitutil.FetchRemote(gitRepo, "origin", sourceToken); err != nil {
				slog.Warn("fetch before retry failed", "error", err)
			}
		}
		if err := gitutil.CherryPick(gitRepo, commitSHA); err != nil {
			lastErr = err
			if isConflictError(err) {
				slog.Warn("cherry-pick conflict detected, will retry", "attempt", attempt+1, "commit", commitSHA)
				continue
			}
			return err
		}
		return nil
	}
	slog.Error("cherry-pick failed after all retries", "commit", commitSHA, "attempts", syncMaxRetries)
	return fmt.Errorf("cherry-pick failed after %d retries: %w", syncMaxRetries, lastErr)
}

// pushWithRetry attempts push with exponential backoff on transient errors.
// Uses force push to ensure target platform state matches source.
func (s *Syncer) pushWithRetry(gitRepo *git.Repository, remoteName, branch, token, targetName string, pr *models.PRRecord) error {
	var lastErr error
	for attempt := 0; attempt < syncMaxRetries; attempt++ {
		if attempt > 0 {
			delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			slog.Info("retrying push", "attempt", attempt+1, "max", syncMaxRetries, "delay", delay, "target", targetName)
			time.Sleep(delay)
		}
		if err := gitutil.Push(gitRepo, remoteName, branch, token); err != nil {
			lastErr = err
			if isTransientError(err) {
				slog.Warn("transient push error, retrying", "attempt", attempt+1, "target", targetName, "error", err)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("push to %s failed after %d retries: %w", targetName, syncMaxRetries, lastErr)
}

// isConflictError checks if an error is a merge/push conflict.
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "conflict") ||
		strings.Contains(msg, "merge conflict") ||
		strings.Contains(msg, "cherry-pick conflict") ||
		strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "rejected") ||
		strings.Contains(msg, "failed to push")
}

// isTransientError checks if an error is likely temporary.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "temporary failure") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "no such host")
}
