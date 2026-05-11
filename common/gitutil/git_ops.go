package gitutil

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func CherryPick(repo *git.Repository, commitSHA string) error {
	commitHash := plumbing.NewHash(commitSHA)
	sourceCommit, err := repo.CommitObject(commitHash)
	if err != nil {
		return fmt.Errorf("failed to get commit %s: %w", commitSHA, err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.Reset(&git.ResetOptions{
		Commit: commitHash,
		Mode:   git.HardReset,
	})
	if err != nil {
		return fmt.Errorf("failed to reset worktree to cherry-pick commit: %w", err)
	}

	_, err = w.Add(".")
	if err != nil {
		return fmt.Errorf("failed to stage cherry-pick changes: %w", err)
	}

	commitMsg := fmt.Sprintf("cherry-pick: %s\n\n(original commit: %s)", sourceCommit.Message, commitSHA)
	_, err = w.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Asika Bot",
			Email: "bot@asika",
		},
		AllowEmptyCommits: true,
	})
	if err != nil {
		return fmt.Errorf("failed to commit cherry-pick: %w", err)
	}

	return nil
}

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

func CheckoutBranch(repo *git.Repository, branch string) error {
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branch),
	})
}

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

func AddRemote(repo *git.Repository, name, url string) error {
	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	return err
}

func GetCommit(repo *git.Repository, sha string) (*object.Commit, error) {
	hash := plumbing.NewHash(sha)
	return repo.CommitObject(hash)
}
