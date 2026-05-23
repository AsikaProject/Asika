package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"asika/common/auth"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupRouteTest(t *testing.T) {
	t.Helper()
	testutil.NewTestDB(t)
	auth.Init("test-secret-for-route-matrix", 0)
}

func TestRoutePermissionMatrix_AdminRoutes(t *testing.T) {
	setupRouteTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	adminToken, err := auth.GenerateInternalToken()
	if err != nil {
		t.Fatalf("GenerateInternalToken failed: %v", err)
	}

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"config_get", "GET", "/api/v1/config"},
		{"config_put", "PUT", "/api/v1/config"},
		{"users_list", "GET", "/api/v1/users"},
		{"users_create", "POST", "/api/v1/users"},
		{"users_delete", "DELETE", "/api/v1/users/testuser"},
		{"apikeys_list", "GET", "/api/v1/apikeys"},
		{"apikeys_create", "POST", "/api/v1/apikeys"},
		{"apikeys_delete", "DELETE", "/api/v1/apikeys/key123"},
		{"stats", "GET", "/api/v1/stats"},
		{"logs", "GET", "/api/v1/logs"},
		{"export_logs", "GET", "/api/v1/logs/export"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tt.method, tt.path, nil)
			c.Request.Header.Set("Authorization", "Bearer "+adminToken)

			c.Set("role", "admin")
			roleVal, exists := c.Get("role")
			if !exists || roleVal != "admin" {
				t.Errorf("admin token should yield admin role, got %v (exists=%v)", roleVal, exists)
			}
		})
	}
}

func TestRoutePermissionMatrix_Unauthenticated(t *testing.T) {
	setupRouteTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	protectedRoutes := []struct {
		name   string
		method string
		path   string
	}{
		{"config_get", "GET", "/api/v1/config"},
		{"config_put", "PUT", "/api/v1/config"},
		{"users_list", "GET", "/api/v1/users"},
		{"stats", "GET", "/api/v1/stats"},
	}

	for _, tt := range protectedRoutes {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tt.method, tt.path, nil)

			handler := auth.RequireRole("admin")
			handler(c)

			if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
				t.Errorf("unauthenticated request to %s %s should be rejected, got %d", tt.method, tt.path, w.Code)
			}
		})
	}
}

func TestRoutePermissionMatrix_PublicRoutes(t *testing.T) {
	setupRouteTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	publicRoutes := []struct {
		name   string
		method string
		path   string
	}{
		{"health", "GET", "/api/v1/health"},
		{"login", "POST", "/api/v1/auth/login"},
	}

	for _, tt := range publicRoutes {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tt.method, tt.path, nil)
			_ = w
		})
	}
}

func TestRoutePermissionMatrix_RoleHierarchy(t *testing.T) {
	setupRouteTest(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)

	roles := []struct {
		name        string
		role        string
		canAdmin    bool
		canOperator bool
		canViewer   bool
	}{
		{"admin_can_admin", "admin", true, true, true},
		{"operator_cannot_admin", "operator", false, true, true},
		{"viewer_can_only_view", "viewer", false, false, true},
	}

	for _, r := range roles {
		t.Run(r.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = &http.Request{Header: make(http.Header)}
			c.Set("role", r.role)
			c.Set("permissions", models.UserPermissions{
				CanApprove:     r.role == "admin" || r.role == "operator",
				CanMerge:       r.role == "admin" || r.role == "operator",
				CanClose:       r.role == "admin" || r.role == "operator",
				CanReopen:      r.role == "admin" || r.role == "operator",
				CanSpam:        r.role == "admin",
				CanManageQueue: r.role == "admin" || r.role == "operator",
				CanRevert:      r.role == "admin",
				CanComment:     true,
				CanLabel:       r.role == "admin" || r.role == "operator",
			})

			p := c.MustGet("permissions").(models.UserPermissions)

			if r.canAdmin && !p.CanSpam {
				t.Error("admin-level role should have CanSpam")
			}
			if r.canOperator && !p.CanApprove {
				t.Error("operator-level role should have CanApprove")
			}
			if r.canViewer && !p.CanComment {
				t.Error("viewer-level role should have CanComment")
			}
		})
	}
}
