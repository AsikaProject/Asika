package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"asika/common/auth"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setTestParam(c *gin.Context, key, value string) {
	c.Params = append(c.Params, gin.Param{Key: key, Value: value})
}

func setupSpacesTest(t *testing.T) {
	t.Helper()
	testutil.NewTestDB(t)
	auth.Init("test-secret", 72*3600000000000)
}

func TestCreateSpace_Success(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	body, _ := json.Marshal(map[string]string{"name": "test-space", "description": "A test space"})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")

	CreateSpace(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var space models.TeamSpace
	if err := json.Unmarshal(w.Body.Bytes(), &space); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if space.Name != "test-space" {
		t.Errorf("expected test-space, got %s", space.Name)
	}
	if space.CreatedBy != "admin" {
		t.Errorf("expected admin, got %s", space.CreatedBy)
	}
}

func TestCreateSpace_MissingName(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	body, _ := json.Marshal(map[string]string{"description": "no name"})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateSpace(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSpace_Duplicate(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	body, _ := json.Marshal(map[string]string{"name": "dup-space"})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")

	CreateSpace(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create should succeed, got %d", w.Code)
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(body))
	c2.Request.Header.Set("Content-Type", "application/json")
	c2.Set("username", "admin")

	CreateSpace(c2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("duplicate upsert should succeed, got %d", w2.Code)
	}
}

func TestGetSpace_NotFound(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/v1/spaces/nonexistent", nil)
	setTestParam(c, "name", "nonexistent")

	GetSpace(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetSpace_Success(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	createBody, _ := json.Marshal(map[string]string{"name": "get-test"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(createBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")
	CreateSpace(c)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("GET", "/api/v1/spaces/get-test", nil)
	setTestParam(c2, "name", "get-test")

	GetSpace(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var space models.TeamSpace
	if err := json.Unmarshal(w2.Body.Bytes(), &space); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if space.Name != "get-test" {
		t.Errorf("expected get-test, got %s", space.Name)
	}
}

func TestDeleteSpace_Success(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	createBody, _ := json.Marshal(map[string]string{"name": "del-test"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(createBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")
	CreateSpace(c)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("DELETE", "/api/v1/spaces/del-test", nil)
	setTestParam(c2, "name", "del-test")

	DeleteSpace(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestAddSpaceMember_Success(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	createBody, _ := json.Marshal(map[string]string{"name": "member-test"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(createBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")
	CreateSpace(c)

	memberBody, _ := json.Marshal(map[string]string{"username": "alice", "role": "space_operator"})
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("POST", "/api/v1/spaces/member-test/members", bytes.NewReader(memberBody))
	c2.Request.Header.Set("Content-Type", "application/json")
	setTestParam(c2, "name", "member-test")

	AddSpaceMember(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	members, err := db.GetSpaceMembers("member-test")
	if err != nil {
		t.Fatalf("GetSpaceMembers failed: %v", err)
	}

	found := false
	for _, m := range members {
		if m.Username == "alice" && m.Role == "space_operator" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find alice as space_operator, got %v", members)
	}
}

func TestAddSpaceMember_DefaultRole(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	createBody, _ := json.Marshal(map[string]string{"name": "role-test"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(createBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")
	CreateSpace(c)

	memberBody, _ := json.Marshal(map[string]string{"username": "bob"})
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("POST", "/api/v1/spaces/role-test/members", bytes.NewReader(memberBody))
	c2.Request.Header.Set("Content-Type", "application/json")
	setTestParam(c2, "name", "role-test")

	AddSpaceMember(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	members, _ := db.GetSpaceMembers("role-test")
	if len(members) < 2 {
		t.Fatalf("expected at least 2 members (creator + bob), got %d", len(members))
	}

	found := false
	for _, m := range members {
		if m.Username == "bob" && m.Role == "space_viewer" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected bob with space_viewer role, got %v", members)
	}
}

func TestRemoveSpaceMember_Success(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	createBody, _ := json.Marshal(map[string]string{"name": "rm-test"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(createBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")
	CreateSpace(c)

	memberBody, _ := json.Marshal(map[string]string{"username": "charlie"})
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("POST", "/api/v1/spaces/rm-test/members", bytes.NewReader(memberBody))
	c2.Request.Header.Set("Content-Type", "application/json")
	setTestParam(c2, "name", "rm-test")
	AddSpaceMember(c2)

	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request, _ = http.NewRequest("DELETE", "/api/v1/spaces/rm-test/members/charlie", nil)
	setTestParam(c3, "name", "rm-test")
	setTestParam(c3, "username", "charlie")

	RemoveSpaceMember(c3)

	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestListSpaces(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	for _, name := range []string{"space-a", "space-b"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("username", "admin")
		CreateSpace(c)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/v1/spaces", nil)

	ListSpaces(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var spaces []*models.TeamSpace
	if err := json.Unmarshal(w.Body.Bytes(), &spaces); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(spaces) < 2 {
		t.Errorf("expected at least 2 spaces, got %d", len(spaces))
	}
}

func TestUpdateSpaceRepoGroups(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	createBody, _ := json.Marshal(map[string]string{"name": "rg-test"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/v1/spaces", bytes.NewReader(createBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("username", "admin")
	CreateSpace(c)

	rgBody, _ := json.Marshal(map[string][]string{"repo_groups": {"frontend", "backend"}})
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("PUT", "/api/v1/spaces/rg-test/repo-groups", bytes.NewReader(rgBody))
	c2.Request.Header.Set("Content-Type", "application/json")
	setTestParam(c2, "name", "rg-test")

	UpdateSpaceRepoGroups(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var space models.TeamSpace
	if err := json.Unmarshal(w2.Body.Bytes(), &space); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(space.RepoGroups) != 2 {
		t.Errorf("expected 2 repo groups, got %d", len(space.RepoGroups))
	}
}

func TestGetUserSpaces(t *testing.T) {
	setupSpacesTest(t)
	defer db.Close()

	db.PutSpaceMember("user-space-test", "dave", "space_admin")

	spaces, err := db.GetUserSpaces("dave")
	if err != nil {
		t.Fatalf("GetUserSpaces failed: %v", err)
	}
	if len(spaces) != 1 || spaces[0] != "user-space-test" {
		t.Errorf("expected [user-space-test], got %v", spaces)
	}

	spaces, _ = db.GetUserSpaces("nonexistent")
	if len(spaces) != 0 {
		t.Errorf("expected empty, got %v", spaces)
	}
}
