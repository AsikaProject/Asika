package gitutil

import (
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// createTestRepo creates a test repo with initial commit
func createTestRepo(t *testing.T, workdir string) *git.Repository {
	t.Helper()
	repo, err := git.PlainInit(workdir, false)
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}

	// Create initial file and commit
	err = createFileAndCommit(t, w, "README.md", "# Test Repo", "Initial commit")
	if err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	return repo
}

// getDefaultBranch gets the default branch name of the repo
func getDefaultBranch(t *testing.T, repo *git.Repository) string {
	t.Helper()
	ref, err := repo.Head()
	if err != nil {
		return "master" // Default to master
	}
	return ref.Name().Short()
}

// createFileAndCommit creates file and commits
func createFileAndCommit(t *testing.T, w *git.Worktree, filename, content, msg string) error {
	t.Helper()
	// Use worktree's filesystem to create file
	f, err := w.Filesystem.Create(filename)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(content))
	if err != nil {
		f.Close()
		return err
	}
	f.Close()

	_, err = w.Add(filename)
	if err != nil {
		return err
	}

	_, err = w.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test Author",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	return err
}

// createBareRepo creates a bare repo as remote
func createBareRepo(t *testing.T, path string) *git.Repository {
	t.Helper()
	repo, err := git.PlainInit(path, true)
	if err != nil {
		t.Fatalf("failed to init bare repo: %v", err)
	}
	return repo
}

func TestCloneOrOpen_NewRepo(t *testing.T) {
	dir := t.TempDir()
	remoteDir := t.TempDir()

	// Create a repo with commit as remote
	createTestRepo(t, remoteDir)

	// First call should clone
	repo, err := CloneOrOpen(dir, remoteDir, "")
	if err != nil {
		t.Fatalf("CloneOrOpen() failed: %v", err)
	}
	if repo == nil {
		t.Fatal("expected non-nil repository")
	}
}

func TestCloneOrOpen_ExistingRepo(t *testing.T) {
	dir := t.TempDir()

	// Initialize a repo first
	repo1 := createTestRepo(t, dir)

	// Second call should open existing repo
	repo2, err := CloneOrOpen(dir, "unused-url", "")
	if err != nil {
		t.Fatalf("CloneOrOpen() failed for existing repo: %v", err)
	}

	// Verify it's the same repo
	head1, _ := repo1.Head()
	head2, _ := repo2.Head()
	if head1.Hash() != head2.Hash() {
		t.Error("expected same HEAD for reopened repo")
	}
}

func TestCherryPick_Success(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	w, _ := repo.Worktree()

	defaultBranch := getDefaultBranch(t, repo)

	// Create another branch and commit
	err := CreateBranch(repo, "feature")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// Make commit on feature branch
	err = createFileAndCommit(t, w, "feature.txt", "feature content", "Add feature")
	if err != nil {
		t.Fatalf("failed to commit on feature branch: %v", err)
	}

	// Get this commit's SHA
	head, _ := repo.Head()
	commitSHA := head.Hash().String()

	// Go back to default branch
	err = CheckoutBranch(repo, defaultBranch)
	if err != nil {
		t.Fatalf("CheckoutBranch failed: %v", err)
	}

	// Cherry-pick feature branch's commit
	err = CherryPick(repo, commitSHA)
	if err != nil {
		t.Fatalf("CherryPick() failed: %v", err)
	}

	// Verify cherry-pick succeeded
	newHead, _ := repo.Head()
	if newHead.Hash().String() == commitSHA {
		t.Error("cherry-pick should create a new commit, not reuse the original")
	}
}

func TestCherryPickMergeDiff_Success(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)
	w, _ := repo.Worktree()
	defaultBranch := getDefaultBranch(t, repo)

	// Create feature branch with a new file
	if err := CreateBranch(repo, "feature"); err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}
	if err := createFileAndCommit(t, w, "feature.txt", "feature content", "Add feature"); err != nil {
		t.Fatalf("failed to commit on feature: %v", err)
	}
	featureHead, _ := repo.Head()

	// Go back to default and create a merge commit
	if err := CheckoutBranch(repo, defaultBranch); err != nil {
		t.Fatalf("CheckoutBranch failed: %v", err)
	}

	// Merge feature into default to create a merge commit
	mergeCommit, err := w.Commit("Merge feature", &git.CommitOptions{
		Parents: []plumbing.Hash{featureHead.Hash(), featureHead.Hash()},
		Author: &object.Signature{
			Name:  "Test Author",
			Email: "test@example.com",
			When:  time.Now(),
		},
		AllowEmptyCommits: true,
	})
	if err != nil {
		t.Fatalf("merge commit failed: %v", err)
	}

	// Reset to initial state (before merge)
	initialRef, err := repo.Reference(plumbing.NewBranchReferenceName(defaultBranch), true)
	if err != nil {
		t.Fatalf("failed to get initial ref: %v", err)
	}
	w.Reset(&git.ResetOptions{Commit: initialRef.Hash(), Mode: git.HardReset})

	// Now cherry-pick the merge commit using merge-diff strategy
	err = CherryPickMergeDiff(repo, mergeCommit.String())
	if err != nil {
		t.Fatalf("CherryPickMergeDiff failed: %v", err)
	}
}

func TestCherryPickMergeDiff_NotMergeCommit(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	head, _ := repo.Head()
	err := CherryPickMergeDiff(repo, head.Hash().String())
	if err == nil {
		t.Error("expected error for non-merge commit")
	}
}

func TestCherryPick_InvalidCommit(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	// Try to cherry-pick a non-existent commit
	err := CherryPick(repo, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err == nil {
		t.Error("expected error for invalid commit SHA")
	}
}

func TestCheckoutBranch(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	defaultBranch := getDefaultBranch(t, repo)

	// Create and switch to new branch
	err := CreateBranch(repo, "test-branch")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// Verify current branch
	head, _ := repo.Head()
	if head.Name().Short() != "test-branch" {
		t.Errorf("expected current branch test-branch, got %s", head.Name().Short())
	}

	// Switch back to default branch
	err = CheckoutBranch(repo, defaultBranch)
	if err != nil {
		t.Fatalf("CheckoutBranch failed: %v", err)
	}

	head, _ = repo.Head()
	if head.Name().Short() != defaultBranch {
		t.Errorf("expected current branch %s, got %s", defaultBranch, head.Name().Short())
	}
}

func TestCreateBranch(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	// Create new branch
	err := CreateBranch(repo, "new-branch")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// Verify branch exists
	branchRef, err := repo.Reference("refs/heads/new-branch", false)
	if err != nil {
		t.Fatalf("branch new-branch not found: %v", err)
	}

	head, _ := repo.Head()
	if branchRef.Hash() != head.Hash() {
		t.Error("new branch should point to the same commit as HEAD")
	}
}

func TestAddRemote(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	// Add remote
	err := AddRemote(repo, "upstream", "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("AddRemote failed: %v", err)
	}

	// Verify remote exists
	remote, err := repo.Remote("upstream")
	if err != nil {
		t.Fatalf("remote upstream not found: %v", err)
	}

	urls := remote.Config().URLs
	if len(urls) == 0 || urls[0] != "https://example.com/repo.git" {
		t.Errorf("unexpected remote URL: %v", urls)
	}
}

func TestGetCommit(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	// Get HEAD commit
	head, _ := repo.Head()
	commitSHA := head.Hash().String()

	commit, err := GetCommit(repo, commitSHA)
	if err != nil {
		t.Fatalf("GetCommit failed: %v", err)
	}

	if commit.Hash.String() != commitSHA {
		t.Errorf("expected commit SHA %s, got %s", commitSHA, commit.Hash.String())
	}
}

func TestGetCommit_InvalidSHA(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	_, err := GetCommit(repo, "invalid-sha")
	if err == nil {
		t.Error("expected error for invalid SHA")
	}
}

func TestCommitChanges(t *testing.T) {
	dir := t.TempDir()
	repo := createTestRepo(t, dir)

	w, _ := repo.Worktree()

	// Create new file and commit
	err := createFileAndCommit(t, w, "newfile.txt", "new content", "Add new file")
	if err != nil {
		t.Fatalf("createFileAndCommit failed: %v", err)
	}

	// Verify commit succeeded
	head, _ := repo.Head()
	commit, _ := repo.CommitObject(head.Hash())
	if commit.Message != "Add new file" {
		t.Errorf("unexpected commit message: got %q, want %q", commit.Message, "Add new file")
	}
}

func TestPush(t *testing.T) {
	remoteDir := t.TempDir()
	dir := t.TempDir()

	// Create bare remote repo
	_, err := git.PlainInit(remoteDir, true)
	if err != nil {
		t.Fatalf("failed to create bare remote: %v", err)
	}

	// Create local repo
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("failed to init local repo: %v", err)
	}

	// Add remote
	err = AddRemote(repo, "origin", remoteDir)
	if err != nil {
		t.Fatalf("AddRemote failed: %v", err)
	}

	w, _ := repo.Worktree()

	// Create initial commit
	err = createFileAndCommit(t, w, "README.md", "# Test", "Initial commit")
	if err != nil {
		t.Fatalf("failed to create commit: %v", err)
	}

	defaultBranch := getDefaultBranch(t, repo)

	// Push to remote
	err = Push(repo, "origin", defaultBranch, "")
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Verify remote repo has this commit
	remoteRepo, _ := git.PlainOpen(remoteDir)
	remoteHead, _ := remoteRepo.Head()
	localHead, _ := repo.Head()

	if remoteHead.Hash() != localHead.Hash() {
		t.Error("remote HEAD should match local HEAD after push")
	}
}

func TestFetchRemote(t *testing.T) {
	remoteDir := t.TempDir()
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Create bare remote repo
	_, err := git.PlainInit(remoteDir, true)
	if err != nil {
		t.Fatalf("failed to create bare remote: %v", err)
	}

	// Create local repo repo1
	repo1, err := git.PlainInit(dir1, false)
	if err != nil {
		t.Fatalf("failed to init repo1: %v", err)
	}

	// Add remote and push initial commit
	err = AddRemote(repo1, "origin", remoteDir)
	if err != nil {
		t.Fatalf("AddRemote to repo1 failed: %v", err)
	}

	w1, _ := repo1.Worktree()
	err = createFileAndCommit(t, w1, "README.md", "# Test", "Initial commit")
	if err != nil {
		t.Fatalf("failed to create commit in repo1: %v", err)
	}

	defaultBranch := getDefaultBranch(t, repo1)

	// Push initial commit to remote
	err = Push(repo1, "origin", defaultBranch, "")
	if err != nil {
		t.Fatalf("Push from repo1 failed: %v", err)
	}

	// Clone remote repo to repo2
	repo2, err := git.PlainClone(dir2, false, &git.CloneOptions{
		URL: remoteDir,
	})
	if err != nil {
		t.Fatalf("failed to clone repo2: %v", err)
	}

	// Create new commit in repo1 and push to remote
	err = createFileAndCommit(t, w1, "newfile.txt", "new content", "Add new file")
	if err != nil {
		t.Fatalf("failed to create new commit: %v", err)
	}

	err = Push(repo1, "origin", defaultBranch, "")
	if err != nil {
		t.Fatalf("Push new commit failed: %v", err)
	}

	// Fetch in repo2
	err = FetchRemote(repo2, "origin", "")
	if err != nil {
		t.Fatalf("FetchRemote failed: %v", err)
	}

	// Verify repo2 can get remote's new commit
	remoteRefs, _ := repo2.Remotes()
	if len(remoteRefs) == 0 {
		t.Error("expected remote references after fetch")
	}
}

func TestCreateTempWorkdir(t *testing.T) {
	dir, err := CreateTempWorkdir("test-prefix")
	if err != nil {
		t.Fatalf("CreateTempWorkdir failed: %v", err)
	}
	t.Cleanup(func() { CleanupWorkdir(dir) })

	if dir == "" {
		t.Error("expected non-empty directory path")
	}
}

func TestCleanupWorkdir(t *testing.T) {
	dir, err := CreateTempWorkdir("test-cleanup")
	if err != nil {
		t.Fatalf("CreateTempWorkdir failed: %v", err)
	}

	// Cleanup
	err = CleanupWorkdir(dir)
	if err != nil {
		t.Fatalf("CleanupWorkdir failed: %v", err)
	}

	// Verify directory is deleted
	if _, err := git.PlainOpen(dir); err == nil {
		t.Error("expected directory to be removed after cleanup")
	}
}
