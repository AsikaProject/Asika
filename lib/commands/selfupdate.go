package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-github/v69/github"
	"github.com/spf13/cobra"

	"asika/common/archive"
	"asika/common/version"
)

const (
	githubOwner = "AsikaProject"
	githubRepo  = "asika"
)

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update asika/asikad to the latest version",
	Long: `Download and install the latest version of asika or asikad.

The command detects whether you are running asika (CLI) or asikad (daemon),
downloads the matching tarball from GitHub Releases, extracts the target
binary, verifies its SHA256 checksum, replaces the current binary, and exits.
The process manager (systemd, launchd, etc.) will restart the service
automatically.

Self-update is only available for release tarball installs. Package
(apt/brew/choco/docker) and manual go build installs must be upgraded
through their respective channels.

A backup of the old binary is saved as {binary}.old for easy rollback.`,
	Run: runSelfUpdate,
}

func init() {
	selfUpdateCmd.Flags().String("version", "", "Install a specific version tag")
	selfUpdateCmd.Flags().Bool("check", false, "Only check for new versions, do not download")
	selfUpdateCmd.Flags().Bool("dry-run", false, "Print what would be done without making changes")
	selfUpdateCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	selfUpdateCmd.Flags().Bool("rollback", false, "Rollback to the previous version (.old)")
	selfUpdateCmd.Flags().Bool("restart", false, "Restart in-place after update (standalone mode)")
	RootCmd.AddCommand(selfUpdateCmd)
}

func runSelfUpdate(cmd *cobra.Command, args []string) {
	rollback, _ := cmd.Flags().GetBool("rollback")
	if rollback {
		doRollback()
		return
	}

	if version.Channel != "release" {
		rejectChannel()
		return
	}

	checkOnly, _ := cmd.Flags().GetBool("check")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	skipConfirm, _ := cmd.Flags().GetBool("yes")
	specifiedVersion, _ := cmd.Flags().GetString("version")
	restart, _ := cmd.Flags().GetBool("restart")

	currentVersion := version.Version
	binaryName := filepath.Base(detectBinary())

	client := github.NewClient(nil)

	release, err := fetchRelease(client, specifiedVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching release: %v\n", err)
		os.Exit(1)
	}

	latestVersion := strings.TrimPrefix(release.GetTagName(), "v")

	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Printf("Latest version:   %s\n", latestVersion)

	if !version.IsUpgradeable(currentVersion, latestVersion) {
		fmt.Println("Already up to date.")
		if checkOnly {
			return
		}
		os.Exit(0)
	}

	if version.IsDevBuild(currentVersion) {
		// Dev builds are always upgradeable to a release; the check above
		// already ensured latest is not a dev build. No special prompt is
		// needed — the project rule allows dev → release to proceed
		// silently through the normal confirmation flow.
		fmt.Println("(running development build, proceeding with update)")
	}

	if checkOnly {
		fmt.Println("A new version is available!")
		return
	}

	fmt.Printf("Release date: %s\n", release.GetPublishedAt().Format("2006-01-02"))
	fmt.Printf("URL: %s\n", release.GetHTMLURL())

	if !skipConfirm {
		fmt.Print("\nProceed with update? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Update cancelled.")
			return
		}
	}

	tarballName := fmt.Sprintf("asika-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	checksumName := fmt.Sprintf("%s-%s-%s.sha256sum", binaryName, runtime.GOOS, runtime.GOARCH)
	downloadURL, checksumURL := findAssets(release, tarballName, checksumName)
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "Error: no tarball asset found for %s\n", tarballName)
		os.Exit(1)
	}
	if checksumURL == "" {
		fmt.Fprintf(os.Stderr, "Error: no checksum asset found for %s; refusing unverified install\n", checksumName)
		os.Exit(1)
	}

	extractName := binaryName
	if runtime.GOOS == "windows" {
		extractName += ".exe"
	}

	if dryRun {
		fmt.Printf("\nWould download: %s\n", tarballName)
		fmt.Printf("  Binary to extract: %s\n", extractName)
		fmt.Printf("  Checksum:         %s\n", checksumName)
		fmt.Printf("From: %s\n", downloadURL)
		fmt.Println("Dry run complete. No changes made.")
		return
	}

	tmpDir, err := os.MkdirTemp("", "asika_update")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, tarballName)
	fmt.Printf("\nDownloading %s...\n", tarballName)
	if err := downloadFile(downloadURL, tarballPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading tarball: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Download complete.")

	checksumPath := filepath.Join(tmpDir, checksumName)
	fmt.Println("Downloading checksum...")
	if err := downloadFile(checksumURL, checksumPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading checksum: %v\n", err)
		os.Exit(1)
	}

	extractedPath := filepath.Join(tmpDir, extractName)
	fmt.Printf("Extracting %s from tarball...\n", extractName)
	tgzFile, err := os.Open(tarballPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening tarball: %v\n", err)
		os.Exit(1)
	}
	if _, err := archive.ExtractFileByBaseName(tgzFile, extractName, extractedPath); err != nil {
		tgzFile.Close()
		fmt.Fprintf(os.Stderr, "Error extracting binary: %v\n", err)
		os.Exit(1)
	}
	tgzFile.Close()

	fmt.Println("Verifying checksum...")
	if err := verifyChecksum(extractedPath, checksumPath); err != nil {
		fmt.Fprintf(os.Stderr, "Checksum verification failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Checksum OK.")

	currentPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding current binary: %v\n", err)
		os.Exit(1)
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving binary path: %v\n", err)
		os.Exit(1)
	}

	backupPath := currentPath + ".old"

	fmt.Println("Backing up current binary to", backupPath)
	if err := os.Rename(currentPath, backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error backing up binary: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Installing new binary to", currentPath)
	if err := atomicInstall(extractedPath, currentPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error installing binary: %v\n", err)
		fmt.Fprintf(os.Stderr, "Attempting to restore backup...\n")
		if rerr := os.Rename(backupPath, currentPath); rerr != nil {
			fmt.Fprintf(os.Stderr, "CRITICAL: restore failed: %v\n", rerr)
			fmt.Fprintf(os.Stderr, "Manual recovery: rename %s to %s\n", backupPath, currentPath)
			os.Exit(2)
		}
		os.Exit(1)
	}

	if err := os.Chmod(currentPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not set executable permission: %v\n", err)
	}

	fmt.Println("Update complete!")
	fmt.Println("To rollback, run: asika self-update --rollback")

	if restart {
		fmt.Println("Restarting...")
		execSelf(currentPath)
	} else {
		fmt.Println("Exiting for process manager to restart...")
		os.Exit(0)
	}
}

// rejectChannel reports that self-update is unavailable for the current
// distribution channel and points the user at the right upgrade path.
func rejectChannel() {
	switch version.Channel {
	case "package":
		fmt.Fprintln(os.Stderr, "Self-update is disabled for package installs.")
		fmt.Fprintln(os.Stderr, "Upgrade via your package manager (apt/brew/choco) or pull the new Docker image.")
	case "manual":
		fmt.Fprintln(os.Stderr, "Self-update is disabled for manual builds.")
		fmt.Fprintln(os.Stderr, "Reinstall via 'bash build.sh build' or download a release tarball.")
	default:
		fmt.Fprintf(os.Stderr, "Self-update is disabled for channel %q.\n", version.Channel)
	}
	os.Exit(1)
}

func detectBinary() string {
	path, err := os.Executable()
	if err != nil {
		return "asika"
	}
	return path
}

func fetchRelease(client *github.Client, tag string) (*github.RepositoryRelease, error) {
	ctx := context.Background()
	if tag != "" {
		release, _, err := client.Repositories.GetReleaseByTag(ctx, githubOwner, githubRepo, tag)
		return release, err
	}
	release, _, err := client.Repositories.GetLatestRelease(ctx, githubOwner, githubRepo)
	return release, err
}

// findAssets locates the release tarball and matching checksum.
//
// Release tarballs are published under a unified "asika-<os>-<arch>.tar.gz"
// name regardless of which binary the user is upgrading (the archive contains
// both asika and asikad plus service files). Checksums are named after the
// target binary — "<binaryName>-<os>-<arch>.sha256sum" — because they are
// computed over the extracted bare binary rather than the tarball itself.
func findAssets(release *github.RepositoryRelease, tarballName, checksumName string) (tarballURL, checksumURL string) {
	for _, asset := range release.Assets {
		switch asset.GetName() {
		case tarballName:
			tarballURL = asset.GetBrowserDownloadURL()
		case checksumName:
			checksumURL = asset.GetBrowserDownloadURL()
		}
	}
	if tarballURL != "" && !archive.IsValidGitHubDownloadURL(tarballURL) {
		tarballURL = ""
	}
	if checksumURL != "" && !archive.IsValidGitHubDownloadURL(checksumURL) {
		checksumURL = ""
	}
	return
}

var downloadHTTPClient = &http.Client{Timeout: 60 * time.Second}

func downloadFile(url, dest string) error {
	resp, err := downloadHTTPClient.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
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
	_, err = io.Copy(f, resp.Body)
	return err
}

func verifyChecksum(binaryPath, checksumPath string) error {
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
		return fmt.Errorf("checksum mismatch\nexpected: %s\nactual:   %s", expected, actual)
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

// atomicInstall writes src to dst via a sibling temp file followed by an
// atomic rename and fsync. The intermediate file is removed on any failure.
func atomicInstall(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	tmpTarget := dst + ".new"
	out, err := os.OpenFile(tmpTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("create target: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("write target: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("sync target: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("close target: %w", err)
	}
	if err := os.Rename(tmpTarget, dst); err != nil {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("rename target: %w", err)
	}
	return nil
}

func execSelf(path string) {
	args := os.Args
	env := os.Environ()
	if err := syscall.Exec(path, args, env); err != nil {
		fmt.Fprintf(os.Stderr, "Error restarting: %v\n", err)
		os.Exit(1)
	}
}

func doRollback() {
	currentPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding current binary: %v\n", err)
		os.Exit(1)
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving binary path: %v\n", err)
		os.Exit(1)
	}

	backupPath := currentPath + ".old"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: no backup file found at %s\n", backupPath)
		os.Exit(1)
	}

	brokenPath := currentPath + ".broken"
	fmt.Println("Moving current binary to", brokenPath)
	if err := os.Rename(currentPath, brokenPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error moving current binary: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Restoring backup from", backupPath)
	if err := os.Rename(backupPath, currentPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error restoring backup: %v\n", err)
		os.Rename(brokenPath, currentPath)
		os.Exit(1)
	}

	if err := os.Chmod(currentPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not set executable permission: %v\n", err)
	}

	fmt.Println("Rollback complete. Please restart the service manually.")
}
