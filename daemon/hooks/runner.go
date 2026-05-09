package hooks

import (
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

	cmd := exec.Command(hookScript)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("GIT_DIR=%s", gitDir),
		fmt.Sprintf("OLD_REV=%s", oldRev),
		fmt.Sprintf("NEW_REV=%s", newRev),
		fmt.Sprintf("REF_NAME=%s", refName),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set timeout
	timer := time.AfterFunc(30*time.Second, func() {
		cmd.Process.Kill()
	})
	defer timer.Stop()

	if err := cmd.Run(); err != nil {
		slog.Warn("hook failed", "hook", hookName, "error", err)
		return nil
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
