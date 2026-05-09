package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/usage", GetUsage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/usage", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp UsageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.CPUPercent < 0 || resp.CPUPercent > 100 {
		t.Errorf("CPUPercent = %f, want 0-100", resp.CPUPercent)
	}
	if resp.MemAllocMB < 0 {
		t.Errorf("MemAllocMB = %f, want >= 0", resp.MemAllocMB)
	}
	if resp.MemTotalMB < 0 {
		t.Errorf("MemTotalMB = %f, want >= 0", resp.MemTotalMB)
	}
	if resp.MemSysMB < 0 {
		t.Errorf("MemSysMB = %f, want >= 0", resp.MemSysMB)
	}
	if resp.Goroutines < 1 {
		t.Errorf("Goroutines = %d, want >= 1", resp.Goroutines)
	}
	if resp.NumCPU < 1 {
		t.Errorf("NumCPU = %d, want >= 1", resp.NumCPU)
	}
	if resp.PID < 1 {
		t.Errorf("PID = %d, want >= 1", resp.PID)
	}
}

func TestGetUsage_WithGOMEMLIMIT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/usage", GetUsage)

	t.Setenv("GOMEMLIMIT", "1073741824") // 1GB

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/usage", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp UsageResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.MemLimitMB != 1024 {
		t.Errorf("MemLimitMB = %f, want 1024", resp.MemLimitMB)
	}
	if resp.MemPercent < 0 || resp.MemPercent > 100 {
		t.Errorf("MemPercent = %f, want 0-100", resp.MemPercent)
	}
}

func TestSplitFields(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a b c", []string{"a", "b", "c"}},
		{"  a  b  ", []string{"a", "b"}},
		{"", nil},
		{"single", []string{"single"}},
		{"a\tb\tc", []string{"a", "b", "c"}},
		{"a  b\tc d", []string{"a", "b", "c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitFields(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitFields(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitFields(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestReadCPUPercent(t *testing.T) {
	// Should return a value >= 0 (may be 0 on non-Linux)
	pct := readCPUPercent()
	if pct < 0 {
		t.Errorf("readCPUPercent() = %f, want >= 0", pct)
	}
	if pct > 100 {
		t.Errorf("readCPUPercent() = %f, want <= 100", pct)
	}
}
