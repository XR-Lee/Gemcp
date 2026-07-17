package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/buildinfo"
	"github.com/XR-Lee/Gemcp/internal/config"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/httpapi"
	"github.com/XR-Lee/Gemcp/internal/mcpserver"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	setupservice "github.com/XR-Lee/Gemcp/internal/setup"
	"github.com/XR-Lee/Gemcp/internal/web"
	"github.com/gin-gonic/gin"
)

type Database interface {
	Ping(context.Context) error
}

type Dependencies struct {
	Config  config.Config
	Build   buildinfo.Info
	DB      Database
	Ent     *ent.Client
	Secrets *secrets.Box
}

func New(deps Dependencies) *http.Server {
	if deps.Config.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.Use(gin.Recovery(), securityHeaders(), limitRequestBody(1<<20))

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", readinessHandler(deps.DB))

	api := router.Group("/api/v1")
	api.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": deps.Build})
	})

	setupHandlers := httpapi.NewSetupHandlers(setupservice.NewService(deps.Ent, deps.Secrets), deps.Config.BootstrapToken)
	authHandlers := httpapi.NewAuthHandlers(auth.NewService(deps.Ent, deps.Secrets, 12*time.Hour), deps.Config.SecureCookies)
	api.GET("/setup/status", setupHandlers.Status)
	api.POST("/setup", setupHandlers.Initialize)
	api.POST("/auth/login", authHandlers.Login)
	protected := api.Group("")
	protected.Use(authHandlers.RequireSession())
	protected.GET("/auth/me", authHandlers.Me)
	protected.POST("/auth/logout", authHandlers.Logout)
	projectHandlers := httpapi.NewProjectHandlers(deps.Ent)
	protected.GET("/projects", projectHandlers.List)

	repositoryService := gitrepository.NewService(deps.Ent, deps.Secrets, nil)
	repositoryHandlers := httpapi.NewRepositoryHandlers(repositoryService)
	protected.GET("/repositories", repositoryHandlers.List)
	protected.POST("/repositories", repositoryHandlers.Create)
	protected.POST("/repositories/:id/verify", repositoryHandlers.Verify)

	experimentService := experiment.NewService(deps.Ent, deps.Secrets, repositoryService)
	agentAuthService := agentauth.NewService(deps.Ent, deps.Secrets)
	mcpHandler := mcpserver.New(agentAuthService, experimentService, deps.Build.Version, nil).Handler()
	router.Any("/mcp", gin.WrapH(mcpHandler))

	frontend := web.Handler()
	router.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		if strings.HasPrefix(requestPath, "/api/") || strings.HasPrefix(requestPath, "/mcp") {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
				"code":    "NOT_FOUND",
				"message": "route not found",
			}})
			return
		}
		if !web.Enabled() {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
				"code":    "FRONTEND_NOT_EMBEDDED",
				"message": "build with the webembed tag to serve the frontend",
			}})
			return
		}
		frontend.ServeHTTP(c.Writer, c.Request)
	})

	return &http.Server{
		Addr:              deps.Config.Address,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}

func limitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func readinessHandler(db Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "reason": "database unavailable"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "reason": "database unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
