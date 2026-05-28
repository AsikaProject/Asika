package db

import (
	"testing"

	"asika/common/models"
)

func TestPutTeamSpace_And_GetTeamSpace(t *testing.T) {
	initTestDB(t)

	space := &models.TeamSpace{
		Name:        "frontend-team",
		Description: "Frontend development team",
		CreatedBy:   "admin",
		RepoGroups:  []string{"web-app", "ui-lib"},
	}

	err := PutTeamSpace(space)
	if err != nil {
		t.Fatalf("PutTeamSpace failed: %v", err)
	}

	got, err := GetTeamSpace("frontend-team")
	if err != nil {
		t.Fatalf("GetTeamSpace failed: %v", err)
	}
	if got.Description != space.Description {
		t.Errorf("Description mismatch: got %q, want %q", got.Description, space.Description)
	}
	if len(got.RepoGroups) != 2 {
		t.Errorf("expected 2 repo groups, got %d", len(got.RepoGroups))
	}
}

func TestGetTeamSpace_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetTeamSpace("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListTeamSpaces(t *testing.T) {
	initTestDB(t)

	PutTeamSpace(&models.TeamSpace{Name: "team-a", Description: "Team A"})
	PutTeamSpace(&models.TeamSpace{Name: "team-b", Description: "Team B"})
	PutTeamSpace(&models.TeamSpace{Name: "team-c", Description: "Team C"})

	spaces, err := ListTeamSpaces()
	if err != nil {
		t.Fatalf("ListTeamSpaces failed: %v", err)
	}
	if len(spaces) != 3 {
		t.Errorf("expected 3 spaces, got %d", len(spaces))
	}
}

func TestDeleteTeamSpace(t *testing.T) {
	initTestDB(t)

	PutTeamSpace(&models.TeamSpace{Name: "to-delete"})

	err := DeleteTeamSpace("to-delete")
	if err != nil {
		t.Fatalf("DeleteTeamSpace failed: %v", err)
	}

	_, err = GetTeamSpace("to-delete")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPutSpaceMember_And_GetSpaceMembers(t *testing.T) {
	initTestDB(t)

	err := PutSpaceMember("frontend", "alice", "space_admin")
	if err != nil {
		t.Fatalf("PutSpaceMember failed: %v", err)
	}

	err = PutSpaceMember("frontend", "bob", "space_operator")
	if err != nil {
		t.Fatalf("PutSpaceMember failed: %v", err)
	}

	members, err := GetSpaceMembers("frontend")
	if err != nil {
		t.Fatalf("GetSpaceMembers failed: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}
}

func TestRemoveSpaceMember(t *testing.T) {
	initTestDB(t)

	PutSpaceMember("team-x", "alice", "admin")
	PutSpaceMember("team-x", "bob", "viewer")

	err := RemoveSpaceMember("team-x", "alice")
	if err != nil {
		t.Fatalf("RemoveSpaceMember failed: %v", err)
	}

	members, _ := GetSpaceMembers("team-x")
	if len(members) != 1 {
		t.Errorf("expected 1 member after removal, got %d", len(members))
	}
	if members[0].Username != "bob" {
		t.Errorf("expected bob to remain, got %q", members[0].Username)
	}
}

func TestGetUserSpaces(t *testing.T) {
	initTestDB(t)

	PutSpaceMember("team-a", "alice", "admin")
	PutSpaceMember("team-b", "alice", "viewer")
	PutSpaceMember("team-c", "bob", "admin")

	aliceSpaces, err := GetUserSpaces("alice")
	if err != nil {
		t.Fatalf("GetUserSpaces failed: %v", err)
	}
	if len(aliceSpaces) != 2 {
		t.Errorf("expected 2 spaces for alice, got %d", len(aliceSpaces))
	}

	bobSpaces, err := GetUserSpaces("bob")
	if err != nil {
		t.Fatalf("GetUserSpaces failed: %v", err)
	}
	if len(bobSpaces) != 1 {
		t.Errorf("expected 1 space for bob, got %d", len(bobSpaces))
	}
}

func TestPutSpaceSetting_And_GetSpaceSetting(t *testing.T) {
	initTestDB(t)

	err := PutSpaceSetting("frontend", "auto_approve", []byte("true"))
	if err != nil {
		t.Fatalf("PutSpaceSetting failed: %v", err)
	}

	got, err := GetSpaceSetting("frontend", "auto_approve")
	if err != nil {
		t.Fatalf("GetSpaceSetting failed: %v", err)
	}
	if string(got) != "true" {
		t.Errorf("GetSpaceSetting = %q, want %q", string(got), "true")
	}
}

func TestGetSpaceSetting_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetSpaceSetting("nonexistent", "key")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
