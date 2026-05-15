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
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupAPIKeyTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	testutil.NewTestDB(t)

	auth.Init("apikey-test-secret", 72*time.Hour)

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})

	apikeys := protected.Group("/apikeys")
	{
		apikeys.POST("", CreateAPIKey)
		apikeys.GET("", ListAPIKeys)
		apikeys.DELETE("/:id", RevokeAPIKey)
	}

	cleanup := func() { db.Close() }
	return engine, cleanup
}

func TestCreateAPIKey_Success(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	body := `{"name": "ci-cd", "role": "operator"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp apiKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Name != "ci-cd" {
		t.Errorf("Name = %q, want ci-cd", resp.Name)
	}
	if resp.Role != "operator" {
		t.Errorf("Role = %q, want operator", resp.Role)
	}
	if resp.RawKey == "" {
		t.Error("RawKey should be present on creation")
	}
	if !strings.HasPrefix(resp.RawKey, "ak_") {
		t.Errorf("RawKey = %q, want prefix ak_", resp.RawKey)
	}
	if resp.ID == "" {
		t.Error("ID should not be empty")
	}
	if resp.CreatedBy != "admin" {
		t.Errorf("CreatedBy = %q, want admin", resp.CreatedBy)
	}
}

func TestCreateAPIKey_DefaultRole(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	body := `{"name": "default-role"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	var resp apiKeyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Role != "operator" {
		t.Errorf("default Role = %q, want operator", resp.Role)
	}
}

func TestCreateAPIKey_InvalidRole(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	body := `{"name": "bad-role", "role": "superadmin"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateAPIKey_MissingName(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	body := `{"role": "viewer"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateAPIKey_InvalidJSON(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	body := `not json`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateAPIKey_WithPermissions(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	body := `{"name": "perm-key", "role": "operator", "permissions": {"can_approve": true, "can_merge": true}}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	var resp apiKeyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Permissions.CanApprove {
		t.Error("CanApprove should be true")
	}
	if !resp.Permissions.CanMerge {
		t.Error("CanMerge should be true")
	}
}

func TestListAPIKeys_Empty(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/apikeys", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp []apiKeyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 0 {
		t.Errorf("len = %d, want 0", len(resp))
	}
}

func TestListAPIKeys_WithKeys(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	// Create two keys
	for _, name := range []string{"key1", "key2"} {
		body := `{"name": "` + name + `", "role": "viewer"}`
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("failed to create key %s: %d", name, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/apikeys", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp []apiKeyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 2 {
		t.Errorf("len = %d, want 2", len(resp))
	}

	// List should not contain raw keys
	for _, k := range resp {
		if k.RawKey != "" {
			t.Errorf("list response should not contain raw key, got %q", k.RawKey)
		}
	}
}

func TestRevokeAPIKey_Success(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	// Create a key
	body := `{"name": "to-revoke", "role": "viewer"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	var created apiKeyResponse
	json.Unmarshal(w.Body.Bytes(), &created)

	// Revoke it
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/v1/apikeys/"+created.ID, nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	// Verify it's gone
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/apikeys", nil)
	engine.ServeHTTP(w, req)

	var resp []apiKeyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 0 {
		t.Errorf("len = %d, want 0 after revoke", len(resp))
	}
}

func TestRevokeAPIKey_MissingID(t *testing.T) {
	engine, cleanup := setupAPIKeyTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/apikeys/", nil)
	engine.ServeHTTP(w, req)

	// Gin treats trailing slash as missing param
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 400 or 404", w.Code)
	}
}

func TestValidateAPIKey(t *testing.T) {
	testutil.NewTestDB(t)
	t.Cleanup(func() { db.Close() })

	SetHMACSecret("test-hmac-secret")

	// Store a known key
	rawKey := "ak_testkey1234567890abcdef"
	hash, _ := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	apiKey := &models.APIKey{
		ID:      "test-id",
		Name:    "test-key",
		KeyHash: string(hash),
		KeyHMAC: computeAPIKeyHMAC(rawKey),
		Role:    "operator",
	}
	db.PutAPIKey(apiKey)

	t.Run("valid key", func(t *testing.T) {
		result := ValidateAPIKey(rawKey)
		if result == nil {
			t.Fatal("ValidateAPIKey returned nil for valid key")
		}
		if result.ID != "test-id" {
			t.Errorf("ID = %q, want test-id", result.ID)
		}
		if result.Role != "operator" {
			t.Errorf("Role = %q, want operator", result.Role)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		result := ValidateAPIKey("ak_wrongkey")
		if result != nil {
			t.Error("ValidateAPIKey should return nil for invalid key")
		}
	})

	t.Run("empty key", func(t *testing.T) {
		result := ValidateAPIKey("")
		if result != nil {
			t.Error("ValidateAPIKey should return nil for empty key")
		}
	})
}

func TestAPIKeyAuth_Middleware(t *testing.T) {
	testutil.NewTestDB(t)
	t.Cleanup(func() { db.Close() })

	auth.Init("apikey-middleware-test", 72*time.Hour)
	SetHMACSecret("test-hmac-secret-mw")

	// Store a known key
	rawKey := "ak_middleware_test_key"
	hash, _ := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	apiKey := &models.APIKey{
		ID:      "mw-test-id",
		Name:    "mw-test-key",
		KeyHash: string(hash),
		KeyHMAC: computeAPIKeyHMAC(rawKey),
		Role:    "viewer",
	}
	db.PutAPIKey(apiKey)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(APIKeyAuth())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	t.Run("valid API key", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", rawKey)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing API key", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("invalid API key", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "ak_invalid")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})
}

func TestAPIKeyAuth_SkipWhenJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(APIKeyAuth())
	router.GET("/test", func(c *gin.Context) {
		username := c.GetString("username")
		c.String(http.StatusOK, username)
	})

	// When username is already set (by JWT auth), API key auth should skip
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	// Simulate JWT auth having set the username
	router.Use(func(c *gin.Context) {
		c.Set("username", "jwtuser")
		c.Set("role", "admin")
		c.Next()
	})
	// Re-register with the pre-set middleware
	router2 := gin.New()
	router2.Use(func(c *gin.Context) {
		c.Set("username", "jwtuser")
		c.Set("role", "admin")
		c.Next()
	})
	router2.Use(APIKeyAuth())
	router2.GET("/test", func(c *gin.Context) {
		username := c.GetString("username")
		c.String(http.StatusOK, username)
	})

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	router2.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "jwtuser" {
		t.Errorf("username = %q, want jwtuser", w.Body.String())
	}
}

func TestGenerateAPIKey(t *testing.T) {
	key := generateAPIKey()
	if !strings.HasPrefix(key, "ak_") {
		t.Errorf("generateAPIKey() = %q, want prefix ak_", key)
	}
	if len(key) != 67 { // "ak_" + 64 hex chars (32 bytes * 2 hex/byte)
		t.Errorf("generateAPIKey() length = %d, want 67", len(key))
	}
}

func TestGenerateAPIKeyID(t *testing.T) {
	id := generateAPIKeyID()
	if len(id) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("generateAPIKeyID() length = %d, want 16", len(id))
	}
}

func TestCreateAPIKey_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	t.Cleanup(func() { db.Close() })

	auth.Init("unauth-test", 72*time.Hour)

	engine := gin.New()
	api := engine.Group("/api/v1")
	apikeys := api.Group("/apikeys")
	apikeys.POST("", CreateAPIKey)

	body := `{"name": "noauth", "role": "viewer"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
