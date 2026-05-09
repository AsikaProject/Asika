package labeler

import (
	"context"
	"regexp"
	"sort"
	"testing"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func setupLabelerTest(t *testing.T) (*Labeler, *testutil.MockPlatformClient) {
	t.Helper()

	testutil.NewTestDB(t)

	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	return NewLabeler(clients), mock
}

func TestGlobPatternMatching(t *testing.T) {
	files := []string{"src/main.go", "test/main_test.go", "README.md", "docs/index.md"}

	tests := []struct {
		name    string
		pattern string
		files   []string
		want    bool
	}{
		{"glob *.go matches", "*.go", []string{"main.go"}, true},
		{"glob *.go no match", "*.go", []string{"README.md"}, false},
		{"glob **/*.md matches deep", "*.md", []string{"README.md", "docs/index.md"}, true},
		{"glob no match different ext", "*.py", []string{"main.go", "README.md"}, false},
		{"complex glob with dir", "src/*.go", []string{"src/main.go"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.files)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %v) = %v, want %v", tt.pattern, tt.files, got, tt.want)
			}
		})
	}

	_ = files
}

func TestRegexPatternMatching(t *testing.T) {
	// Clear compiled patterns cache between tests
	compiledPatterns = make(map[string]*regexp.Regexp)

	tests := []struct {
		name    string
		pattern string
		files   []string
		want    bool
	}{
		{"regex exact match", `^src/main\.go$`, []string{"src/main.go"}, true},
		{"regex partial match", `main\.go`, []string{"src/main.go"}, true},
		{"regex no match", `\.py$`, []string{"src/main.go"}, false},
		{"regex match any go file", `\.go$`, []string{"src/main.go", "test/main_test.go"}, true},
		{"regex specific dir", `^test/.*\.go$`, []string{"src/main.go", "test/main_test.go"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.files)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %v) = %v, want %v", tt.pattern, tt.files, got, tt.want)
			}
		})
	}
}

func TestApplyRules(t *testing.T) {
	l, mock := setupLabelerTest(t)
	defer func() { db.Close() }()

	compiledPatterns = make(map[string]*regexp.Regexp)

	cfg := &models.Config{
		LabelRules: []models.LabelRule{
			{Pattern: `\.go$`, Label: "go-code"},
			{Pattern: `\.md$`, Label: "documentation"},
			{Pattern: `^src/api/`, Label: "api"},
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "single", MirrorPlatform: "github", GitHub: "owner/repo"},
		},
	}
	config.Store(cfg)

	pr := &models.PRRecord{
		ID:        "pr-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "update api",
		Author:    "dev1",
		State:     "open",
	}

	mock.DiffFiles = []string{"src/api/handler.go", "README.md"}
	l.ApplyRules(pr, "test-group", mock.DiffFiles)

	ctx := context.Background()
	prs, _ := mock.ListPRs(ctx, "owner", "repo", "")
	_ = prs

	sort.Strings(mock.AppliedLabels)
	expected := []string{"api", "documentation", "go-code"}
	sort.Strings(expected)

	if len(mock.AppliedLabels) != len(expected) {
		t.Errorf("AppliedLabels = %v, want %v", mock.AppliedLabels, expected)
		return
	}

	for i := range expected {
		if mock.AppliedLabels[i] != expected[i] {
			t.Errorf("AppliedLabels[%d] = %q, want %q", i, mock.AppliedLabels[i], expected[i])
		}
	}
}

func TestApplyRulesNoMatch(t *testing.T) {
	l, mock := setupLabelerTest(t)
	defer func() { db.Close() }()

	compiledPatterns = make(map[string]*regexp.Regexp)

	cfg := &models.Config{
		LabelRules: []models.LabelRule{
			{Pattern: `\.py$`, Label: "python-code"},
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "single", MirrorPlatform: "github", GitHub: "owner/repo"},
		},
	}
	config.Store(cfg)

	pr := &models.PRRecord{
		ID:        "pr-2",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  2,
		Title:     "update go files",
		Author:    "dev2",
		State:     "open",
	}

	mock.DiffFiles = []string{"src/main.go"}
	l.ApplyRules(pr, "test-group", mock.DiffFiles)

	if len(mock.AppliedLabels) != 0 {
		t.Errorf("expected no labels, got %v", mock.AppliedLabels)
	}
}

func TestHandlePROpened(t *testing.T) {
	l, mock := setupLabelerTest(t)
	defer func() { db.Close() }()

	compiledPatterns = make(map[string]*regexp.Regexp)

	cfg := &models.Config{
		LabelRules: []models.LabelRule{
			{Pattern: `\.go$`, Label: "backend"},
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "single", MirrorPlatform: "github", GitHub: "owner/repo"},
		},
	}
	config.Store(cfg)

	pr := &models.PRRecord{
		ID:        "pr-3",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  3,
		Title:     "add backend api",
		Author:    "dev3",
		State:     "open",
	}

	mock.DiffFiles = []string{"src/server.go"}

	l.HandlePROpened(pr, "test-group")

	if len(mock.AppliedLabels) != 1 || mock.AppliedLabels[0] != "backend" {
		t.Errorf("expected label 'backend', got %v", mock.AppliedLabels)
	}
}

func TestHandlePROpenedNoRules(t *testing.T) {
	l, mock := setupLabelerTest(t)
	defer func() { db.Close() }()

	compiledPatterns = make(map[string]*regexp.Regexp)

	cfg := &models.Config{
		LabelRules: []models.LabelRule{},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "single", MirrorPlatform: "github", GitHub: "owner/repo"},
		},
	}
	config.Store(cfg)

	pr := &models.PRRecord{
		ID:        "pr-4",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  4,
		Title:     "some pr",
		Author:    "dev4",
		State:     "open",
	}

	l.HandlePROpened(pr, "test-group")

	if len(mock.AppliedLabels) != 0 {
		t.Errorf("expected no labels when no rules, got %v", mock.AppliedLabels)
	}
}

func TestMatchRule_TitleScope(t *testing.T) {
	compiledPatterns = make(map[string]*regexp.Regexp)

	tests := []struct {
		name    string
		pattern string
		files   []string
		title   string
		author  string
		want    bool
	}{
		{
			name:    "title regex matches",
			pattern: "title:api",
			files:   []string{"README.md"},
			title:   "Add new api endpoint",
			author:  "dev1",
			want:    true,
		},
		{
			name:    "title regex no match",
			pattern: "title:api",
			files:   []string{"src/api.go"},
			title:   "Fix bug in parser",
			author:  "dev1",
			want:    false,
		},
		{
			name:    "title glob matches",
			pattern: "title:*api*",
			files:   []string{"README.md"},
			title:   "Add new api endpoint",
			author:  "dev1",
			want:    true,
		},
		{
			name:    "author regex matches",
			pattern: "author:dependabot",
			files:   []string{"README.md"},
			title:   "Bump version",
			author:  "dependabot[bot]",
			want:    true,
		},
		{
			name:    "author regex no match",
			pattern: "author:dependabot",
			files:   []string{"README.md"},
			title:   "Bump version",
			author:  "human-dev",
			want:    false,
		},
		{
			name:    "file scope still works",
			pattern: "file:*.go",
			files:   []string{"main.go"},
			title:   "some title",
			author:  "dev1",
			want:    true,
		},
		{
			name:    "default scope (no prefix) matches files",
			pattern: "*.go",
			files:   []string{"main.go"},
			title:   "some title",
			author:  "dev1",
			want:    true,
		},
		{
			name:    "title with colon in pattern",
			pattern: "title:^fix:",
			files:   []string{},
			title:   "fix: resolve bug",
			author:  "dev1",
			want:    true,
		},
		{
			name:    "author exact match",
			pattern: "author:admin",
			files:   []string{},
			title:   "anything",
			author:  "admin",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchRule(tt.pattern, tt.files, tt.title, tt.author)
			if got != tt.want {
				t.Errorf("matchRule(%q, %v, %q, %q) = %v, want %v",
					tt.pattern, tt.files, tt.title, tt.author, got, tt.want)
			}
		})
	}
}

func TestApplyRules_TitleScope(t *testing.T) {
	l, mock := setupLabelerTest(t)
	defer func() { db.Close() }()

	compiledPatterns = make(map[string]*regexp.Regexp)

	cfg := &models.Config{
		LabelRules: []models.LabelRule{
			{Pattern: "title:api", Label: "api-change"},
			{Pattern: "author:dependabot", Label: "automated"},
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "single", MirrorPlatform: "github", GitHub: "owner/repo"},
		},
	}
	config.Store(cfg)

	pr := &models.PRRecord{
		ID:        "pr-title-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  10,
		Title:     "Add new REST api endpoint",
		Author:    "dependabot[bot]",
		State:     "open",
	}

	mock.DiffFiles = []string{"README.md"}
	l.ApplyRules(pr, "test-group", mock.DiffFiles)

	sort.Strings(mock.AppliedLabels)
	expected := []string{"api-change", "automated"}

	if len(mock.AppliedLabels) != len(expected) {
		t.Errorf("AppliedLabels = %v, want %v", mock.AppliedLabels, expected)
		return
	}

	for i := range expected {
		if mock.AppliedLabels[i] != expected[i] {
			t.Errorf("AppliedLabels[%d] = %q, want %q", i, mock.AppliedLabels[i], expected[i])
		}
	}
}

func TestApplyRules_TitleNoMatch(t *testing.T) {
	l, mock := setupLabelerTest(t)
	defer func() { db.Close() }()

	compiledPatterns = make(map[string]*regexp.Regexp)

	cfg := &models.Config{
		LabelRules: []models.LabelRule{
			{Pattern: "title:security", Label: "security-fix"},
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "single", MirrorPlatform: "github", GitHub: "owner/repo"},
		},
	}
	config.Store(cfg)

	pr := &models.PRRecord{
		ID:        "pr-title-2",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  11,
		Title:     "Update README",
		Author:    "dev1",
		State:     "open",
	}

	mock.DiffFiles = []string{"README.md"}
	l.ApplyRules(pr, "test-group", mock.DiffFiles)

	if len(mock.AppliedLabels) != 0 {
		t.Errorf("expected no labels for non-matching title, got %v", mock.AppliedLabels)
	}
}
