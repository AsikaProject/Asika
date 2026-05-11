package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"asika/common/db"
)

func healthHandler(c *gin.Context) {
	dbHealthy := db.Ping() == nil
	metrics := GetMetrics()
	status := "healthy"
	statusCode := http.StatusOK
	if !dbHealthy {
		status = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, gin.H{
		"status":    status,
		"timestamp": metrics["timestamp"],
		"uptime":    fmt.Sprintf("%.0fs", metrics["uptime_seconds"]),
		"database":  map[string]bool{"connected": dbHealthy},
		"metrics": map[string]interface{}{
			"goroutines": metrics["goroutines"],
			"memory_mb":  float64(metrics["memory_alloc_bytes"].(uint64)) / 1024 / 1024,
		},
	})
}

func manifestHandler(c *gin.Context) {
	c.Header("Content-Type", "application/json")
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{
		"name":             "Asika",
		"short_name":       "Asika",
		"description":      "Asika PR Manager — Git collaboration control center",
		"start_url":        "/dashboard",
		"display":          "standalone",
		"background_color": "#f5f5f5",
		"theme_color":      "#0066cc",
		"orientation":      "any",
		"icons": []gin.H{
			{
				"src":   "/static/icon-192.png",
				"sizes": "192x192",
				"type":  "image/png",
			},
			{
				"src":   "/static/icon-512.png",
				"sizes": "512x512",
				"type":  "image/png",
			},
		},
	})
}

func serviceWorkerHandler(c *gin.Context) {
	c.Header("Content-Type", "application/javascript")
	c.Header("Cache-Control", "no-cache")
	const sw = `
const CACHE_NAME = 'asika-v1';
const urlsToCache = [
  '/',
  '/login',
  '/dashboard'
];

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME).then(cache => cache.addAll(urlsToCache))
  );
  self.skipWaiting();
});

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(keys =>
      Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
    )
  );
  self.clients.claim();
});

self.addEventListener('fetch', event => {
  if (event.request.method !== 'GET') return;
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) return;
  event.respondWith(
    caches.match(event.request).then(cached => {
      const fetchPromise = fetch(event.request).then(response => {
        if (response && response.status === 200 && response.type === 'basic') {
          const clone = response.clone();
          caches.open(CACHE_NAME).then(cache => cache.put(event.request, clone));
        }
        return response;
      }).catch(() => cached);
      return cached || fetchPromise;
    })
  );
});
`
	c.String(http.StatusOK, sw)
}
