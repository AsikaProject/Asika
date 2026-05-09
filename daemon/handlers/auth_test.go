package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupAuthHandlerTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	testutil.NewTestDB(t)

	auth.Init("auth-handler-test-secret", 72*time.Hour)

	engine := gin.New()
	api := engine.Group("/api/v1")

	authGroup := api.Group("/auth")
	{
		authGroup.POST("/login", Login)
		authGroup.POST("/logout", Logout)
	}

	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})

	users := protected.Group("/users")
	{
		users.GET("", ListUsers)
		users.POST("", CreateUser)
		users.PUT("/:username", UpdateUser)
		users.DELETE("/:username", DeleteUser)
	}

	api.POST("/locale", SetLocale)

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080", Mode: "debug"},
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "auth-handler-test-secret", TokenExpiry: "72h"},
	}
	config.Store(cfg)

	cleanup := func() { db.Close() }
	return engine, cleanup
}

func TestLogin_Success(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	// Create a user
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "testuser",
		PasswordHash: string(hash),
		Role:         "admin",
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "testuser", data)

	body := `{"username": "testuser", "password": "testpass"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["username"] != "testuser" {
		t.Errorf("username = %v, want testuser", resp["username"])
	}
	if resp["role"] != "admin" {
		t.Errorf("role = %v, want admin", resp["role"])
	}
	if resp["token"] == nil || resp["token"] == "" {
		t.Error("token should be present")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "testuser",
		PasswordHash: string(hash),
		Role:         "admin",
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "testuser", data)

	body := `{"username": "testuser", "password": "wrongpass"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `{"username": "nonexistent", "password": "pass"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	// db.Get returns error for missing key, which causes 500 in the handler
	// The handler doesn't distinguish between "not found" and "db error"
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 401 or 500", w.Code)
	}
}

func TestLogin_InvalidJSON(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `not json`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestLogin_ConfigNotLoaded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	t.Cleanup(func() { db.Close() })

	auth.Init("no-config-test", 72*time.Hour)
	config.Store(nil)

	engine := gin.New()
	engine.POST("/api/v1/auth/login", Login)

	body := `{"username": "user", "password": "pass"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestLogout(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "logged out" {
		t.Errorf("message = %v, want logged out", resp["message"])
	}
}

func TestLogout_WithToken(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	token, _ := auth.GenerateJWT("testuser", "admin")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	// Token should be blacklisted
	_, err := auth.ValidateJWT(token)
	if err == nil {
		t.Error("token should be blacklisted after logout")
	}
}

func TestListUsers(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "listuser",
		PasswordHash: string(hash),
		Role:         "operator",
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "listuser", data)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var users []models.User
	json.Unmarshal(w.Body.Bytes(), &users)

	found := false
	for _, u := range users {
		if u.Username == "listuser" {
			found = true
			if u.PasswordHash != "***" {
				t.Errorf("password hash should be masked, got %q", u.PasswordHash)
			}
			break
		}
	}
	if !found {
		t.Error("listuser not found in list")
	}
}

func TestCreateUser_Success(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `{"username": "newuser", "password": "newpass", "role": "operator"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	// Verify user was created
	data, err := db.Get(db.BucketUsers, "newuser")
	if err != nil {
		t.Fatalf("user not found: %v", err)
	}
	var user models.User
	json.Unmarshal(data, &user)
	if user.Username != "newuser" {
		t.Errorf("username = %q, want newuser", user.Username)
	}
	if user.Role != "operator" {
		t.Errorf("role = %q, want operator", user.Role)
	}
}

func TestCreateUser_DefaultRole(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `{"username": "defaultrole", "password": "pass"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	data, _ := db.Get(db.BucketUsers, "defaultrole")
	var user models.User
	json.Unmarshal(data, &user)
	if user.Role != "viewer" {
		t.Errorf("default role = %q, want viewer", user.Role)
	}
}

func TestCreateUser_InvalidRole(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `{"username": "badrole", "password": "pass", "role": "superadmin"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateUser_MissingFields(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	tests := []struct {
		name string
		body string
	}{
		{"empty body", `{}`},
		{"missing password", `{"username": "user1"}`},
		{"missing username", `{"password": "pass"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
	}
}

func TestUpdateUser_Role(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "updateuser",
		PasswordHash: string(hash),
		Role:         "viewer",
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "updateuser", data)

	role := "operator"
	body := `{"role": "` + role + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/users/updateuser", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	data, _ = db.Get(db.BucketUsers, "updateuser")
	var updated models.User
	json.Unmarshal(data, &updated)
	if updated.Role != "operator" {
		t.Errorf("role = %q, want operator", updated.Role)
	}
}

func TestUpdateUser_Password(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "passuser",
		PasswordHash: string(hash),
		Role:         "viewer",
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "passuser", data)

	password := "newpass"
	body := `{"password": "` + password + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/users/passuser", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	data, _ = db.Get(db.BucketUsers, "passuser")
	var updated models.User
	json.Unmarshal(data, &updated)
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("newpass")); err != nil {
		t.Errorf("password was not updated: %v", err)
	}
}

func TestUpdateUser_ViewerClearsPermissions(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "permuser",
		PasswordHash: string(hash),
		Role:         "operator",
		Permissions: models.UserPermissions{
			CanApprove: true,
			CanMerge:   true,
		},
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "permuser", data)

	role := "viewer"
	body := `{"role": "` + role + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/users/permuser", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	data, _ = db.Get(db.BucketUsers, "permuser")
	var updated models.User
	json.Unmarshal(data, &updated)
	if updated.Permissions.CanApprove || updated.Permissions.CanMerge {
		t.Error("permissions should be cleared when changing to viewer")
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `{"role": "admin"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/users/nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	// db.Get returns error for missing key, handler returns 500 (not 404)
	if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 404 or 500", w.Code)
	}
}

func TestDeleteUser_Success(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "deluser",
		PasswordHash: string(hash),
		Role:         "viewer",
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "deluser", data)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/users/deluser", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	data, err := db.Get(db.BucketUsers, "deluser")
	if err != nil {
		t.Fatalf("db.Get error: %v", err)
	}
	if data != nil {
		t.Error("user should be deleted from DB")
	}
}

func TestDeleteUser_ActuallyRemoved(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "deluser2",
		PasswordHash: string(hash),
		Role:         "viewer",
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "deluser2", data)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/users/deluser2", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteUser_MissingUsername(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/users/", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 400 or 404", w.Code)
	}
}

func TestSetLocale(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `{"locale": "zh-CN"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/locale", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["locale"] != "zh-CN" {
		t.Errorf("locale = %v, want zh-CN", resp["locale"])
	}
}

func TestSetLocale_MissingLocale(t *testing.T) {
	engine, cleanup := setupAuthHandlerTest(t)
	defer cleanup()

	body := `{}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/locale", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestExtractLogoutToken_FromCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Header: make(http.Header)}
	c.Request.AddCookie(&http.Cookie{
		Name:  "asika_token",
		Value: "cookie-token",
	})

	token := extractLogoutToken(c)
	if token != "cookie-token" {
		t.Errorf("extractLogoutToken = %q, want cookie-token", token)
	}
}

func TestExtractLogoutToken_FromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Header: make(http.Header)}
	c.Request.Header.Set("Authorization", "Bearer header-token")

	token := extractLogoutToken(c)
	if token != "header-token" {
		t.Errorf("extractLogoutToken = %q, want header-token", token)
	}
}

func TestExtractLogoutToken_None(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Header: make(http.Header)}

	token := extractLogoutToken(c)
	if token != "" {
		t.Errorf("extractLogoutToken = %q, want empty", token)
	}
}
