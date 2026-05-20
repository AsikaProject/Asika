package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner executes git hooks
type Runner struct {
	hookPath string
}

// NewRunner creates a new hook runner
func NewRunner(hookPath string) *Runner {
	return &Runner{
		hookPath: hookPath,
	}
}

// Run executes a hook script
func (r *Runner) Run(hookName, gitDir, oldRev, newRev, refName string) error {
	if r.hookPath == "" {
		return nil
	}

	if !isValidHookName(hookName) {
		slog.Warn("invalid hook name, skipping", "hook", hookName)
		return nil
	}

	hookScript := filepath.Join(r.hookPath, hookName)
	if _, err := os.Stat(hookScript); os.IsNotExist(err) {
		slog.Info("hook script not found, skipping", "hook", hookName)
		return nil
	}

	slog.Info("running hook", "hook", hookName, "script", hookScript)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, hookScript)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("GIT_DIR=%s", gitDir),
		fmt.Sprintf("OLD_REV=%s", oldRev),
		fmt.Sprintf("NEW_REV=%s", newRev),
		fmt.Sprintf("REF_NAME=%s", refName),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			slog.Error("hook timed out", "hook", hookName, "timeout", "30s")
			return fmt.Errorf("hook %s timed out after 30s", hookName)
		}
		slog.Warn("hook failed", "hook", hookName, "error", err)
		return fmt.Errorf("hook %s failed: %w", hookName, err)
	}

	return nil
}

// ValidateHookPath checks that the hook path is absolute and contains no .. components.
func ValidateHookPath(hookPath string) error {
	if hookPath == "" {
		return nil
	}
	if !filepath.IsAbs(hookPath) {
		return fmt.Errorf("hook path must be an absolute path: %s", hookPath)
	}
	if strings.Contains(hookPath, "..") {
		return fmt.Errorf("hook path must not contain .. components: %s", hookPath)
	}
	return nil
}

func isValidHookName(name string) bool {
	if name == "" {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
