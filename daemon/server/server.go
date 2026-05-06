package server

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/handlers"
	"asika/daemon/templates"
)

// Server represents the HTTP server
type Server struct {
	engine   *gin.Engine
	httpSrv  *http.Server
	cfg      *models.Config
	clients  map[platforms.PlatformType]platforms.PlatformClient
}

// NewServer creates a new server instance
func NewServer(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Server {
	if cfg != nil && cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	// Load HTML templates from embedded FS
	t, err := template.ParseFS(templates.FS, "*.html")
	if err != nil {
		panic(fmt.Sprintf("failed to parse templates: %v", err))
	}
	engine.SetHTMLTemplate(t)

	s := &Server{
		cfg:     cfg,
		engine:  engine,
		clients: clients,
	}

	// Initialize handlers with clients
	handlers.InitClients(clients)

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// initCheckMiddleware redirects to wizard if not initialized
func initCheckMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		skip := false
		skipPaths := []string{"/api/v1/auth", "/api/v1/wizard", "/api/v1/feishu", "/login", "/wizard", "/health", "/metrics"}
		for _, p := range skipPaths {
			if strings.HasPrefix(path, p) {
				skip = true
				break
			}
		}
		if path == "/health" || path == "/metrics" || skip {
			c.Next()
			return
		}

		if config.Current() == nil {
			if strings.HasPrefix(path, "/api/") {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": "server not initialized",
					"code":  503,
				})
			} else {
				c.Redirect(http.StatusFound, "/wizard")
			}
			c.Abort()
			return
		}

		c.Next()
	}
}

// setupMiddleware configures middleware
func (s *Server) setupMiddleware() {
	s.engine.Use(initCheckMiddleware())
	s.engine.Use(Logger())
	s.engine.Use(gin.Recovery())
	s.engine.Use(metricsMiddleware())
	s.engine.Use(CORS(s.cfg.Server.CORSOrigins))
	if s.cfg.Server.RateLimitEnabled {
		s.engine.Use(RateLimit(rate.Limit(s.cfg.Server.RateLimitRPS), s.cfg.Server.RateLimitBurst))
	}
	s.engine.Use(AuthMiddleware())
}

// setupRoutes configures routes according to tasks.md section 8
func (s *Server) setupRoutes() {
	s.engine.GET("/health", healthHandler)
	s.engine.GET("/metrics", metricsHandler)

	// pprof debug endpoints (no auth, only in debug mode)
	if s.cfg != nil && s.cfg.Server.EnablePprof {
		debug := s.engine.Group("/debug/pprof")
		debug.GET("/", gin.WrapF(pprof.Index))
		debug.GET("/cmdline", gin.WrapF(pprof.Cmdline))
		debug.GET("/profile", gin.WrapF(pprof.Profile))
		debug.POST("/symbol", gin.WrapF(pprof.Symbol))
		debug.GET("/symbol", gin.WrapF(pprof.Symbol))
		debug.GET("/trace", gin.WrapF(pprof.Trace))
		debug.GET("/allocs", gin.WrapF(pprof.Handler("allocs").ServeHTTP))
		debug.GET("/block", gin.WrapF(pprof.Handler("block").ServeHTTP))
		debug.GET("/goroutine", gin.WrapF(pprof.Handler("goroutine").ServeHTTP))
		debug.GET("/heap", gin.WrapF(pprof.Handler("heap").ServeHTTP))
		debug.GET("/mutex", gin.WrapF(pprof.Handler("mutex").ServeHTTP))
		debug.GET("/threadcreate", gin.WrapF(pprof.Handler("threadcreate").ServeHTTP))
	}

	api := s.engine.Group("/api/v1")

	// Authentication routes (8.1)
	auth := api.Group("/auth")
	{
		auth.POST("/login", handlers.Login)
		auth.POST("/logout", handlers.Logout)
	}

	// Wizard routes (10. WebUI Wizard)
	wizard := api.Group("/wizard")
	{
		wizard.GET("", handlers.GetWizardSteps)
		wizard.POST("/step/complete", handlers.CompleteWizard)
		wizard.POST("/step/:step", handlers.SubmitWizardStep)
	}

	// Protected routes (require JWT)
	protected := api.Group("")
	protected.Use(RequireAuth())
	{
		// User management (8.1)
		users := protected.Group("/users")
		users.Use(RequireRole("admin"))
		{
			users.GET("", handlers.ListUsers)
			users.POST("", handlers.CreateUser)
			users.DELETE("/:username", handlers.DeleteUser)
		}

		// PR management (8.2)
		prs := protected.Group("/repos/:repo_group/prs")
		prs.Use(RequireAnyRole("viewer", "operator", "admin"))
		prs.Use(RequireRepoGroupAccess())
		{
			prs.GET("", handlers.ListPRs)
			prs.GET("/:pr_id", handlers.GetPR)
			prs.POST("/:pr_id/approve", handlers.ApprovePR)
			prs.POST("/:pr_id/close", handlers.ClosePR)
			prs.POST("/:pr_id/reopen", handlers.ReopenPR)
			prs.POST("/:pr_id/spam", handlers.MarkSpam)
			prs.POST("/:pr_id/comment", handlers.CommentPR)
			prs.POST("/:pr_id/rebase", handlers.RebaseSinglePR)
			prs.POST("/batch/approve", handlers.BatchApprovePR)
			prs.POST("/batch/close", handlers.BatchClosePR)
			prs.POST("/batch/label", handlers.BatchLabelPR)
		}

		// Queue management (8.3)
		queue := protected.Group("/queue/:repo_group")
		queue.Use(RequireAnyRole("viewer", "operator", "admin"))
		queue.Use(RequireRepoGroupAccess())
		{
			queue.GET("", handlers.GetQueue)
			queue.POST("/recheck", handlers.RecheckQueue)
			queue.POST("/rebase", handlers.RebaseQueue)
		}

		// Audit logs (8.2)
		logs := protected.Group("/logs")
		logs.Use(RequireAnyRole("viewer", "operator", "admin"))
		{
			logs.GET("", handlers.GetLogs)
			logs.GET("/export", handlers.ExportLogs)
		}

		// Config management (8.4)
		cfgGroup := protected.Group("/config")
		cfgGroup.Use(RequireRole("admin"))
		{
			cfgGroup.GET("", handlers.GetConfig)
			cfgGroup.PUT("", handlers.UpdateConfig)
		}

		// Stats / DORA metrics
		protected.GET("/stats", handlers.GetStats)

		// Sync history (8.5)
		sync := protected.Group("/sync")
		sync.Use(RequireAnyRole("viewer", "operator", "admin"))
		{
			sync.GET("/history", handlers.GetSyncHistory)
			sync.POST("/retry/:sync_id", handlers.RetrySync)
		}

		// Test notification (8.6)
		test := protected.Group("/test")
		test.Use(RequireRole("admin"))
		{
			test.POST("/notify", handlers.TestNotify)
		}

		// Self-update (admin only)
		update := protected.Group("/self-update")
		update.Use(RequireRole("admin"))
		{
			update.GET("/check", handlers.CheckForUpdate)
			update.GET("/run", handlers.PerformWebUpdate)
		}

		// Stale PR management (admin only)
		staleGroup := protected.Group("/stale")
		staleGroup.Use(RequireRole("admin"))
		{
			staleGroup.POST("/check", handlers.HandleStaleCheck)
			staleGroup.POST("/check/:repo_group", handlers.HandleStaleCheck)
			staleGroup.POST("/unmark/:repo_group/:pr_number", handlers.HandleStaleUnmark)
		}
	}

	// WebUI routes - server-rendered (SSR) per tasks.md 2.3
	s.engine.GET("/", func(c *gin.Context) {
		if config.Current() != nil {
			c.Redirect(http.StatusFound, "/login")
		} else {
			c.Redirect(http.StatusFound, "/wizard")
		}
	})

	s.engine.GET("/wizard", func(c *gin.Context) {
		c.HTML(http.StatusOK, "wizard.html", gin.H{"title": "Setup Wizard - Asika"})
	})

	s.engine.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", gin.H{"title": "Login - Asika"})
	})

	ssr := s.engine.Group("")
	ssr.Use(SSRAuthRequired())
	{
		ssr.GET("/dashboard", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "dashboard.html", gin.H{
				"title":    "Dashboard - Asika",
				"username": user,
			})
		})

		ssr.GET("/prs", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "pr_list.html", gin.H{
				"title":    "PRs - Asika",
				"username": user,
			})
		})

		ssr.GET("/repos/:repo_group/prs/:pr_id", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "pr_detail.html", gin.H{
				"title":      "PR Detail - Asika",
				"username":   user,
				"repo_group": c.Param("repo_group"),
				"pr_id":      c.Param("pr_id"),
			})
		})

		ssr.GET("/queue", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "queue.html", gin.H{
				"title":    "Queue - Asika",
				"username": user,
			})
		})

		ssr.GET("/users", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "users.html", gin.H{
				"title":    "Users - Asika",
				"username": user,
			})
		})

		ssr.GET("/config", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "config.html", gin.H{
				"title":    "Config - Asika",
				"username": user,
			})
		})
	}

	// Webhook routes (no auth)
	s.engine.POST("/webhook/:repo_group/:platform", handlers.WebhookHandler)

	// Feishu event callback (no auth, validated by feishu's verification token)
	s.engine.POST("/api/v1/feishu/event", handlers.FeishuEventHandler)
}

// Start starts the server
func (s *Server) Start() error {
	var addr string
	if s.cfg == nil {
		addr = ":8080"
	} else {
		addr = s.cfg.Server.Listen
	}

	readTimeout := time.Duration(s.cfg.Server.ReadTimeoutSeconds) * time.Second
	writeTimeout := time.Duration(s.cfg.Server.WriteTimeoutSeconds) * time.Second
	if readTimeout == 0 {
		readTimeout = 30 * time.Second
	}
	if writeTimeout == 0 {
		writeTimeout = 30 * time.Second
	}

	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s.engine,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	slog.Info("starting server", "listen", addr)
	return s.httpSrv.ListenAndServe()
}

// Stop stops the server gracefully
func (s *Server) Stop() error {
	if s.httpSrv == nil {
		return nil
	}
	timeout := time.Duration(s.cfg.Server.ShutdownTimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpSrv.Shutdown(ctx)
}

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
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    fmt.Sprintf("%.0fs", metrics["uptime_seconds"]),
		"database":  map[string]bool{"connected": dbHealthy},
		"metrics": map[string]interface{}{
			"goroutines": metrics["goroutines"],
			"memory_mb":  float64(metrics["memory_alloc_bytes"].(uint64)) / 1024 / 1024,
		},
	})
}
