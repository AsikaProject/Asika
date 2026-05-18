package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen int64
	mu       sync.Mutex
}

var visitors sync.Map

func getVisitor(ip string, r rate.Limit, b int) *rate.Limiter {
	v, loaded := visitors.LoadOrStore(ip, &visitor{limiter: rate.NewLimiter(r, b), lastSeen: time.Now().UnixNano()})
	vv := v.(*visitor)
	if loaded {
		vv.mu.Lock()
		vv.lastSeen = time.Now().UnixNano()
		vv.mu.Unlock()
	}
	return vv.limiter
}

func cleanupVisitors(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			visitors.Range(func(key, value interface{}) bool {
				v := value.(*visitor)
				v.mu.Lock()
				ls := v.lastSeen
				v.mu.Unlock()
				if time.Since(time.Unix(0, ls)) > 3*time.Minute {
					visitors.Delete(key)
				}
				return true
			})
		}
	}()
}

var cleanupStarted sync.Once

// RateLimit middleware limits requests per IP.
// r = requests per second, b = burst size.
func RateLimit(r rate.Limit, b int) gin.HandlerFunc {
	cleanupStarted.Do(func() {
		cleanupVisitors(time.Minute)
	})
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := getVisitor(ip, r, b)
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			c.Abort()
			return
		}
		c.Next()
	}
}
