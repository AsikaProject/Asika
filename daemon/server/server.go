package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"asika/common/config"
	"asika/common/i18n"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/handlers"
	"asika/daemon/templates"
)

func tFunc(key string, args ...interface{}) string {
	return i18n.T(key, args...)
}

func currentLangFunc() string {
	return i18n.Locale()
}

func i18nJSONFunc() string {
	msgs := i18n.AllMessages(i18n.Locale())
	data, _ := json.Marshal(msgs)
	return string(data)
}

// Server represents the HTTP server
type Server struct {
	engine  *gin.Engine
	httpSrv *http.Server
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
}

// NewServer creates a new server instance
func NewServer(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Server {
	if cfg != nil && cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	t, err := template.New("").Funcs(template.FuncMap{"t": tFunc, "currentLang": currentLangFunc, "i18nJSON": i18nJSONFunc}).ParseFS(templates.FS, "*.html")
	if err != nil {
		panic(fmt.Sprintf("failed to parse templates: %v", err))
	}
	engine.SetHTMLTemplate(t)

	s := &Server{
		cfg:     cfg,
		engine:  engine,
		clients: clients,
	}

	handlers.InitClients(clients)

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

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

func (s *Server) setupMiddleware() {
	s.engine.Use(initCheckMiddleware())
	s.engine.Use(LocaleMiddleware())
	s.engine.Use(Logger())
	s.engine.Use(gin.Recovery())
	s.engine.Use(metricsMiddleware())
	if s.cfg != nil {
		s.engine.Use(CORS(s.cfg.Server.CORSOrigins))
		if s.cfg.Server.RateLimitEnabled {
			s.engine.Use(RateLimit(rate.Limit(s.cfg.Server.RateLimitRPS), s.cfg.Server.RateLimitBurst))
		}
	}
	s.engine.Use(AuthMiddleware())
	if s.cfg != nil && s.cfg.Auth.FingerprintEnabled {
		s.engine.Use(FingerprintMiddleware())
		s.engine.Use(fingerprintContextMiddleware())
	}
}

func fingerprintContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("fingerprint_enabled", true)
		c.Next()
	}
}

// Start starts the server
func (s *Server) Start() error {
	var addr string
	if s.cfg == nil {
		addr = ":8080"
	} else {
		addr = s.cfg.Server.Listen
	}

	readTimeout := 30 * time.Second
	writeTimeout := 30 * time.Second
	if s.cfg != nil {
		if t := time.Duration(s.cfg.Server.ReadTimeoutSeconds) * time.Second; t > 0 {
			readTimeout = t
		}
		if t := time.Duration(s.cfg.Server.WriteTimeoutSeconds) * time.Second; t > 0 {
			writeTimeout = t
		}
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
	timeout := 30 * time.Second
	if s.cfg != nil && s.cfg.Server.ShutdownTimeoutSeconds > 0 {
		timeout = time.Duration(s.cfg.Server.ShutdownTimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpSrv.Shutdown(ctx)
}
