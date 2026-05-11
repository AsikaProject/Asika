package gitutil

import (
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func CloneOrOpen(workdir, url, token string) (*git.Repository, error) {
	if workdir != "" {
		if _, err := os.Stat(workdir); os.IsNotExist(err) {
			repo, err := cloneRepo(workdir, url, token)
			if err != nil {
				return nil, fmt.Errorf("failed to clone repo: %w", err)
			}
			return repo, nil
		}
		repo, err := git.PlainOpen(workdir)
		if err != nil {
			os.RemoveAll(workdir)
			repo, err = cloneRepo(workdir, url, token)
			if err != nil {
				return nil, fmt.Errorf("failed to clone repo: %w", err)
			}
			return repo, nil
		}
		return repo, nil
	}

	repo, dir, cleanup, err := prepareRepo("", url, token, "")
	if err != nil {
		return nil, err
	}
	if cleanup {
		defer CleanupWorkdir(dir)
	}
	return repo, nil
}

func CherryPickRemote(workdir, remoteURL, token, targetBranch, commitSHA string, persistentPath string) error {
	repo, dir, cleanup, err := prepareRepo(workdir, remoteURL, token, persistentPath)
	if err != nil {
		return err
	}
	if cleanup {
		defer CleanupWorkdir(dir)
	}

	err = FetchRemote(repo, "origin", token)
	if err != nil && !isUpToDate(err) {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + targetBranch),
	})
	if err != nil {
		remoteRef := plumbing.NewRemoteReferenceName("origin", targetBranch)
		err = w.Checkout(&git.CheckoutOptions{
			Branch: plumbing.ReferenceName("refs/heads/" + targetBranch),
			Create: true,
			Hash:   plumbing.ZeroHash,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout target branch %s: %w", targetBranch, err)
		}
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

	err = CherryPick(repo, commitSHA)
	if err != nil {
		return fmt.Errorf("cherry-pick failed: %w", err)
	}

	err = Push(repo, "origin", targetBranch, token)
	if err != nil {
		return fmt.Errorf("failed to push cherry-pick: %w", err)
	}

	return nil
}

func Rebase(workdir, remoteURL, token, headBranch, baseBranch string, persistentPath string) error {
	repo, dir, cleanup, err := prepareRepo(workdir, remoteURL, token, persistentPath)
	if err != nil {
		return err
	}
	if cleanup {
		defer CleanupWorkdir(dir)
	}

	err = FetchRemote(repo, "origin", token)
	if err != nil {
		if !isUpToDate(err) {
			return fmt.Errorf("failed to fetch: %w", err)
		}
	}

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

	baseRef := plumbing.NewRemoteReferenceName("origin", baseBranch)
	baseCommit, err := repo.CommitObject(plumbing.NewHash(baseRef.String()))
	if err != nil {
		remoteRef, refErr := repo.Reference(baseRef, true)
		if refErr != nil {
			return fmt.Errorf("failed to resolve base branch %s: %w", baseBranch, refErr)
		}
		baseCommit, err = repo.CommitObject(remoteRef.Hash())
		if err != nil {
			return fmt.Errorf("failed to get base commit: %w", err)
		}
	}

	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	prCommits, err := findPRCommits(repo, headCommit, baseCommit)
	if err != nil {
		return fmt.Errorf("failed to find PR commits: %w", err)
	}

	if len(prCommits) == 0 {
		return nil
	}

	err = w.Reset(&git.ResetOptions{
		Commit: baseCommit.Hash,
		Mode:   git.HardReset,
	})
	if err != nil {
		return fmt.Errorf("failed to reset to base: %w", err)
	}

	for _, commit := range prCommits {
		err = cherryPickCommit(repo, commit)
		if err != nil {
			return fmt.Errorf("failed to cherry-pick %s during rebase: %w", commit.Hash.String()[:8], err)
		}
	}

	err = Push(repo, "origin", headBranch, token)
	if err != nil {
		return fmt.Errorf("failed to push rebased branch: %w", err)
	}

	return nil
}

func RebaseAndPush(workdir, remoteURL, token, headBranch, baseBranch string) error {
	repo, dir, cleanup, err := prepareRepo(workdir, remoteURL, token, "")
	if err != nil {
		return err
	}
	if cleanup {
		defer CleanupWorkdir(dir)
	}

	err = FetchRemote(repo, "origin", token)
	if err != nil {
		if !isUpToDate(err) {
			return fmt.Errorf("failed to fetch: %w", err)
		}
	}

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

	baseRef := plumbing.NewRemoteReferenceName("origin", baseBranch)
	baseCommit, err := repo.ResolveRevision(plumbing.Revision(baseRef))
	if err != nil {
		return fmt.Errorf("failed to resolve base branch %s: %w", baseBranch, err)
	}

	headRef, err := repo.ResolveRevision(plumbing.Revision(plumbing.HEAD))
	if err != nil {
		return fmt.Errorf("failed to resolve HEAD: %w", err)
	}

	if *baseCommit == *headRef {
		return nil
	}

	iter, err := repo.Log(&git.LogOptions{From: *headRef})
	if err != nil {
		return fmt.Errorf("failed to get commit log: %w", err)
	}

	var commitsToRebase []*object.Commit
	iter.ForEach(func(c *object.Commit) error {
		if c.Hash == *baseCommit {
			return storer.ErrStop
		}
		commitsToRebase = append([]*object.Commit{c}, commitsToRebase...)
		return nil
	})

	err = w.Reset(&git.ResetOptions{
		Commit: *baseCommit,
		Mode:   git.HardReset,
	})
	if err != nil {
		return fmt.Errorf("failed to reset to base: %w", err)
	}

	for _, commit := range commitsToRebase {
		err = CherryPick(repo, commit.Hash.String())
		if err != nil {
			return fmt.Errorf("cherry-pick %s failed: %w", commit.Hash.String()[:8], err)
		}
	}

	opts := &git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", headBranch, headBranch)),
		},
	}
	if token != "" {
		opts.Auth = &http.BasicAuth{
			Username: "git",
			Password: token,
		}
	}

	err = repo.Push(opts)
	if err != nil && !isUpToDate(err) {
		return fmt.Errorf("push failed: %w", err)
	}

	return nil
}

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
