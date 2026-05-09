package reviewer

import (
	"testing"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func TestMatchReviewRule_EmptyPattern(t *testing.T) {
	rule := models.ReviewRule{Pattern: "", Reviewers: []string{"user1"}}
	if matchReviewRule(rule, []string{"main.go"}, "title", "author") {
		t.Error("expected false for empty pattern")
	}
}

func TestMatchReviewRule_MatchFile(t *testing.T) {
	rule := models.ReviewRule{Pattern: "**/*.go", Reviewers: []string{"user1"}}
	if !matchReviewRule(rule, []string{"src/main.go"}, "title", "author") {
		t.Error("expected true for matching file pattern")
	}
}

func TestMatchReviewRule_NoMatch(t *testing.T) {
	rule := models.ReviewRule{Pattern: "**/*.go", Reviewers: []string{"user1"}}
	if matchReviewRule(rule, []string{"README.md"}, "title", "author") {
		t.Error("expected false for non-matching file")
	}
}

func TestMatchReviewRule_MatchTitle(t *testing.T) {
	rule := models.ReviewRule{Pattern: "title:fix", Reviewers: []string{"user1"}}
	if !matchReviewRule(rule, []string{}, "fix bug", "author") {
		t.Error("expected true for matching title pattern")
	}
}

func TestMatchReviewRule_MatchAuthor(t *testing.T) {
	rule := models.ReviewRule{Pattern: "author:dev1", Reviewers: []string{"user1"}}
	if !matchReviewRule(rule, []string{}, "title", "dev1") {
		t.Error("expected true for matching author pattern")
	}
}

func TestNewReviewer(t *testing.T) {
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}
	r := NewReviewer(clients)
	if r == nil {
		t.Fatal("NewReviewer returned nil")
	}
	if len(r.clients) != 1 {
		t.Errorf("clients len = %d, want 1", len(r.clients))
	}
}

func TestHandlePROpened_NoConfig(t *testing.T) {
	config.Store(nil)
	defer config.Store(nil)

	r := NewReviewer(map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	})
	pr := &models.PRRecord{Platform: "github", PRNumber: 1, Title: "test"}
	r.HandlePROpened(pr, "mygroup")
}

func TestHandlePROpened_NoRules(t *testing.T) {
	cfg := &models.Config{
		ReviewRules: []models.ReviewRule{},
		RepoGroups:  []models.RepoGroupConfig{{Name: "mygroup", GitHub: "org/repo"}},
	}
	config.Store(cfg)

	r := NewReviewer(map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	})
	pr := &models.PRRecord{Platform: "github", PRNumber: 1, Title: "test"}
	r.HandlePROpened(pr, "mygroup")
}

func TestHandlePROpened_NoClient(t *testing.T) {
	cfg := &models.Config{
		ReviewRules: []models.ReviewRule{
			{Pattern: "**/*.go", Reviewers: []string{"user1"}},
		},
		RepoGroups: []models.RepoGroupConfig{{Name: "mygroup", GitHub: "org/repo"}},
	}
	config.Store(cfg)

	r := NewReviewer(map[platforms.PlatformType]platforms.PlatformClient{})
	pr := &models.PRRecord{Platform: "github", PRNumber: 1, Title: "test"}
	r.HandlePROpened(pr, "mygroup")
}

func TestHandlePROpened_NoGroup(t *testing.T) {
	cfg := &models.Config{
		ReviewRules: []models.ReviewRule{
			{Pattern: "**/*.go", Reviewers: []string{"user1"}},
		},
		RepoGroups: []models.RepoGroupConfig{},
	}
	config.Store(cfg)

	r := NewReviewer(map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	})
	pr := &models.PRRecord{Platform: "github", PRNumber: 1, Title: "test"}
	r.HandlePROpened(pr, "mygroup")
}

func TestHandlePROpened_NoMatch(t *testing.T) {
	cfg := &models.Config{
		ReviewRules: []models.ReviewRule{
			{Pattern: "**/*.go", Reviewers: []string{"user1"}},
		},
		RepoGroups: []models.RepoGroupConfig{{Name: "mygroup", GitHub: "org/repo"}},
	}
	config.Store(cfg)

	mock := testutil.NewMockPlatformClient()
	mock.DiffFiles = []string{"README.md"}
	r := NewReviewer(map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	})
	pr := &models.PRRecord{Platform: "github", PRNumber: 1, Title: "test"}
	r.HandlePROpened(pr, "mygroup")
}

func TestHandlePROpened_MatchAndRequest(t *testing.T) {
	cfg := &models.Config{
		ReviewRules: []models.ReviewRule{
			{Pattern: "**/*.go", Reviewers: []string{"user1", "user2"}},
		},
		RepoGroups: []models.RepoGroupConfig{{Name: "mygroup", GitHub: "org/repo"}},
	}
	config.Store(cfg)

	mock := testutil.NewMockPlatformClient()
	mock.DiffFiles = []string{"src/main.go"}
	r := NewReviewer(map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	})
	pr := &models.PRRecord{Platform: "github", PRNumber: 42, Title: "fix bug"}
	r.HandlePROpened(pr, "mygroup")
}

func TestHandlePROpened_EmptyOwnerRepo(t *testing.T) {
	cfg := &models.Config{
		ReviewRules: []models.ReviewRule{
			{Pattern: "**/*.go", Reviewers: []string{"user1"}},
		},
		RepoGroups: []models.RepoGroupConfig{{Name: "mygroup"}},
	}
	config.Store(cfg)

	mock := testutil.NewMockPlatformClient()
	mock.DiffFiles = []string{"main.go"}
	r := NewReviewer(map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	})
	pr := &models.PRRecord{Platform: "github", PRNumber: 1, Title: "test"}
	r.HandlePROpened(pr, "mygroup")
}
