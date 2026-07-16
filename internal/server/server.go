package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/buildinfo"
	"github.com/XR-Lee/Gemcp/internal/config"
	"github.com/XR-Lee/Gemcp/internal/web"
	"github.com/gin-gonic/gin"
)

type Database interface {
	Ping(context.Context) error
}

type Dependencies struct {
	Config config.Config
	Build  buildinfo.Info
	DB     Database
}

func New(deps Dependencies) *http.Server {
	if deps.Config.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", readinessHandler(deps.DB))

	api := router.Group("/api/v1")
	api.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": deps.Build})
	})

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
		IdleTimeout:       90 * time.Second,
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
