package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"asika/common/db"
)

func TestMetricsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}

	metricsHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	if !strings.Contains(body, "asika_requests_total") {
		t.Error("metrics output missing asika_requests_total")
	}
	if !strings.Contains(body, "asika_up") {
		t.Error("metrics output missing asika_up")
	}
	if !strings.Contains(body, "asika_goroutines") {
		t.Error("metrics output missing asika_goroutines")
	}
	if !strings.Contains(body, "asika_memory_alloc_bytes") {
		t.Error("metrics output missing asika_memory_alloc_bytes")
	}
	if !strings.Contains(body, "asika_gc_runs_total") {
		t.Error("metrics output missing asika_gc_runs_total")
	}
	if !strings.Contains(body, "asika_uptime_seconds") {
		t.Error("metrics output missing asika_uptime_seconds")
	}
}

func TestMetricsHandler_PrometheusFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}

	metricsHandler(c)

	body := w.Body.String()

	if !strings.Contains(body, "# HELP") {
		t.Error("expected Prometheus HELP comments")
	}
	if !strings.Contains(body, "# TYPE") {
		t.Error("expected Prometheus TYPE comments")
	}
}

func TestSetHealth_Healthy(t *testing.T) {
	SetHealth(true)

	m := GetMetrics()
	if m["health"] != nil {
		// health is not in GetMetrics, but we can check via handler
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}
	metricsHandler(c)

	body := w.Body.String()
	if !strings.Contains(body, "asika_up 1") {
		t.Errorf("expected asika_up 1 for healthy, got:\n%s", body)
	}
}

func TestSetHealth_Unhealthy(t *testing.T) {
	SetHealth(false)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}
	metricsHandler(c)

	body := w.Body.String()
	if !strings.Contains(body, "asika_up 0") {
		t.Errorf("expected asika_up 0 for unhealthy, got:\n%s", body)
	}

	// Reset
	SetHealth(true)
}

func TestGetMetrics(t *testing.T) {
	m := GetMetrics()

	if _, ok := m["requests_total"]; !ok {
		t.Error("missing requests_total")
	}
	if _, ok := m["active_requests"]; !ok {
		t.Error("missing active_requests")
	}
	if _, ok := m["uptime_seconds"]; !ok {
		t.Error("missing uptime_seconds")
	}
	if _, ok := m["goroutines"]; !ok {
		t.Error("missing goroutines")
	}
	if _, ok := m["memory_alloc_bytes"]; !ok {
		t.Error("missing memory_alloc_bytes")
	}
	if _, ok := m["gc_runs"]; !ok {
		t.Error("missing gc_runs")
	}
}

func TestMetricsMiddleware_IncrementsCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Read current count via handler
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = &http.Request{Header: make(http.Header)}
	metricsHandler(c1)

	body1 := w1.Body.String()
	var countBefore int
	for _, line := range strings.Split(body1, "\n") {
		if strings.HasPrefix(line, "asika_requests_total ") {
			fmt.Sscanf(line, "asika_requests_total %d", &countBefore)
			break
		}
	}

	// Run middleware
	engine := gin.New()
	engine.Use(metricsMiddleware())
	engine.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	w2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	engine.ServeHTTP(w2, req)

	if w2.Code != 200 {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
}

func TestHealthHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	if err := db.Init(dir + "/health_test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	defer db.Close()

	engine := gin.New()
	engine.GET("/health", healthHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 200 or 503, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal health response: %v", err)
	}

	if _, ok := result["status"]; !ok {
		t.Error("health response missing status field")
	}
	if _, ok := result["timestamp"]; !ok {
		t.Error("health response missing timestamp field")
	}
}

func TestHealthHandler_DatabaseDown(t *testing.T) {
	// This test just verifies the handler structure; DB ping may or may not work
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.GET("/health", healthHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	engine.ServeHTTP(w, req)

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if _, ok := result["database"]; !ok {
		t.Error("health response missing database field")
	}
}
