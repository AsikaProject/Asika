package gitutil

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// CloneOrOpen clones a repository or opens if it exists
func CloneOrOpen(workdir, url, token string) (*git.Repository, error) {
	if _, err := os.Stat(workdir); os.IsNotExist(err) {
		return cloneRepo(workdir, url, token)
	}

	repo, err := git.PlainOpen(workdir)
	if err != nil {
		os.RemoveAll(workdir)
		return cloneRepo(workdir, url, token)
	}

	return repo, nil
}

// cloneRepo clones a repository
func cloneRepo(workdir, url, token string) (*git.Repository, error) {
	opts := &git.CloneOptions{
		URL: url,
	}
	if token != "" {
		opts.Auth = &http.BasicAuth{
			Username: "git",
			Password: token,
		}
	}

	return git.PlainClone(workdir, false, opts)
}

// CherryPick performs a cherry-pick of a commit onto the current branch.
// Strategy: Hard reset worktree to source commit, stage changes, then commit on top of HEAD.
func CherryPick(repo *git.Repository, commitSHA string) error {
	commitHash := plumbing.NewHash(commitSHA)

	// Get the source commit
	sourceCommit, err := repo.CommitObject(commitHash)
	if err != nil {
		return fmt.Errorf("failed to get commit %s: %w", commitSHA, err)
	}

	// Get the worktree
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Reset the worktree to the source commit (hard reset brings source files into workdir)
	err = w.Reset(&git.ResetOptions{
		Commit: commitHash,
		Mode:   git.HardReset,
	})
	if err != nil {
		return fmt.Errorf("failed to reset worktree to cherry-pick commit: %w", err)
	}

	// Stage all changes
	_, err = w.Add(".")
	if err != nil {
		return fmt.Errorf("failed to stage cherry-pick changes: %w", err)
	}

	// Create a new commit with the cherry-picked changes
	commitMsg := fmt.Sprintf("cherry-pick: %s\n\n(original commit: %s)", sourceCommit.Message, commitSHA)
	_, err = w.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Asika Bot",
			Email: "bot@asika",
			When:  time.Now(),
		},
		AllowEmptyCommits: true,
	})
	if err != nil {
		return fmt.Errorf("failed to commit cherry-pick: %w", err)
	}

	return nil
}

// Push pushes changes to a remote with a specific branch refspec
func Push(repo *git.Repository, remoteName, branch, token string) error {
	opts := &git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)),
		},
	}
	if token != "" {
		opts.Auth = &http.BasicAuth{
			Username: "git",
			Password: token,
		}
	}

	if remoteName == "" {
		remoteName = "origin"
	}

	remote, err := repo.Remote(remoteName)
	if err != nil {
		return fmt.Errorf("failed to get remote %s: %w", remoteName, err)
	}

	return remote.Push(opts)
}

// FetchRemote fetches from a remote
func FetchRemote(repo *git.Repository, remoteName, token string) error {
	opts := &git.FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/" + remoteName + "/*"),
		},
	}
	if token != "" {
		opts.Auth = &http.BasicAuth{
			Username: "git",
			Password: token,
		}
	}

	if remoteName == "" {
		remoteName = "origin"
	}

	return repo.Fetch(opts)
}

// CheckoutBranch checks out a branch in the worktree
func CheckoutBranch(repo *git.Repository, branch string) error {
	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branch),
	})
}

// CreateBranch creates and checks out a new branch
func CreateBranch(repo *git.Repository, branch string) error {
	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branch),
		Create: true,
	})
}

// CreateTempWorkdir creates a temporary working directory
func CreateTempWorkdir(prefix string) (string, error) {
	return os.MkdirTemp("", prefix)
}

// CleanupWorkdir removes a working directory
func CleanupWorkdir(workdir string) error {
	return os.RemoveAll(workdir)
}

// AddRemote adds a remote to the repository
func AddRemote(repo *git.Repository, name, url string) error {
	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	return err
}

// GetCommit gets a commit by SHA
func GetCommit(repo *git.Repository, sha string) (*object.Commit, error) {
	hash := plumbing.NewHash(sha)
	return repo.CommitObject(hash)
}

// CommitChanges commits staged changes in the worktree
func CommitChanges(repo *git.Repository, message string) error {
	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	_, err = w.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Asika Bot",
			Email: "bot@asika",
			When:  time.Now(),
		},
	})
	return err
}

// CherryPickRemote clones a repo, checks out the target branch,
// cherry-picks the given commit SHA, and pushes the result.
// workdir is the local clone directory. If empty, a temp dir is used.
func CherryPickRemote(workdir, remoteURL, token, targetBranch, commitSHA string, persistentPath string) error {
	repo, dir, cleanup, err := prepareRepo(workdir, remoteURL, token, persistentPath)
	if err != nil {
		return err
	}
	if cleanup {
		defer CleanupWorkdir(dir)
	}

	// Fetch latest
	err = FetchRemote(repo, "origin", token)
	if err != nil && !isUpToDate(err) {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Checkout target branch (create from remote if needed)
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + targetBranch),
	})
	if err != nil {
		// Try creating from remote tracking branch
		remoteRef := plumbing.NewRemoteReferenceName("origin", targetBranch)
		err = w.Checkout(&git.CheckoutOptions{
			Branch: plumbing.ReferenceName("refs/heads/" + targetBranch),
			Create: true,
			Hash:   plumbing.ZeroHash,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout target branch %s: %w", targetBranch, err)
		}
		// Reset to remote tracking branch
		remoteHash, refErr := repo.Reference(remoteRef, true)
		if refErr == nil {
			err = w.Reset(&git.ResetOptions{
				Commit: remoteHash.Hash(),
				Mode:   git.HardReset,
			})
			if err != nil {
				return fmt.Errorf("failed to reset to remote branch: %w", err)
			}
		}
	}

	// Cherry-pick the commit
	err = CherryPick(repo, commitSHA)
	if err != nil {
		return fmt.Errorf("cherry-pick failed: %w", err)
	}

	// Push the result
	err = Push(repo, "origin", targetBranch, token)
	if err != nil {
		return fmt.Errorf("failed to push cherry-pick: %w", err)
	}

	return nil
}

// Rebase rebases the current branch onto the given target branch.
// It fetches the latest target branch, checks out the head branch,
// performs the rebase, then force-pushes the result.
// workdir is the local clone directory. If empty, a temp dir is used.
// token is used for fetch/push authentication.
func Rebase(workdir, remoteURL, token, headBranch, baseBranch string, persistentPath string) error {
	repo, dir, cleanup, err := prepareRepo(workdir, remoteURL, token, persistentPath)
	if err != nil {
		return err
	}
	if cleanup {
		defer CleanupWorkdir(dir)
	}

	// Fetch latest base branch
	err = FetchRemote(repo, "origin", token)
	if err != nil {
		// Ignore "already up-to-date" errors
		if !isUpToDate(err) {
			return fmt.Errorf("failed to fetch: %w", err)
		}
	}

	// Checkout head branch
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + headBranch),
	})
	if err != nil {
		return fmt.Errorf("failed to checkout head branch %s: %w", headBranch, err)
	}

	// Resolve base branch ref from remote tracking branch
	baseRef := plumbing.NewRemoteReferenceName("origin", baseBranch)

	// Perform rebase: replay commits from HEAD onto baseRef
	// go-git doesn't have a built-in Rebase, so we use Reset to the base
	// and then cherry-pick commits. Simpler approach: merge --ff-only equivalent
	// by resetting to base and replaying. Actually, the simplest correct approach
	// with go-git is to use git's rebase via exec, but to stay pure Go we
	// implement it as: find common ancestor, create temp branch, cherry-pick.

	// Get the base commit
	baseCommit, err := repo.CommitObject(plumbing.NewHash(baseRef.String()))
	if err != nil {
		// Try resolving as remote ref
		remoteRef, refErr := repo.Reference(baseRef, true)
		if refErr != nil {
			return fmt.Errorf("failed to resolve base branch %s: %w", baseBranch, refErr)
		}
		baseCommit, err = repo.CommitObject(remoteRef.Hash())
		if err != nil {
			return fmt.Errorf("failed to get base commit: %w", err)
		}
	}

	// Get current HEAD commit (top of PR branch)
	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	// Find commits on HEAD that are not on base (the PR's own commits)
	prCommits, err := findPRCommits(repo, headCommit, baseCommit)
	if err != nil {
		return fmt.Errorf("failed to find PR commits: %w", err)
	}

	if len(prCommits) == 0 {
		// Nothing to rebase, already up to date
		return nil
	}

	// Reset to base commit
	err = w.Reset(&git.ResetOptions{
		Commit: baseCommit.Hash,
		Mode:   git.HardReset,
	})
	if err != nil {
		return fmt.Errorf("failed to reset to base: %w", err)
	}

	// Cherry-pick each PR commit onto the rebased base
	for _, commit := range prCommits {
		err = cherryPickCommit(repo, commit)
		if err != nil {
			return fmt.Errorf("failed to cherry-pick %s during rebase: %w", commit.Hash.String()[:8], err)
		}
	}

	// Force-push the rebased branch
	err = Push(repo, "origin", headBranch, token)
	if err != nil {
		return fmt.Errorf("failed to push rebased branch: %w", err)
	}

	return nil
}

// prepareRepo clones or opens a repo. Returns repo, workdir, whether to cleanup, and error.
func prepareRepo(workdir, url, token string, persistentPath string) (*git.Repository, string, bool, error) {
	if workdir != "" {
		repo, err := git.PlainOpen(workdir)
		if err != nil {
			return nil, "", false, fmt.Errorf("failed to open repo at %s: %w", workdir, err)
		}
		return repo, workdir, false, nil
	}

	dir := persistentPath
	cleanup := false
	if dir == "" {
		var err error
		dir, err = CreateTempWorkdir("asika-rebase-")
		if err != nil {
			return nil, "", false, fmt.Errorf("failed to create temp dir: %w", err)
		}
		cleanup = true
	}

	repo, err := CloneOrOpen(dir, url, token)
	if err != nil {
		return nil, dir, cleanup, fmt.Errorf("failed to clone repo: %w", err)
	}
	return repo, dir, cleanup, nil
}

// findPRCommits returns commits reachable from head but not from base, in chronological order.
func findPRCommits(repo *git.Repository, head, base *object.Commit) ([]*object.Commit, error) {
	// Build set of base commit hashes
	baseHashes := make(map[plumbing.Hash]bool)
	err := object.NewCommitPreorderIter(base, nil, nil).ForEach(func(c *object.Commit) error {
		baseHashes[c.Hash] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Walk head commits, collect those not in base set
	var prCommits []*object.Commit
	err = object.NewCommitPreorderIter(head, nil, nil).ForEach(func(c *object.Commit) error {
		if c.Hash == base.Hash {
			return storer.ErrStop
		}
		if !baseHashes[c.Hash] {
			prCommits = append(prCommits, c)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Reverse to get chronological order (oldest first)
	for i, j := 0, len(prCommits)-1; i < j; i, j = i+1, j-1 {
		prCommits[i], prCommits[j] = prCommits[j], prCommits[i]
	}

	return prCommits, nil
}

// cherryPickCommit cherry-picks a single commit onto the current HEAD.
func cherryPickCommit(repo *git.Repository, commit *object.Commit) error {
	return CherryPick(repo, commit.Hash.String())
}

func isUpToDate(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already up-to-date") ||
		strings.Contains(msg, "up to date")
}