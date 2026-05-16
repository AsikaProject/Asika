package gitutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func CreateTempWorkdir(prefix string) (string, error) {
	return os.MkdirTemp("", prefix)
}

func CleanupWorkdir(workdir string) error {
	return os.RemoveAll(workdir)
}

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
	} else {
		repoDir := repoScopedPath(dir, url)
		repo, err := git.PlainOpen(repoDir)
		if err == nil {
			return repo, repoDir, false, nil
		}
		dir = repoDir
	}

	repo, err := CloneOrOpen(dir, url, token)
	if err != nil {
		return nil, dir, cleanup, fmt.Errorf("failed to clone repo: %w", err)
	}
	return repo, dir, cleanup, nil
}

func repoScopedPath(basePath, url string) string {
	h := sha256.Sum256([]byte(url))
	return filepath.Join(basePath, hex.EncodeToString(h[:8]))
}

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

func findPRCommits(repo *git.Repository, head, base *object.Commit) ([]*object.Commit, error) {
	baseHashes := make(map[plumbing.Hash]bool)
	err := object.NewCommitPreorderIter(base, nil, nil).ForEach(func(c *object.Commit) error {
		baseHashes[c.Hash] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

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

	for i, j := 0, len(prCommits)-1; i < j; i, j = i+1, j-1 {
		prCommits[i], prCommits[j] = prCommits[j], prCommits[i]
	}
	return prCommits, nil
}

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
