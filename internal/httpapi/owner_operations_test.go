package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/gin-gonic/gin"
)

func TestEmergencyStopRequiresServerSideSTOPConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{TenantID: 1, UserPublicID: "owner-1", Role: "owner"}})
		c.Next()
	})
	router.POST("/emergency", NewRuntimeHandlers(nil).EmergencyStop)
	request := httptest.NewRequest(http.MethodPost, "/emergency", strings.NewReader(`{"confirmation":"stop"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRuntimeNotificationAndFinanceOperationsRequireOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{TenantID: 1, UserPublicID: "member-1", Role: "member"}})
		c.Next()
	})
	runtimeHandlers := NewRuntimeHandlers(nil)
	notificationHandlers := NewNotificationHandlers(nil)
	agentTokenHandlers := NewAgentTokenHandlers(nil)
	financeHandlers := NewFinanceHandlers(nil)
	router.GET("/managed", runtimeHandlers.List)
	router.GET("/runtime/status", runtimeHandlers.Status)
	router.POST("/emergency", runtimeHandlers.EmergencyStop)
	router.GET("/notifications", notificationHandlers.List)
	router.PUT("/notifications/settings", notificationHandlers.Configure)
	router.GET("/projects/:id/agent-tokens", agentTokenHandlers.List)
	router.POST("/projects/:id/agent-tokens", agentTokenHandlers.Issue)
	router.DELETE("/projects/:id/agent-tokens/:tokenID", agentTokenHandlers.Revoke)
	router.GET("/finance", financeHandlers.Dashboard)
	router.POST("/projects/:id/budget-adjustments", financeHandlers.Adjust)
	router.PATCH("/projects/:id", NewProjectHandlers(nil).Update)
	router.GET("/projects/:id/dataset-bindings", NewDatasetBindingHandlers(nil).List)
	router.POST("/projects/:id/dataset-bindings", NewDatasetBindingHandlers(nil).Create)
	router.GET("/projects/:id/dataset-sources", NewDatasetBindingHandlers(nil).Sources)
	router.DELETE("/projects/:id/dataset-bindings/:bindingID", NewDatasetBindingHandlers(nil).Remove)
	router.GET("/projects/:id/environments", NewEnvironmentHandlers(nil).List)
	router.POST("/projects/:id/environments", NewEnvironmentHandlers(nil).Create)
	router.DELETE("/projects/:id/environments/:environmentID", NewEnvironmentHandlers(nil).Remove)
	router.POST("/projects/:id/experiment-proposals", NewExperimentHandlers(nil).Prepare)
	router.POST("/projects/:id/experiment-proposals/:proposalID/submit", NewExperimentHandlers(nil).SubmitPrepared)
	router.GET("/projects/:id/workloads", NewExperimentHandlers(nil).ListWorkloads)
	router.POST("/projects/:id/workloads/preview", NewExperimentHandlers(nil).PreviewWorkload)
	router.POST("/projects/:id/workloads", NewExperimentHandlers(nil).SaveWorkload)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/managed", nil),
		httptest.NewRequest(http.MethodGet, "/runtime/status", nil),
		httptest.NewRequest(http.MethodPost, "/emergency", nil),
		httptest.NewRequest(http.MethodGet, "/notifications", nil),
		httptest.NewRequest(http.MethodPut, "/notifications/settings", nil),
		httptest.NewRequest(http.MethodGet, "/projects/project-id/agent-tokens", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/agent-tokens", nil),
		httptest.NewRequest(http.MethodDelete, "/projects/project-id/agent-tokens/token-id", nil),
		httptest.NewRequest(http.MethodGet, "/finance", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/budget-adjustments", nil),
		httptest.NewRequest(http.MethodPatch, "/projects/project-id", nil),
		httptest.NewRequest(http.MethodGet, "/projects/project-id/dataset-bindings", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/dataset-bindings", nil),
		httptest.NewRequest(http.MethodGet, "/projects/project-id/dataset-sources", nil),
		httptest.NewRequest(http.MethodDelete, "/projects/project-id/dataset-bindings/binding-id", nil),
		httptest.NewRequest(http.MethodGet, "/projects/project-id/environments", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/environments", nil),
		httptest.NewRequest(http.MethodDelete, "/projects/project-id/environments/environment-id", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/experiment-proposals", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/experiment-proposals/proposal-id/submit", nil),
		httptest.NewRequest(http.MethodGet, "/projects/project-id/workloads", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/workloads/preview", nil),
		httptest.NewRequest(http.MethodPost, "/projects/project-id/workloads", nil),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
}
