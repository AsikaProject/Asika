package webhook

import (
	"encoding/json"
	"testing"

	"asika/common/events"
)

func TestParseGitHubWebhook_PROpened(t *testing.T) {
	body := `{
		"action": "opened",
		"pull_request": {
			"id": 101,
			"number": 42,
			"title": "Add feature",
			"state": "open",
			"html_url": "https://github.com/org/repo/pull/42",
			"user": {"login": "dev1"},
			"head": {"ref": "feature", "sha": "abc123"},
			"base": {"ref": "main", "sha": "def456"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGitHubWebhook([]byte(body), "mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPROpened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPROpened)
	}
	if pr == nil {
		t.Fatal("pr is nil")
	}
	if pr.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", pr.PRNumber)
	}
	if pr.Title != "Add feature" {
		t.Errorf("Title = %q, want %q", pr.Title, "Add feature")
	}
	if pr.Author != "dev1" {
		t.Errorf("Author = %q, want %q", pr.Author, "dev1")
	}
	if pr.Platform != "github" {
		t.Errorf("Platform = %q, want %q", pr.Platform, "github")
	}
	if pr.RepoGroup != "mygroup" {
		t.Errorf("RepoGroup = %q, want %q", pr.RepoGroup, "mygroup")
	}
}

func TestParseGitHubWebhook_PRClosed(t *testing.T) {
	body := `{
		"action": "closed",
		"pull_request": {
			"id": 102, "number": 55, "title": "Fix bug", "state": "closed",
			"html_url": "https://github.com/org/repo/pull/55",
			"user": {"login": "dev2"},
			"head": {"ref": "fix", "sha": "aaa"},
			"base": {"ref": "main", "sha": "bbb"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGitHubWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRClosed) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRClosed)
	}
	if pr == nil || pr.PRNumber != 55 {
		t.Errorf("pr.PRNumber = %d, want 55", pr.PRNumber)
	}
}

func TestParseGitHubWebhook_PRMerged(t *testing.T) {
	body := `{
		"action": "closed",
		"pull_request": {
			"id": 103, "number": 66, "title": "Merged PR", "state": "closed",
			"html_url": "https://github.com/org/repo/pull/66",
			"user": {"login": "dev3"},
			"merged": true,
			"head": {"ref": "feat", "sha": "ccc"},
			"base": {"ref": "main", "sha": "ddd"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, _, err := parseGitHubWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRMerged) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRMerged)
	}
}

func TestParseGitHubWebhook_PRApproved(t *testing.T) {
	body := `{
		"action": "submitted",
		"review": {
			"state": "approved",
			"user": {"login": "reviewer1"}
		},
		"pull_request": {
			"id": 104, "number": 77, "title": "Approved PR", "state": "open",
			"html_url": "https://github.com/org/repo/pull/77",
			"user": {"login": "author1"},
			"head": {"ref": "feat", "sha": "eee"},
			"base": {"ref": "main", "sha": "fff"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGitHubWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRApproved) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRApproved)
	}
	if pr == nil || pr.PRNumber != 77 {
		t.Errorf("pr.PRNumber = %d, want 77", pr.PRNumber)
	}
}

func TestParseGitHubWebhook_PRReopened(t *testing.T) {
	body := `{
		"action": "reopened",
		"pull_request": {
			"id": 105, "number": 88, "title": "Reopened PR", "state": "open",
			"html_url": "https://github.com/org/repo/pull/88",
			"user": {"login": "dev5"},
			"head": {"ref": "feat", "sha": "ggg"},
			"base": {"ref": "main", "sha": "hhh"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, _, err := parseGitHubWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRReopened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRReopened)
	}
}

func TestParseGitHubWebhook_Labeled(t *testing.T) {
	body := `{
		"action": "labeled",
		"pull_request": {
			"id": 106, "number": 99, "title": "Labeled", "state": "open",
			"html_url": "https://github.com/org/repo/pull/99",
			"user": {"login": "dev6"},
			"head": {"ref": "feat", "sha": "iii"},
			"base": {"ref": "main", "sha": "jjj"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGitHubWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRLabeled) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRLabeled)
	}
	if pr == nil || pr.PRNumber != 99 {
		t.Errorf("pr.PRNumber = %d, want 99", pr.PRNumber)
	}
}

func TestParseGitHubWebhook_InvalidJSON(t *testing.T) {
	_, _, err := parseGitHubWebhook([]byte("not json"), "g1")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseGitHubIssueComment(t *testing.T) {
	body := `{
		"action": "created",
		"issue": {
			"number": 42,
			"title": "Test PR",
			"pull_request": {"url": "https://api.github.com/repos/org/repo/pulls/42"}
		},
		"comment": {
			"body": "/approve",
			"user": {"login": "reviewer1"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGitHubIssueComment([]byte(body), "mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRComment) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRComment)
	}
	if pr == nil {
		t.Fatal("pr is nil")
	}
	if pr.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", pr.PRNumber)
	}
}

func TestParseGitHubIssueComment_NoPR(t *testing.T) {
	body := `{
		"action": "created",
		"issue": {
			"number": 42,
			"title": "Test Issue"
		},
		"comment": {
			"body": "just a comment",
			"user": {"login": "user1"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGitHubIssueComment([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != "" {
		t.Errorf("event type should be empty for non-PR comment, got %q", evt)
	}
	if pr != nil {
		t.Error("pr should be nil for non-PR comment")
	}
}

func TestParseGitLabWebhook_MROpened(t *testing.T) {
	body := `{
		"object_kind": "merge_request",
		"event_type": "merge_request",
		"object_attributes": {
			"id": 201,
			"iid": 10,
			"title": "Add feature",
			"state": "opened",
			"action": "open",
			"target_branch": "main",
			"source_branch": "feature",
			"author_id": 1
		},
		"project": {"path_with_namespace": "org/repo"}
	}`
	evt, pr, err := parseGitLabWebhook([]byte(body), "mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPROpened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPROpened)
	}
	if pr == nil || pr.PRNumber != 10 {
		t.Errorf("pr.PRNumber = %d, want 10", pr.PRNumber)
	}
}

func TestParseGitLabWebhook_MRMerged(t *testing.T) {
	body := `{
		"object_kind": "merge_request",
		"event_type": "merge_request",
		"object_attributes": {
			"id": 202, "iid": 11, "title": "Merged MR", "state": "merged",
			"action": "merge", "target_branch": "main", "source_branch": "feat",
			"author_id": 2
		},
		"project": {"path_with_namespace": "org/repo"}
	}`
	evt, _, err := parseGitLabWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRMerged) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRMerged)
	}
}

func TestParseGitLabWebhook_MRClosed(t *testing.T) {
	body := `{
		"object_kind": "merge_request",
		"event_type": "merge_request",
		"object_attributes": {
			"id": 203, "iid": 12, "title": "Closed MR", "state": "closed",
			"action": "close", "target_branch": "main", "source_branch": "feat",
			"author_id": 3
		},
		"project": {"path_with_namespace": "org/repo"}
	}`
	evt, _, err := parseGitLabWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRClosed) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRClosed)
	}
}

func TestParseGitLabWebhook_MRReopened(t *testing.T) {
	body := `{
		"object_kind": "merge_request",
		"event_type": "merge_request",
		"object_attributes": {
			"id": 204, "iid": 13, "title": "Reopened MR", "state": "reopened",
			"action": "reopen", "target_branch": "main", "source_branch": "feat",
			"author_id": 4
		},
		"project": {"path_with_namespace": "org/repo"}
	}`
	evt, _, err := parseGitLabWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRReopened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRReopened)
	}
}

func TestParseGitLabWebhook_WIP(t *testing.T) {
	body := `{
		"object_kind": "merge_request",
		"event_name": "merge_request",
		"object_attributes": {
			"id": 205, "iid": 14, "title": "WIP: Draft feature", "state": "opened",
			"action": "open", "target_branch": "main", "source_branch": "feat"
		},
		"user": {"username": "dev1"},
		"project": {"path_with_namespace": "org/repo"}
	}`
	_, pr, err := parseGitLabWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil || !pr.IsDraft {
		t.Error("expected IsDraft=true for WIP title")
	}
}

func TestParseGitLabWebhook_Draft(t *testing.T) {
	body := `{
		"object_kind": "merge_request",
		"event_name": "merge_request",
		"object_attributes": {
			"id": 206, "iid": 15, "title": "Draft: My feature", "state": "opened",
			"action": "open", "target_branch": "main", "source_branch": "feat"
		},
		"user": {"username": "dev1"},
		"project": {"path_with_namespace": "org/repo"}
	}`
	_, pr, err := parseGitLabWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil || !pr.IsDraft {
		t.Error("expected IsDraft=true for Draft title")
	}
}

func TestParseGitLabWebhook_InvalidJSON(t *testing.T) {
	_, _, err := parseGitLabWebhook([]byte("not json"), "g1")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseGitLabWebhook_NotMergeRequest(t *testing.T) {
	body := `{"object_kind": "push"}`
	evt, pr, err := parseGitLabWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != "" {
		t.Errorf("event type = %q, want empty", evt)
	}
	if pr == nil || pr.PRNumber != 0 {
		t.Error("expected pr with PRNumber=0 for non-MR event")
	}
}

func TestParseGitLabNoteWebhook(t *testing.T) {
	body := `{
		"object_kind": "note",
		"event_type": "note",
		"object_attributes": {
			"id": 1,
			"note": "/approve",
			"noteable_type": "MergeRequest"
		},
		"merge_request": {
			"id": 301,
			"iid": 20,
			"title": "Test MR",
			"state": "opened"
		},
		"project": {"path_with_namespace": "org/repo"},
		"user": {"name": "reviewer1"}
	}`
	evt, pr, err := parseGitLabNoteWebhook([]byte(body), "mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRComment) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRComment)
	}
	if pr == nil || pr.PRNumber != 20 {
		t.Errorf("pr.PRNumber = %d, want 20", pr.PRNumber)
	}
}

func TestParseGitLabNoteWebhook_NotMR(t *testing.T) {
	body := `{
		"object_kind": "note",
		"event_type": "note",
		"object_attributes": {
			"id": 1, "note": "comment", "noteable_type": "Issue"
		},
		"project": {"path_with_namespace": "org/repo"},
		"user": {"name": "user1"}
	}`
	evt, pr, err := parseGitLabNoteWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != "" || pr != nil {
		t.Error("expected empty event and nil pr for non-MR note")
	}
}

func TestParseGiteaWebhook_PROpened(t *testing.T) {
	body := `{
		"action": "opened",
		"number": 30,
		"pull_request": {
			"title": "Gitea PR", "state": "open",
			"poster": {"login": "dev1"}
		},
		"repository": {"full_name": "org/repo"},
		"sender": {"login": "dev1"}
	}`
	evt, pr, err := parseGiteaWebhook([]byte(body), "mygroup", "gitea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPROpened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPROpened)
	}
	if pr == nil || pr.PRNumber != 30 {
		t.Errorf("pr.PRNumber = %d, want 30", pr.PRNumber)
	}
	if pr.Platform != "gitea" {
		t.Errorf("Platform = %q, want %q", pr.Platform, "gitea")
	}
}

func TestParseGiteaWebhook_PRClosed(t *testing.T) {
	body := `{
		"action": "closed",
		"number": 31,
		"pull_request": {
			"title": "Closed Gitea PR", "state": "closed",
			"poster": {"login": "dev2"}
		},
		"repository": {"full_name": "org/repo"},
		"sender": {"login": "dev2"}
	}`
	evt, _, err := parseGiteaWebhook([]byte(body), "g1", "gitea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRClosed) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRClosed)
	}
}

func TestParseGiteaWebhook_PRMerged(t *testing.T) {
	body := `{
		"action": "closed",
		"number": 32,
		"pull_request": {
			"title": "Merged Gitea PR", "state": "closed", "merged": true,
			"poster": {"login": "dev3"}
		},
		"repository": {"full_name": "org/repo"},
		"sender": {"login": "dev3"}
	}`
	evt, _, err := parseGiteaWebhook([]byte(body), "g1", "gitea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRMerged) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRMerged)
	}
}

func TestParseGiteaWebhook_PRReopened(t *testing.T) {
	body := `{
		"action": "reopened",
		"number": 33,
		"pull_request": {
			"title": "Reopened Gitea PR", "state": "open",
			"poster": {"login": "dev4"}
		},
		"repository": {"full_name": "org/repo"},
		"sender": {"login": "dev4"}
	}`
	evt, _, err := parseGiteaWebhook([]byte(body), "g1", "gitea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRReopened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRReopened)
	}
}

func TestParseGiteaWebhook_Draft(t *testing.T) {
	body := `{
		"action": "opened",
		"number": 34,
		"pull_request": {
			"title": "Draft: WIP feature", "state": "open", "draft": true,
			"poster": {"login": "dev5"}
		},
		"repository": {"full_name": "org/repo"},
		"sender": {"login": "dev5"}
	}`
	_, pr, err := parseGiteaWebhook([]byte(body), "g1", "gitea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil || !pr.IsDraft {
		t.Error("expected IsDraft=true")
	}
}

func TestParseGiteaWebhook_InvalidJSON(t *testing.T) {
	_, _, err := parseGiteaWebhook([]byte("not json"), "g1", "gitea")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseGiteaIssueCommentWebhook(t *testing.T) {
	body := `{
		"action": "created",
		"issue": {
			"number": 30,
			"title": "Test PR",
			"pull_request": {"url": "https://gitea.com/org/repo/pulls/30"}
		},
		"comment": {
			"body": "/approve",
			"user": {"login": "reviewer1"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGiteaIssueCommentWebhook([]byte(body), "mygroup", "gitea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRComment) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRComment)
	}
	if pr == nil || pr.PRNumber != 30 {
		t.Errorf("pr.PRNumber = %d, want 30", pr.PRNumber)
	}
}

func TestParseGiteaIssueCommentWebhook_NoPR(t *testing.T) {
	body := `{
		"action": "created",
		"issue": {"number": 30, "title": "Test Issue"},
		"comment": {"body": "comment", "user": {"login": "user1"}},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseGiteaIssueCommentWebhook([]byte(body), "g1", "gitea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != "" || pr != nil {
		t.Error("expected empty event and nil pr for non-PR comment")
	}
}

func TestParseBitbucketWebhook_Comment(t *testing.T) {
	body := `{
		"comment": {
			"content": {"raw": "/approve"},
			"user": {"display_name": "reviewer1"}
		},
		"pullrequest": {
			"id": 50,
			"title": "Test PR",
			"state": "OPEN",
			"author": {"display_name": "dev1"}
		},
		"repository": {"full_name": "org/repo"},
		"actor": {"display_name": "reviewer1"}
	}`
	evt, pr, err := parseBitbucketWebhook([]byte(body), "mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRComment) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRComment)
	}
	if pr == nil {
		t.Fatal("pr is nil")
	}
	if pr.PRNumber != 50 {
		t.Errorf("PRNumber = %d, want 50", pr.PRNumber)
	}
	if pr.Title != "Test PR" {
		t.Errorf("Title = %q, want %q", pr.Title, "Test PR")
	}
	if pr.Author != "dev1" {
		t.Errorf("Author = %q, want %q", pr.Author, "dev1")
	}
	if pr.State != "open" {
		t.Errorf("State = %q, want %q", pr.State, "open")
	}
	if pr.Platform != "bitbucket" {
		t.Errorf("Platform = %q, want %q", pr.Platform, "bitbucket")
	}
}

func TestParseBitbucketWebhook_NoComment(t *testing.T) {
	body := `{
		"comment": {
			"content": {"raw": ""},
			"user": {"display_name": "user1"}
		},
		"pullrequest": {
			"id": 51, "title": "Test", "state": "OPEN",
			"author": {"display_name": "dev1"}
		},
		"repository": {"full_name": "org/repo"}
	}`
	evt, pr, err := parseBitbucketWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != "" || pr != nil {
		t.Error("expected empty event and nil pr for empty comment")
	}
}

func TestParseBitbucketWebhook_InvalidJSON(t *testing.T) {
	_, _, err := parseBitbucketWebhook([]byte("not json"), "g1")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseGerritWebhook_PatchsetCreated(t *testing.T) {
	body := `{
		"type": "patchset-created",
		"change": {
			"project": "my-project",
			"branch": "main",
			"id": "Iabc123",
			"number": 100,
			"subject": "Add feature",
			"status": "NEW",
			"url": "https://gerrit.example.com/c/100",
			"owner": {"name": "dev1"}
		},
		"patchSet": {"number": 1, "revision": "abc123"}
	}`
	evt, pr, err := parseGerritWebhook([]byte(body), "mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPROpened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPROpened)
	}
	if pr == nil || pr.PRNumber != 100 {
		t.Errorf("pr.PRNumber = %d, want 100", pr.PRNumber)
	}
	if pr.Title != "Add feature" {
		t.Errorf("Title = %q, want %q", pr.Title, "Add feature")
	}
}

func TestParseGerritWebhook_ChangeMerged(t *testing.T) {
	body := `{
		"type": "change-merged",
		"change": {
			"project": "my-project",
			"branch": "main",
			"id": "Iabc456",
			"number": 101,
			"subject": "Merged change",
			"status": "MERGED",
			"url": "https://gerrit.example.com/c/101",
			"owner": {"name": "dev2"}
		},
		"patchSet": {"number": 2, "revision": "def456"}
	}`
	evt, _, err := parseGerritWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRMerged) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRMerged)
	}
}

func TestParseGerritWebhook_ChangeAbandoned(t *testing.T) {
	body := `{
		"type": "change-abandoned",
		"change": {
			"project": "my-project",
			"branch": "main",
			"id": "Iabc789",
			"number": 102,
			"subject": "Abandoned change",
			"status": "ABANDONED",
			"url": "https://gerrit.example.com/c/102",
			"owner": {"name": "dev3"}
		},
		"patchSet": {"number": 1, "revision": "ghi789"}
	}`
	evt, _, err := parseGerritWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRClosed) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRClosed)
	}
}

func TestParseGerritWebhook_ChangeRestored(t *testing.T) {
	body := `{
		"type": "change-restored",
		"change": {
			"project": "my-project",
			"branch": "main",
			"id": "Iabc000",
			"number": 103,
			"subject": "Restored change",
			"status": "NEW",
			"url": "https://gerrit.example.com/c/103",
			"owner": {"name": "dev4"}
		},
		"patchSet": {"number": 3, "revision": "jkl000"}
	}`
	evt, _, err := parseGerritWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRReopened) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRReopened)
	}
}

func TestParseGerritWebhook_CommentAdded(t *testing.T) {
	body := `{
		"type": "comment-added",
		"change": {
			"project": "my-project",
			"branch": "main",
			"id": "Iabc111",
			"number": 104,
			"subject": "Commented change",
			"status": "NEW",
			"url": "https://gerrit.example.com/c/104",
			"owner": {"name": "dev5"}
		},
		"comment": "/approve",
		"patchSet": {"number": 1, "revision": "mno111"},
		"author": {"name": "reviewer1"}
	}`
	evt, pr, err := parseGerritWebhook([]byte(body), "mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPRComment) {
		t.Errorf("event type = %q, want %q", evt, events.EventPRComment)
	}
	if pr == nil || pr.PRNumber != 104 {
		t.Errorf("pr.PRNumber = %d, want 104", pr.PRNumber)
	}
}

func TestParseGerritWebhook_Draft(t *testing.T) {
	body := `{
		"type": "patchset-created",
		"change": {
			"project": "my-project",
			"branch": "main",
			"id": "Iabc222",
			"number": 105,
			"subject": "Draft change",
			"status": "DRAFT",
			"url": "https://gerrit.example.com/c/105",
			"owner": {"name": "dev6"}
		},
		"patchSet": {"number": 1, "revision": "pqr222"}
	}`
	_, pr, err := parseGerritWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil || !pr.IsDraft {
		t.Error("expected IsDraft=true for DRAFT status")
	}
}

func TestParseGerritWebhook_InvalidJSON(t *testing.T) {
	_, _, err := parseGerritWebhook([]byte("not json"), "g1")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseGerritWebhook_UnknownType(t *testing.T) {
	body := `{"type": "unknown-event"}`
	evt, pr, err := parseGerritWebhook([]byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != "" || pr != nil {
		t.Error("expected empty event and nil pr for unknown type")
	}
}

func TestExtractCommentPayload_GitHub(t *testing.T) {
	body := map[string]interface{}{
		"comment": map[string]interface{}{
			"body": "/approve",
			"user": map[string]interface{}{"login": "reviewer1"},
		},
	}
	payload := extractCommentPayload("github", mustMarshal(body))
	if payload == nil {
		t.Fatal("payload is nil")
	}
	if payload.CommentBody != "/approve" {
		t.Errorf("CommentBody = %q, want %q", payload.CommentBody, "/approve")
	}
	if payload.CommentAuthor != "reviewer1" {
		t.Errorf("CommentAuthor = %q, want %q", payload.CommentAuthor, "reviewer1")
	}
}

func TestExtractCommentPayload_GitLab(t *testing.T) {
	body := map[string]interface{}{
		"object_attributes": map[string]interface{}{
			"note": "/merge",
		},
		"user": map[string]interface{}{"username": "operator1"},
	}
	payload := extractCommentPayload("gitlab", mustMarshal(body))
	if payload == nil {
		t.Fatal("payload is nil")
	}
	if payload.CommentBody != "/merge" {
		t.Errorf("CommentBody = %q, want %q", payload.CommentBody, "/merge")
	}
	if payload.CommentAuthor != "operator1" {
		t.Errorf("CommentAuthor = %q, want %q", payload.CommentAuthor, "operator1")
	}
}

func TestExtractCommentPayload_Gitea(t *testing.T) {
	body := map[string]interface{}{
		"comment": map[string]interface{}{
			"body": "/close",
			"user": map[string]interface{}{"login": "admin1"},
		},
	}
	payload := extractCommentPayload("gitea", mustMarshal(body))
	if payload == nil {
		t.Fatal("payload is nil")
	}
	if payload.CommentBody != "/close" {
		t.Errorf("CommentBody = %q, want %q", payload.CommentBody, "/close")
	}
	if payload.CommentAuthor != "admin1" {
		t.Errorf("CommentAuthor = %q, want %q", payload.CommentAuthor, "admin1")
	}
}

func TestExtractCommentPayload_Bitbucket(t *testing.T) {
	body := map[string]interface{}{
		"comment": map[string]interface{}{
			"content": map[string]interface{}{"raw": "/spam"},
		},
		"actor": map[string]interface{}{"display_name": "mod1"},
	}
	payload := extractCommentPayload("bitbucket", mustMarshal(body))
	if payload == nil {
		t.Fatal("payload is nil")
	}
	if payload.CommentBody != "/spam" {
		t.Errorf("CommentBody = %q, want %q", payload.CommentBody, "/spam")
	}
	if payload.CommentAuthor != "mod1" {
		t.Errorf("CommentAuthor = %q, want %q", payload.CommentAuthor, "mod1")
	}
}

func TestExtractCommentPayload_UnsupportedPlatform(t *testing.T) {
	body := map[string]interface{}{"foo": "bar"}
	payload := extractCommentPayload("gerrit", mustMarshal(body))
	if payload != nil {
		t.Error("expected nil for unsupported platform")
	}
}

func TestExtractCommentPayload_InvalidJSON(t *testing.T) {
	payload := extractCommentPayload("github", []byte("not json"))
	if payload != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestParseWebhookEvent_Dispatch(t *testing.T) {
	body := `{
		"action": "opened",
		"pull_request": {
			"id": 1, "number": 1, "title": "Test", "state": "open",
			"html_url": "https://github.com/o/r/pull/1",
			"user": {"login": "a"},
			"head": {"ref": "f", "sha": "s"},
			"base": {"ref": "m", "sha": "b"}
		},
		"repository": {"full_name": "o/r"}
	}`
	evt, pr, err := parseWebhookEvent("github", []byte(body), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPROpened) {
		t.Errorf("event = %q, want %q", evt, events.EventPROpened)
	}
	if pr == nil || pr.PRNumber != 1 {
		t.Error("pr.PRNumber != 1")
	}
}

func TestParseWebhookEvent_UnsupportedPlatform(t *testing.T) {
	evt, pr, err := parseWebhookEvent("unknown", []byte("{}"), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != "" || pr != nil {
		t.Error("expected empty event and nil pr for unsupported platform")
	}
}

func TestProcessWebhook_ParsesAndPublishes(t *testing.T) {
	events.Init()
	body := `{
		"action": "opened",
		"pull_request": {
			"id": 1, "number": 99, "title": "Process test", "state": "open",
			"html_url": "https://github.com/o/r/pull/99",
			"user": {"login": "author"},
			"head": {"ref": "f", "sha": "s"},
			"base": {"ref": "m", "sha": "b"}
		},
		"repository": {"full_name": "o/r"}
	}`
	evt, pr, err := ProcessWebhook("github", "g1", []byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != string(events.EventPROpened) {
		t.Errorf("event = %q, want %q", evt, events.EventPROpened)
	}
	if pr == nil || pr.PRNumber != 99 {
		t.Error("pr.PRNumber != 99")
	}
}

func mustMarshal(v map[string]interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
