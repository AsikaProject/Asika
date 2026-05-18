package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

func TestRateLimit_AllowsWithinBurst(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RateLimit(rate.Limit(10), 5))
	engine.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// First 5 requests (burst) should all succeed
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Use a unique IP to avoid shared state from other tests
	engine := gin.New()
	engine.Use(RateLimit(rate.Limit(1), 1))
	engine.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// First request — should pass (burst of 1)
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "10.0.0.99:1234"
	engine.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Second request immediately from same IP — should be rate limited
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.99:1234"
	engine.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", w2.Code)
	}
}

func TestRateLimit_ReturnsErrorJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RateLimit(rate.Limit(1), 1))
	engine.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// Consume the burst
	w1 := httptest.NewRecorder()
	engine.ServeHTTP(w1, httptest.NewRequest("GET", "/test", nil))

	// Next request should be rate limited
	w2 := httptest.NewRecorder()
	engine.ServeHTTP(w2, httptest.NewRequest("GET", "/test", nil))

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w2.Code)
	}

	body := w2.Body.String()
	if body == "" {
		t.Error("expected error response body")
	}
}

func TestRateLimit_DifferentIPsIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RateLimit(rate.Limit(1), 1))
	engine.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// Exhaust burst for one IP
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:1234"
	engine.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:1234"
	engine.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("same IP second request: expected 429, got %d", w2.Code)
	}

	// Different IP should still pass
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.2:1234"
	engine.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("different IP: expected 200, got %d", w3.Code)
	}
}

func TestGetVisitor_ConcurrentSameIP(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter := getVisitor("10.99.99.99", rate.Limit(10), 5)
			if limiter == nil {
				t.Error("getVisitor returned nil")
			}
		}()
	}
	wg.Wait()

	limiter := getVisitor("10.99.99.99", rate.Limit(10), 5)
	if limiter == nil {
		t.Fatal("getVisitor returned nil after concurrent access")
	}
}
