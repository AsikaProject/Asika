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

	if sourceCommit.NumParents() >= 2 {
		return CherryPickMergeDiff(repo, commitSHA)
	}

	return cherryPickRegular(repo, sourceCommit, commitHash)
}

func cherryPickRegular(repo *git.Repository, sourceCommit *object.Commit, commitHash plumbing.Hash) error {
	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	parentCommit, err := sourceCommit.Parent(0)
	if err != nil {
		return fmt.Errorf("failed to get parent commit: %w", err)
	}

	parentTree, err := parentCommit.Tree()
	if err != nil {
		return fmt.Errorf("failed to get parent tree: %w", err)
	}

	sourceTree, err := sourceCommit.Tree()
	if err != nil {
		return fmt.Errorf("failed to get source tree: %w", err)
	}

	changes, err := object.DiffTree(parentTree, sourceTree)
	if err != nil {
		return fmt.Errorf("failed to diff trees: %w", err)
	}

	if len(changes) == 0 {
		return nil
	}

	headTree, err := headCommit.Tree()
	if err != nil {
		return fmt.Errorf("failed to get HEAD tree: %w", err)
	}

	headChanged := make(map[string]bool)
	headChanges, err := object.DiffTree(parentTree, headTree)
	if err != nil {
		return fmt.Errorf("failed to diff HEAD vs parent: %w", err)
	}
	for _, ch := range headChanges {
		if ch.To.Name != "" {
			headChanged[ch.To.Name] = true
		}
	}

	conflictedFiles := make([]string, 0)
	appliedFiles := make([]string, 0)

	for _, change := range changes {
		filename := change.To.Name
		if filename == "" {
			filename = change.From.Name
		}
		if filename == "" {
			continue
		}

		if headChanged[filename] {
			conflictedFiles = append(conflictedFiles, filename)
			continue
		}

		sourceEntry, err := sourceTree.File(filename)
		if err != nil {
			continue
		}
		content, err := sourceEntry.Contents()
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", filename, err)
		}

		if err := writeFileAndStage(w, filename, content); err != nil {
			return err
		}
		appliedFiles = append(appliedFiles, filename)
	}

	if len(conflictedFiles) > 0 {
		return fmt.Errorf("files modified on both target branch and source commit, cannot auto-cherry-pick: %v", conflictedFiles)
	}

	if len(appliedFiles) == 0 {
		return nil
	}

	commitMsg := fmt.Sprintf("cherry-pick: %s\n\n(original commit: %s)", sourceCommit.Message, commitHash.String())
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

func writeFileAndStage(w *git.Worktree, filename, content string) error {
	f, err := w.Filesystem.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		f.Close()
		return fmt.Errorf("failed to write file %s: %w", filename, err)
	}
	f.Close()
	if _, err := w.Add(filename); err != nil {
		return fmt.Errorf("failed to stage file %s: %w", filename, err)
	}
	return nil
}

// CherryPickMergeDiff applies only the changes introduced by a merge commit
// relative to its first parent (the base branch side). This is a fallback when
// the standard CherryPick (full tree reset) fails due to the target branch
// having diverged from the merge commit's first parent.
//
// Strategy: diff parent1 -> merge_tree, then for each changed file, write the
// merge version onto the current worktree. Files that the target branch has
// independently modified (vs parent1) are flagged as conflicts to avoid silent
// data loss (dabao1955 scenario).
func CherryPickMergeDiff(repo *git.Repository, commitSHA string) error {
	commitHash := plumbing.NewHash(commitSHA)
	sourceCommit, err := repo.CommitObject(commitHash)
	if err != nil {
		return fmt.Errorf("failed to get commit %s: %w", commitSHA, err)
	}

	if sourceCommit.NumParents() < 2 {
		return fmt.Errorf("commit %s is not a merge commit (has %d parents)", commitSHA, sourceCommit.NumParents())
	}

	parent1, err := sourceCommit.Parent(0)
	if err != nil {
		return fmt.Errorf("failed to get parent 1: %w", err)
	}

	parent1Tree, err := parent1.Tree()
	if err != nil {
		return fmt.Errorf("failed to get parent 1 tree: %w", err)
	}

	mergeTree, err := sourceCommit.Tree()
	if err != nil {
		return fmt.Errorf("failed to get merge tree: %w", err)
	}

	changes, err := object.DiffTree(parent1Tree, mergeTree)
	if err != nil {
		return fmt.Errorf("failed to diff trees: %w", err)
	}

	if len(changes) == 0 {
		return nil
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return fmt.Errorf("failed to get HEAD commit: %w", err)
	}
	headTree, err := headCommit.Tree()
	if err != nil {
		return fmt.Errorf("failed to get HEAD tree: %w", err)
	}

	headChanges, err := object.DiffTree(parent1Tree, headTree)
	if err != nil {
		return fmt.Errorf("failed to diff HEAD vs parent1: %w", err)
	}

	headChanged := make(map[string]bool)
	for _, ch := range headChanges {
		if ch.To.Name != "" {
			headChanged[ch.To.Name] = true
		}
	}

	conflictedFiles := make([]string, 0)
	appliedFiles := make([]string, 0)

	for _, change := range changes {
		filename := change.To.Name
		if filename == "" {
			filename = change.From.Name
		}
		if filename == "" {
			continue
		}

		if headChanged[filename] {
			conflictedFiles = append(conflictedFiles, filename)
			continue
		}

		mergeEntry, err := mergeTree.File(filename)
		if err != nil {
			continue
		}
		content, err := mergeEntry.Contents()
		if err != nil {
			return fmt.Errorf("failed to read file %s from merge tree: %w", filename, err)
		}

		if err := writeFileAndStage(w, filename, content); err != nil {
			return err
		}
		appliedFiles = append(appliedFiles, filename)
	}

	if len(conflictedFiles) > 0 {
		return fmt.Errorf("files modified on both target branch and merge commit, cannot auto-apply: %v", conflictedFiles)
	}

	if len(appliedFiles) == 0 {
		return nil
	}

	commitMsg := fmt.Sprintf("cherry-pick (merge-diff): %s\n\n(original commit: %s)", sourceCommit.Message, commitSHA)
	_, err = w.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Asika Bot",
			Email: "bot@asika",
		},
		AllowEmptyCommits: true,
	})
	if err != nil {
		return fmt.Errorf("failed to commit merge-diff cherry-pick: %w", err)
	}

	return nil
}

func Push(repo *git.Repository, remoteName, branch, token string) error {
	opts := &git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)),
		},
		Force: true,
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
