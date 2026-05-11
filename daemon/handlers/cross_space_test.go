package handlers

import (
	"encoding/json"
	"testing"

	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/testutil"
)

func TestGetSpaceForPR(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	_ = testDB
	invalidateSpaceCache()

	space := &models.TeamSpace{
		Name:        "space-a",
		Description: "Team A",
		RepoGroups:  []string{"group-1", "group-2"},
	}
	db.PutTeamSpace(space)

	pr := &models.PRRecord{
		RepoGroup: "group-1",
	}

	s := getSpaceForPR(pr)
	if s != "space-a" {
		t.Errorf("getSpaceForPR = %q, want %q", s, "space-a")
	}

	pr2 := &models.PRRecord{
		RepoGroup: "group-other",
	}
	s2 := getSpaceForPR(pr2)
	if s2 != "" {
		t.Errorf("getSpaceForPR = %q, want empty", s2)
	}
}

func TestNotifyCrossSpaceDeps_NoDeps(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	_ = testDB
	events.Init()
	invalidateSpaceCache()

	pr := &models.PRRecord{
		ID:        "pr-1",
		RepoGroup: "group-1",
	}

	NotifyCrossSpaceDeps(pr)
}

func TestNotifyCrossSpaceDeps_CrossSpace(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	_ = testDB
	events.Init()
	invalidateSpaceCache()

	db.PutTeamSpace(&models.TeamSpace{
		Name:       "space-a",
		RepoGroups: []string{"group-a"},
	})
	db.PutTeamSpace(&models.TeamSpace{
		Name:       "space-b",
		RepoGroups: []string{"group-b"},
	})

	sourcePR := &models.PRRecord{
		ID:        "pr-source",
		RepoGroup: "group-a",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Source PR",
		State:     "merged",
	}
	sourceData, _ := json.Marshal(sourcePR)
	db.PutPRWithIndex("group-a#github#1", sourceData, "pr-source", "group-a", 1)

	depPR := &models.PRRecord{
		ID:        "pr-dep",
		RepoGroup: "group-b",
		Platform:  "github",
		PRNumber:  2,
		Title:     "Dependent PR",
		State:     "open",
	}
	depData, _ := json.Marshal(depPR)
	db.PutPRWithIndex("group-b#github#2", depData, "pr-dep", "group-b", 2)

	db.PutPRDependency(&models.PRDependency{
		PRID:          "pr-dep",
		DependsOnPRID: "pr-source",
	})

	NotifyCrossSpaceDeps(sourcePR)

	depRecordData, err := db.Get(db.BucketCrossSpaceDeps, "pr-source:pr-dep")
	if err != nil {
		t.Fatalf("Failed to get cross-space dep: %v", err)
	}

	var dep CrossSpaceDep
	if err := json.Unmarshal(depRecordData, &dep); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if dep.SourcePRID != "pr-source" {
		t.Errorf("SourcePRID = %q, want %q", dep.SourcePRID, "pr-source")
	}
	if dep.TargetPRID != "pr-dep" {
		t.Errorf("TargetPRID = %q, want %q", dep.TargetPRID, "pr-dep")
	}
	if dep.SourceSpace != "space-a" {
		t.Errorf("SourceSpace = %q, want %q", dep.SourceSpace, "space-a")
	}
	if dep.TargetSpace != "space-b" {
		t.Errorf("TargetSpace = %q, want %q", dep.TargetSpace, "space-b")
	}
}

func TestFindPRByGlobalID(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	_ = testDB

	pr := &models.PRRecord{
		ID:        "global-pr-1",
		RepoGroup: "default",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Test",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("default#github#42", data, "global-pr-1", "default", 42)

	found, err := findPRByGlobalID("global-pr-1")
	if err != nil {
		t.Fatalf("findPRByGlobalID failed: %v", err)
	}
	if found.ID != "global-pr-1" {
		t.Errorf("ID = %q, want %q", found.ID, "global-pr-1")
	}

	_, err = findPRByGlobalID("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent PR")
	}
}
