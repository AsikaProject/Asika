package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v69/github"
	"golang.org/x/oauth2"

	"asika/common/config"
	"asika/common/version"
)

var httpUpdateClient = &http.Client{Timeout: 60 * time.Second}

const githubOwner = "AsikaProject"
const githubRepo = "asika"

var updateProgressMap = make(map[string]chan UpdateProgress)

type UpdateProgress struct {
	Status   string `json:"status"`   // "downloading", "verifying", "installing", "done", "error"
	Progress int    `json:"progress"` // 0-100
	Message  string `json:"message"`
	Error    string `json:"error,omitempty"`
}

// isDevVersion returns true if the version is a development build (contains a hyphen suffix like "20260511DEV-b92c70f").
// Release versions are pure date strings like "20260511" or "20260511HF".
func isDevVersion(v string) bool {
	return strings.Contains(v, "-")
}

// CheckForUpdate checks GitHub for a newer version.
// Dev builds (version contains "-") skip the check and report as up-to-date.
func CheckForUpdate(c *gin.Context) {
	if isDevVersion(version.Version) {
		c.JSON(http.StatusOK, gin.H{
			"current":    version.Version,
			"latest":     version.Version,
			"upgradable": false,
			"dev":        true,
		})
		return
	}

	cfg := config.Current()
	var httpClient *http.Client
	if cfg != nil && cfg.Tokens.GitHub != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Tokens.GitHub})
		httpClient = oauth2.NewClient(context.Background(), ts)
	} else {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	client := github.NewClient(httpClient)

	release, _, err := client.Repositories.GetLatestRelease(context.Background(), githubOwner, githubRepo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to check releases: %v", err)})
		return
	}

	latestVersion := strings.TrimPrefix(release.GetTagName(), "v")
	upgradable := latestVersion != "" && latestVersion != version.Version

	c.JSON(http.StatusOK, gin.H{
		"current":      version.Version,
		"latest":       latestVersion,
		"upgradable":   upgradable,
		"url":          release.GetHTMLURL(),
		"published_at": release.GetPublishedAt(),
	})
}

// PerformWebUpdate performs the update via SSE progress stream.
func PerformWebUpdate(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil || !cfg.Server.EnableWebUpdate {
		c.JSON(http.StatusForbidden, gin.H{"error": "web update is disabled"})
		return
	}
	if version.Enabled != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "self-update not available on this platform"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	sendEvent := func(event string, data string) {
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	// Create GitHub client with optional authentication
	var httpClient *http.Client
	if cfg.Tokens.GitHub != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Tokens.GitHub})
		httpClient = oauth2.NewClient(context.Background(), ts)
	} else {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	client := github.NewClient(httpClient)

	// Get latest release
	release, _, err := client.Repositories.GetLatestRelease(context.Background(), githubOwner, githubRepo)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"failed to fetch release: %s"}`, err.Error()))
		return
	}

	binaryName := "asikad"
	assetName := fmt.Sprintf("%s-%s-%s", binaryName, runtime.GOOS, runtime.GOARCH)

	downloadURL := ""
	checksumURL := ""
	for _, asset := range release.Assets {
		if asset.GetName() == assetName {
			downloadURL = asset.GetBrowserDownloadURL()
		}
		if asset.GetName() == assetName+".sha256sum" {
			checksumURL = asset.GetBrowserDownloadURL()
		}
	}
	if downloadURL == "" {
		sendEvent("error", fmt.Sprintf(`{"error":"no asset found for %s"}`, assetName))
		return
	}
	if !isValidGitHubDownloadURL(downloadURL) {
		sendEvent("error", fmt.Sprintf(`{"error":"invalid download URL: %s"}`, downloadURL))
		return
	}

	sendEvent("progress", `{"status":"downloading","progress":0,"message":"Starting download..."}`)

	tmpDir, err := os.MkdirTemp("", "asika_update")
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"failed to create temp dir: %s"}`, err.Error()))
		return
	}
	defer os.RemoveAll(tmpDir)

	binaryPath := filepath.Join(tmpDir, assetName)
	if err := downloadWithProgress(downloadURL, binaryPath, sendEvent); err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"download failed: %s"}`, err.Error()))
		return
	}

	if checksumURL == "" {
		sendEvent("error", `{"error":"checksum file not available, refusing to install unverified binary"}`)
		return
	}

	sendEvent("progress", `{"status":"verifying","progress":100,"message":"Verifying checksum..."}`)

	checksumPath := filepath.Join(tmpDir, assetName+".sha256sum")
	resp, err := httpUpdateClient.Get(checksumURL)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"failed to download checksum: %s"}`, err.Error()))
		return
	}
	f, err := os.Create(checksumPath)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		resp.Body.Close()
		return
	}
	written, err := io.Copy(f, resp.Body)
	f.Close()
	resp.Body.Close()
	if err != nil || written == 0 {
		sendEvent("error", `{"error":"failed to download checksum file"}`)
		return
	}

	if err := verifyWebChecksum(binaryPath, checksumPath); err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"checksum verification failed: %s"}`, err.Error()))
		return
	}

	sendEvent("progress", `{"status":"installing","progress":100,"message":"Installing update..."}`)

	currentPath, err := os.Executable()
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		return
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		return
	}

	backupPath := currentPath + ".old"
	if err := os.Rename(currentPath, backupPath); err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"backup failed: %s"}`, err.Error()))
		return
	}

	in, err := os.Open(binaryPath)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":"failed to open downloaded binary: %s"}`, err.Error()))
		return
	}
	out, err := os.Create(currentPath)
	if err != nil {
		in.Close()
		// Restore backup
		os.Rename(backupPath, currentPath)
		sendEvent("error", fmt.Sprintf(`{"error":"failed to create target binary: %s"}`, err.Error()))
		return
	}
	if _, err := io.Copy(out, in); err != nil {
		in.Close()
		out.Close()
		os.Rename(backupPath, currentPath)
		sendEvent("error", fmt.Sprintf(`{"error":"failed to write binary: %s"}`, err.Error()))
		return
	}
	in.Close()
	out.Close()
	if err := os.Chmod(currentPath, 0755); err != nil {
		os.Rename(backupPath, currentPath)
		sendEvent("error", fmt.Sprintf(`{"error":"failed to chmod binary: %s"}`, err.Error()))
		return
	}

	slog.Info("self-update", "version", release.GetTagName(), "from", "webui")
	sendEvent("done", `{"status":"done","progress":100,"message":"Update complete. Service restarting..."}`)

	go func() {
		os.Exit(0)
	}()
}

func downloadWithProgress(url, dest string, sendEvent func(string, string)) error {
	resp, err := httpUpdateClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			downloaded += int64(n)
			if total > 0 {
				pct := int(downloaded * 100 / total)
				sendEvent("progress", fmt.Sprintf(`{"status":"downloading","progress":%d,"message":"Downloading... %d%%"}`, pct, pct))
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}

func verifyWebChecksum(binaryPath, checksumPath string) error {
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	actual := hex.EncodeToString(hash[:])

	expected, err := parseSha256sumFile(checksumPath)
	if err != nil {
		return fmt.Errorf("cannot read checksum: %w", err)
	}

	if actual != expected {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}

func parseSha256sumFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("no valid checksum entry found")
}

func isValidGitHubDownloadURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") || strings.HasPrefix(url, "https://objects.githubusercontent.com/")
}
