package handlers

import (
	"net/http"
	"os"
	"runtime"
	"strconv"

	"github.com/gin-gonic/gin"
)

// UsageResponse holds real-time CPU and memory usage
type UsageResponse struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemAllocMB    float64 `json:"mem_alloc_mb"`
	MemTotalMB    float64 `json:"mem_total_mb"`
	MemSysMB      float64 `json:"mem_sys_mb"`
	MemLimitMB    float64 `json:"mem_limit_mb"`
	MemPercent    float64 `json:"mem_percent"`
	Goroutines    int     `json:"goroutines"`
	NumCPU        int     `json:"num_cpu"`
}

// GetUsage handles GET /api/v1/usage
func GetUsage(c *gin.Context) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	allocMB := float64(memStats.Alloc) / 1024 / 1024
	totalMB := float64(memStats.TotalAlloc) / 1024 / 1024
	sysMB := float64(memStats.Sys) / 1024 / 1024

	// GOMEMLIMIT
	var memLimitMB float64
	var memPercent float64
	if limitStr := os.Getenv("GOMEMLIMIT"); limitStr != "" {
		if limit, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
			memLimitMB = float64(limit) / 1024 / 1024
			if memLimitMB > 0 {
				memPercent = (allocMB / memLimitMB) * 100
			}
		}
	}

	cpuPercent := readCPUPercent()

	resp := UsageResponse{
		CPUPercent: cpuPercent,
		MemAllocMB: allocMB,
		MemTotalMB: totalMB,
		MemSysMB:   sysMB,
		MemLimitMB: memLimitMB,
		MemPercent: memPercent,
		Goroutines: runtime.NumGoroutine(),
		NumCPU:     runtime.NumCPU(),
	}

	c.JSON(http.StatusOK, resp)
}

// readCPUPercent reads current process CPU usage percentage from /proc/stat
// Returns 0 if unable to read (e.g., non-Linux)
func readCPUPercent() float64 {
	// Read process CPU time from /proc/self/stat
	statData, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}

	// Parse fields - we need utime (14) and stime (15)
	// Find the last ')' to skip the comm field which may contain spaces
	data := string(statData)
	lastParen := 0
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == ')' {
			lastParen = i
			break
		}
	}
	if lastParen == 0 {
		return 0
	}

	fields := splitFields(data[lastParen+2:])
	// field index: utime=11, stime=13 (0-based from state field)
	if len(fields) < 14 {
		return 0
	}

	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)

	procTotal := utime + stime

	// Read system CPU from /proc/stat
	sysData, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}

	sysFields := splitFields(string(sysData))
	// First line: cpu user nice system idle iowait irq softirq steal
	if len(sysFields) < 8 || sysFields[0] != "cpu" {
		return 0
	}

	var sysTotal uint64
	for i := 1; i < len(sysFields) && i <= 8; i++ {
		v, _ := strconv.ParseUint(sysFields[i], 10, 64)
		sysTotal += v
	}

	if sysTotal == 0 {
		return 0
	}

	// Get number of CPUs for normalization
	numCPU := runtime.NumCPU()
	if numCPU == 0 {
		numCPU = 1
	}

	// CPU percent = (process_total / system_total) * 100 * numCPU
	// This gives the percentage across all CPUs
	percent := (float64(procTotal) / float64(sysTotal)) * 100.0 * float64(numCPU)

	// Clamp to 0-100
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	return percent
}

func splitFields(s string) []string {
	var fields []string
	start := -1
	for i, c := range s {
		if c != ' ' && c != '\t' {
			if start == -1 {
				start = i
			}
		} else if start != -1 {
			fields = append(fields, s[start:i])
			start = -1
		}
	}
	if start != -1 {
		fields = append(fields, s[start:])
	}
	return fields
}
