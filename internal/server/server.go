package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/guides"
	"github.com/XR-Lee/Gemcp/internal/agentaccess"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/buildinfo"
	"github.com/XR-Lee/Gemcp/internal/config"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/diagnostic"
	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/finance"
	"github.com/XR-Lee/Gemcp/internal/httpapi"
	"github.com/XR-Lee/Gemcp/internal/mcpserver"
	"github.com/XR-Lee/Gemcp/internal/nodeaccess"
	"github.com/XR-Lee/Gemcp/internal/notification"
	providerservice "github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/research"
	runnerservice "github.com/XR-Lee/Gemcp/internal/runner"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/selfhosted"
	setupservice "github.com/XR-Lee/Gemcp/internal/setup"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/XR-Lee/Gemcp/internal/web"
	"github.com/XR-Lee/Gemcp/internal/workspacecatalog"
	"github.com/gin-gonic/gin"
)

type Database interface {
	Ping(context.Context) error
}

type Dependencies struct {
	Config     config.Config
	Build      buildinfo.Info
	DB         Database
	Ent        *ent.Client
	Secrets    *secrets.Box
	SelfHosted *selfhosted.Service
	SSHCloud   *sshcloud.Service
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
	router.GET("/docs/agent-mcp.md", markdownGuide(guides.AgentMCP()))
	router.GET("/docs/owner-mcp.md", markdownGuide(guides.OwnerMCP()))
	router.GET("/agent/setup", markdownGuide(guides.PiSetup(deps.Config.PublicURL)))
	router.GET("/agent/setup/install.mjs", staticGuide("text/javascript; charset=utf-8", guides.PiSetupInstaller(deps.Config.PublicURL)))
	router.GET("/agent/setup/gemcp-tool.mjs", staticGuide("text/javascript; charset=utf-8", guides.GemcpTool()))
	router.GET("/node/setup", localizedNodeSetupGuide(deps.Config.PublicURL, deps.Build))

	api := router.Group("/api/v1")
	api.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": deps.Build})
	})

	setupHandlers := httpapi.NewSetupHandlers(setupservice.NewService(deps.Ent, deps.Secrets), deps.Config.BootstrapToken)
	authHandlers := httpapi.NewAuthHandlers(auth.NewService(deps.Ent, deps.Secrets, 12*time.Hour), deps.Config.SecureCookies)
	api.GET("/setup/status", setupHandlers.Status)
	api.POST("/setup", setupHandlers.Initialize)
	api.POST("/auth/login", authHandlers.Login)
	agentAccessService := agentaccess.NewService(deps.Ent, deps.Secrets, deps.Config.PublicURL)
	agentTokenHandlers := httpapi.NewAgentTokenHandlers(agentAccessService)
	nodeOptions := []nodeaccess.ServiceOption{}
	if deps.SelfHosted != nil {
		nodeOptions = append(nodeOptions, nodeaccess.WithEventProjector(deps.SelfHosted))
	}
	nodeService := nodeaccess.NewService(deps.Ent, deps.Secrets, deps.Config.PublicURL, deps.Config.SelfHostedEnabled, nodeOptions...)
	nodeHandlers := httpapi.NewNodeHandlers(nodeService, deps.SelfHosted)
	api.POST("/agent-enrollments/claim", agentTokenHandlers.ClaimEnrollment)
	api.POST("/agent-enrollments/complete", agentTokenHandlers.CompleteEnrollment)
	api.POST("/node-enrollments/claim", nodeHandlers.ClaimEnrollment)
	api.POST("/nodes/sync", nodeHandlers.Sync)
	api.GET("/node-assignments/:id/source", nodeHandlers.Source)

	repositoryService := gitrepository.NewService(deps.Ent, deps.Secrets, nil)
	runnerHandlers := httpapi.NewRunnerHandlers(runnerservice.NewService(
		deps.Ent, deps.Secrets, repositoryService, runnerservice.WithSourceMaxBytes(deps.Config.RunnerSourceMaxBytes),
	))
	api.GET("/runner/bootstrap", runnerHandlers.Bootstrap)
	api.GET("/runner/spec", runnerHandlers.Spec)
	api.GET("/runner/source", runnerHandlers.Source)
	api.POST("/runner/events", runnerHandlers.Event)

	protected := api.Group("")
	protected.Use(authHandlers.RequireSession())
	protected.GET("/auth/me", authHandlers.Me)
	protected.POST("/auth/logout", authHandlers.Logout)
	projectHandlers := httpapi.NewProjectHandlers(deps.Ent)
	protected.GET("/projects", projectHandlers.List)
	protected.PATCH("/projects/:id", projectHandlers.Update)
	datasetBindingHandlers := httpapi.NewDatasetBindingHandlers(datasetcatalog.NewService(deps.Ent, datasetcatalog.WithLocalCPUFixture(deps.Config.LocalProcessEnabled)))
	protected.GET("/projects/:id/dataset-bindings", datasetBindingHandlers.List)
	protected.POST("/projects/:id/dataset-bindings", datasetBindingHandlers.Create)
	protected.GET("/projects/:id/dataset-sources", datasetBindingHandlers.Sources)
	protected.DELETE("/projects/:id/dataset-bindings/:bindingID", datasetBindingHandlers.Remove)
	financeHandlers := httpapi.NewFinanceHandlers(finance.NewService(deps.Ent))
	protected.GET("/finance", financeHandlers.Dashboard)
	protected.POST("/projects/:id/budget-adjustments", financeHandlers.Adjust)
	protected.GET("/projects/:id/agent-tokens", agentTokenHandlers.List)
	protected.POST("/projects/:id/agent-tokens", agentTokenHandlers.Issue)
	protected.PATCH("/projects/:id/agent-tokens/:tokenID", agentTokenHandlers.UpdateScopes)
	protected.DELETE("/projects/:id/agent-tokens/:tokenID", agentTokenHandlers.Revoke)
	protected.POST("/projects/:id/agent-enrollments", agentTokenHandlers.IssueEnrollment)
	protected.DELETE("/projects/:id/agent-enrollments/:enrollmentID", agentTokenHandlers.RevokeEnrollment)
	protected.GET("/nodes", nodeHandlers.List)
	protected.POST("/node-enrollments", nodeHandlers.IssueEnrollment)
	protected.POST("/node-enrollments/:id/approve", nodeHandlers.ApproveEnrollment)
	protected.DELETE("/node-enrollments/:id", nodeHandlers.RevokeEnrollment)
	protected.GET("/projects/:id/self-hosted-runtimes", nodeHandlers.ListRuntimeConfigs)
	protected.POST("/projects/:id/self-hosted-runtimes", nodeHandlers.CreateRuntimeConfig)
	protected.PUT("/projects/:id/self-hosted-trusted-workspace", nodeHandlers.EnableTrustedWorkspace)
	protected.DELETE("/projects/:id/self-hosted-trusted-workspace/:node_id", nodeHandlers.DisableTrustedWorkspace)
	sshCloudHandlers := httpapi.NewSSHCloudHandlers(deps.SSHCloud)
	protected.GET("/ssh-cloud-nodes", sshCloudHandlers.List)
	protected.POST("/ssh-cloud-nodes", sshCloudHandlers.Create)
	protected.POST("/ssh-cloud-nodes/:id/probe", sshCloudHandlers.Probe)
	protected.POST("/ssh-cloud-nodes/:id/rotate-credential", sshCloudHandlers.Rotate)
	protected.DELETE("/ssh-cloud-nodes/:id", sshCloudHandlers.Revoke)

	providerService := providerservice.NewService(deps.Ent, deps.Secrets, deps.Build.Version)
	environmentService := environmentcatalog.NewService(deps.Ent, providerService)
	environmentHandlers := httpapi.NewEnvironmentHandlers(environmentService)
	protected.GET("/projects/:id/environments", environmentHandlers.List)
	protected.POST("/projects/:id/environments", environmentHandlers.Create)
	protected.DELETE("/projects/:id/environments/:environmentID", environmentHandlers.Remove)
	providerHandlers := httpapi.NewProviderHandlers(providerService)
	protected.GET("/provider", providerHandlers.Summary)
	protected.POST("/provider/query", providerHandlers.Query)
	protected.PUT("/provider", providerHandlers.Configure)
	protected.GET("/provider/deployments/:id", providerHandlers.Deployment)
	runtimeOperations := execution.NewOperations(
		deps.Ent, execution.WithRuntimeConfiguration(deps.Config.SchedulerEnabled, deps.Config.GlobalConcurrency, deps.Config.PublicURL != "", config.HTTPSPublicOrigin(deps.Config.PublicURL)),
		execution.WithSelfHostedEnabled(deps.Config.SelfHostedEnabled),
		execution.WithSSHCloudEnabled(deps.Config.SSHCloudEnabled),
	)
	runtimeHandlers := httpapi.NewRuntimeHandlers(runtimeOperations)
	protected.GET("/runtime/status", runtimeHandlers.Status)
	protected.GET("/provider/managed-resources", runtimeHandlers.List)
	protected.POST("/provider/deployments/:id/stop", runtimeHandlers.Stop)
	protected.POST("/provider/emergency-stop", runtimeHandlers.EmergencyStop)

	notificationHandlers := httpapi.NewNotificationHandlers(notification.NewService(deps.Ent, deps.Secrets))
	protected.GET("/notifications/settings", notificationHandlers.Setting)
	protected.PUT("/notifications/settings", notificationHandlers.Configure)
	protected.POST("/notifications/test", notificationHandlers.Test)
	protected.GET("/notifications", notificationHandlers.List)

	repositoryHandlers := httpapi.NewRepositoryHandlers(repositoryService)
	protected.GET("/repositories", repositoryHandlers.List)
	protected.POST("/repositories", repositoryHandlers.Create)
	protected.POST("/repositories/:id/verify", repositoryHandlers.Verify)

	experimentOptions := []experiment.ServiceOption{
		experiment.WithPreparedExperiments(
			repositoryService, repositoryService, providerService, runtimeOperations,
			experiment.ProposalConfig{
				SourceMaxBytes: deps.Config.RunnerSourceMaxBytes, SelfHostedEnabled: deps.Config.SelfHostedEnabled,
				SSHCloudEnabled: deps.Config.SSHCloudEnabled,
			},
		),
	}
	if deps.SSHCloud != nil {
		experimentOptions = append(experimentOptions, experiment.WithSSHCloud(deps.SSHCloud))
	}
	experimentOptions = append(experimentOptions, experiment.WithLocalCPUDataset(datasetcatalog.NewService(deps.Ent, datasetcatalog.WithLocalCPUFixture(deps.Config.LocalProcessEnabled))))
	experimentService := experiment.NewService(deps.Ent, deps.Secrets, repositoryService, experimentOptions...)
	experimentHandlers := httpapi.NewExperimentHandlers(experimentService)
	protected.GET("/experiments", experimentHandlers.List)
	protected.GET("/experiments/:id", experimentHandlers.Get)
	protected.GET("/experiments/:id/attempts", experimentHandlers.Attempts)
	protected.GET("/projects/:id/cost", experimentHandlers.Cost)
	protected.GET("/projects/:id/operations", experimentHandlers.Operations)
	protected.POST("/projects/:id/experiment-proposals/:proposalID/submit", experimentHandlers.SubmitPrepared)
	protected.GET("/projects/:id/agent-readiness", experimentHandlers.AgentReadiness)
	researchService := research.NewService(deps.Ent)
	experimentService.SetGraphBinder(researchService)
	researchHandlers := httpapi.NewResearchHandlers(researchService)
	protected.GET("/projects/:id/research", researchHandlers.Get)
	protected.PUT("/projects/:id/research", researchHandlers.Update)
	diagnosticService := diagnostic.NewService(
		deps.Ent, deps.Secrets, repositoryService, providerService, runtimeOperations, experimentService,
		diagnostic.Config{SourceMaxBytes: deps.Config.RunnerSourceMaxBytes, SelfHostedEnabled: deps.Config.SelfHostedEnabled},
	)
	diagnosticHandlers := httpapi.NewDiagnosticHandlers(diagnosticService)
	protected.GET("/projects/:id/diagnostics/options", diagnosticHandlers.Options)
	protected.POST("/projects/:id/diagnostics/preflight", diagnosticHandlers.Preflight)
	protected.GET("/projects/:id/diagnostics", diagnosticHandlers.List)
	protected.POST("/projects/:id/diagnostics", diagnosticHandlers.Submit)
	protected.GET("/projects/:id/diagnostics/:runID", diagnosticHandlers.Get)
	protected.POST("/projects/:id/diagnostics/:runID/cancel", diagnosticHandlers.Cancel)

	agentAuthService := agentauth.NewService(deps.Ent, deps.Secrets)
	mcpHandler := mcpserver.New(
		agentAuthService, experimentService, deps.Build.Version, nil,
		mcpserver.WithConfiguration(repositoryService, workspacecatalog.NewService(deps.Ent), datasetcatalog.NewService(deps.Ent, datasetcatalog.WithLocalCPUFixture(deps.Config.LocalProcessEnabled))),
		mcpserver.WithEnvironments(environmentService),
		mcpserver.WithResearch(researchService),
		mcpserver.WithSSHCloud(deps.SSHCloud),
	).Handler()
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

func markdownGuide(content string) gin.HandlerFunc {
	return staticGuide("text/markdown; charset=utf-8", content)
}

func localizedNodeSetupGuide(publicURL string, build buildinfo.Info) gin.HandlerFunc {
	english := guides.NodeSetup(publicURL, build.Version, build.Commit, "en")
	chinese := guides.NodeSetup(publicURL, build.Version, build.Commit, "zh")
	return func(c *gin.Context) {
		language, err := nodeSetupLanguage(c.Request.URL.RawQuery, c.GetHeader("Accept-Language"))
		if err != nil {
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{
				"code": "INVALID_LANGUAGE", "message": "lang must be exactly zh or en",
			}})
			return
		}
		content := english
		contentLanguage := "en"
		if language == "zh" {
			content = chinese
			contentLanguage = "zh-CN"
		}
		c.Header("Cache-Control", "public, max-age=300")
		c.Header("Content-Disposition", "inline")
		c.Header("Content-Language", contentLanguage)
		c.Header("Vary", "Accept-Language")
		c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(content))
	}
}

func nodeSetupLanguage(rawQuery, acceptLanguage string) (string, error) {
	if rawQuery != "" {
		query, err := url.ParseQuery(rawQuery)
		if err != nil || len(query) != 1 || len(query["lang"]) != 1 {
			return "", fmt.Errorf("invalid language query")
		}
		language := query.Get("lang")
		if language != "zh" && language != "en" {
			return "", fmt.Errorf("invalid language query")
		}
		return language, nil
	}
	for _, preference := range strings.Split(strings.ToLower(acceptLanguage), ",") {
		language := strings.TrimSpace(strings.SplitN(preference, ";", 2)[0])
		if strings.HasPrefix(language, "zh") {
			return "zh", nil
		}
		if strings.HasPrefix(language, "en") {
			return "en", nil
		}
	}
	return "en", nil
}

func staticGuide(contentType, content string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=300")
		c.Header("Content-Disposition", "inline")
		c.Data(http.StatusOK, contentType, []byte(content))
	}
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/mcp") {
			c.Header("Cache-Control", "no-store")
			c.Header("Pragma", "no-cache")
		}
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
