package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupAuthTest(t *testing.T) {
	t.Helper()
	Init("middleware-test-secret", 24*time.Hour)
}

func TestExtractToken_FromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Header: make(http.Header)}
	c.Request.Header.Set("Authorization", "Bearer valid-token")

	token := extractToken(c)
	if token != "valid-token" {
		t.Errorf("extractToken = %q, want valid-token", token)
	}
}

func TestExtractToken_NoHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Header: make(http.Header)}

	token := extractToken(c)
	if token != "" {
		t.Errorf("extractToken = %q, want empty", token)
	}
}

func TestExtractToken_InvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "Basic token123"},
		{"single word", "token123"},
		{"empty bearer", "Bearer "},
		{"lowercase bearer", "bearer token123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = &http.Request{Header: make(http.Header)}
			c.Request.Header.Set("Authorization", tt.header)

			token := extractToken(c)
			// "Bearer " with empty token after is still split as len==2, parts[1]==""
			if tt.header == "Bearer " {
				if token != "" {
					t.Errorf("extractToken = %q, want empty", token)
				}
				return
			}
			// lowercase bearer is EqualFold-matched
			if tt.name == "lowercase bearer" {
				if token != "token123" {
					t.Errorf("extractToken = %q, want token123", token)
				}
				return
			}
			if token != "" {
				t.Errorf("extractToken = %q, want empty for %s", token, tt.name)
			}
		})
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	setupAuthTest(t)

	token, err := GenerateJWT("testuser", "admin")
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		username, _ := c.Get("username")
		role, _ := c.Get("role")
		c.String(http.StatusOK, "%s:%s", username, role)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "testuser:admin" {
		t.Errorf("body = %q, want testuser:admin", w.Body.String())
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	setupAuthTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	setupAuthTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_BlacklistedToken(t *testing.T) {
	setupAuthTest(t)

	token, _ := GenerateJWT("testuser", "admin")
	BlacklistToken(token)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for blacklisted token", w.Code)
	}
}

func TestRequireRole_Allowed(t *testing.T) {
	setupAuthTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", "admin")
		c.Next()
	})
	router.Use(RequireRole("operator"))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequireRole_Denied(t *testing.T) {
	setupAuthTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", "viewer")
		c.Next()
	})
	router.Use(RequireRole("admin"))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRequireRole_NoRole(t *testing.T) {
	setupAuthTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequireRole("viewer"))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAnyRole_Match(t *testing.T) {
	setupAuthTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", "operator")
		c.Next()
	})
	router.Use(RequireAnyRole("viewer", "operator"))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequireAnyRole_NoMatch(t *testing.T) {
	setupAuthTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", "viewer")
		c.Next()
	})
	router.Use(RequireAnyRole("admin", "operator"))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestGetCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("username", "alice")
	c.Set("role", "admin")

	username, role := GetCurrentUser(c)
	if username != "alice" {
		t.Errorf("username = %q, want alice", username)
	}
	if role != "admin" {
		t.Errorf("role = %q, want admin", role)
	}
}

func TestGetCurrentUser_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	username, role := GetCurrentUser(c)
	if username != "" {
		t.Errorf("username = %q, want empty", username)
	}
	if role != "" {
		t.Errorf("role = %q, want empty", role)
	}
}

func TestCleanupBlacklist(t *testing.T) {
	Init("cleanup-test-secret", 1*time.Millisecond)

	token, _ := GenerateJWT("user", "viewer")
	BlacklistToken(token)

	// Verify blacklisted
	_, err := ValidateJWT(token)
	if err == nil {
		t.Fatal("token should be blacklisted")
	}

	// Wait for expiry
	time.Sleep(5 * time.Millisecond)

	CleanupBlacklist()

	// After cleanup, the token should be removed from blacklist
	// (though it may still be invalid due to expiry)
	blacklistMu.RLock()
	_, exists := blacklist[token]
	blacklistMu.RUnlock()
	if exists {
		t.Error("blacklisted token should be cleaned up")
	}
}

func TestGenerateInternalToken(t *testing.T) {
	setupAuthTest(t)

	token, err := GenerateInternalToken()
	if err != nil {
		t.Fatalf("GenerateInternalToken failed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}

	claims, err := ValidateJWT(token)
	if err != nil {
		t.Fatalf("ValidateJWT failed for internal token: %v", err)
	}
	if GetUsername(claims) != "bot" {
		t.Errorf("username = %q, want bot", GetUsername(claims))
	}
	if GetUserRole(claims) != "admin" {
		t.Errorf("role = %q, want admin", GetUserRole(claims))
	}
}
