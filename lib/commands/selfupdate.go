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
downloads the matching binary from GitHub Releases, verifies its SHA256
checksum, replaces the current binary, and exits. The process manager
(systemd, launchd, etc.) will restart the service automatically.

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

	if currentVersion != "dev" && !isNewer(latestVersion, currentVersion) {
		fmt.Println("Already up to date.")
		if checkOnly {
			return
		}
		os.Exit(0)
	}

	if currentVersion == "dev" {
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

	assetName := fmt.Sprintf("%s-%s-%s", binaryName, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}
	downloadURL, checksumURL := findAssets(release, assetName)
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "Error: no binary asset found for %s\n", assetName)
		os.Exit(1)
	}

	if dryRun {
		fmt.Printf("\nWould download: %s\n", assetName)
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

	binaryPath := filepath.Join(tmpDir, assetName)
	fmt.Printf("\nDownloading %s...\n", assetName)
	if err := downloadFile(downloadURL, binaryPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading binary: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Download complete.")

	if checksumURL != "" {
		fmt.Println("Verifying checksum...")
		checksumPath := filepath.Join(tmpDir, assetName+".sha256sum")
		if err := downloadFile(checksumURL, checksumPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading checksum: %v\n", err)
			os.Exit(1)
		}
		if err := verifyChecksum(binaryPath, checksumPath); err != nil {
			fmt.Fprintf(os.Stderr, "Checksum verification failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Checksum OK.")
	} else {
		fmt.Printf("Warning: no .sha256sum found for %s, skipping checksum verification\n", assetName)
	}

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
	if err := copyFile(binaryPath, currentPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error installing binary: %v\n", err)
		fmt.Fprintf(os.Stderr, "Attempting to restore backup...\n")
		os.Rename(backupPath, currentPath)
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

func findAssets(release *github.RepositoryRelease, assetName string) (binaryURL, checksumURL string) {
	for _, asset := range release.Assets {
		name := asset.GetName()
		if name == assetName {
			binaryURL = asset.GetBrowserDownloadURL()
		}
		if name == assetName+".sha256sum" {
			checksumURL = asset.GetBrowserDownloadURL()
		}
	}
	if binaryURL != "" {
		if !strings.HasPrefix(binaryURL, "https://github.com/") && !strings.HasPrefix(binaryURL, "https://objects.githubusercontent.com/") {
			binaryURL = ""
		}
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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

func isNewer(latest, current string) bool {
	return latest != current
}
