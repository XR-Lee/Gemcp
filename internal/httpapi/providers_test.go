package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/internal/auth"
	providerservice "github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/gin-gonic/gin"
)

type fakeProviderOperations struct {
	configureInput providerservice.ConfigureInput
	queryBackend   string
	err            error
}

func (f *fakeProviderOperations) Summaries(context.Context, int) ([]providerservice.Summary, error) {
	return []providerservice.Summary{{ID: "provider-id", Name: "Private", BaseURL: "https://private.autodl.com", Backend: "private", Status: "active", CredentialConfigured: true}}, f.err
}

func (f *fakeProviderOperations) QueryResources(_ context.Context, _ int, backend string) (providerservice.ResourceSnapshot, error) {
	f.queryBackend = backend
	return providerservice.ResourceSnapshot{
		GeneratedAt: time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC),
		GPUStock:    []providerservice.GPUStock{{Name: "RTX 3090", Idle: 2, Total: 9}},
	}, f.err
}

func (f *fakeProviderOperations) Configure(_ context.Context, _ int, _ string, input providerservice.ConfigureInput) (providerservice.ConfigureResult, error) {
	f.configureInput = input
	return providerservice.ConfigureResult{Provider: providerservice.Summary{ID: "provider-id", CredentialConfigured: true}}, f.err
}

func (f *fakeProviderOperations) Deployment(context.Context, int, string) (providerservice.DeploymentDetails, error) {
	return providerservice.DeploymentDetails{Deployment: providerservice.Deployment{UUID: "deployment-1"}}, f.err
}

func providerRouter(role string, service providerOperations) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: 7, UserPublicID: "owner-id", Role: role,
		}})
		c.Next()
	})
	handlers := NewProviderHandlers(service)
	router.GET("/provider", handlers.Summary)
	router.POST("/provider/query", handlers.Query)
	router.PUT("/provider", handlers.Configure)
	router.GET("/provider/deployments/:id", handlers.Deployment)
	return router
}

func TestProviderHTTPResponsesNeverReturnCredential(t *testing.T) {
	service := &fakeProviderOperations{}
	router := providerRouter("owner", service)

	for method, path := range map[string][2]string{
		http.MethodGet + " summary":    {"/provider", ""},
		http.MethodPost + " query":     {"/provider/query", ""},
		http.MethodPut + " configure":  {"/provider", `{"name":"Private","base_url":"https://private.autodl.com","token":"real-secret-provider-token"}`},
		http.MethodGet + " deployment": {"/provider/deployments/deployment-1", ""},
	} {
		parts := strings.SplitN(method, " ", 2)
		request := httptest.NewRequest(parts[0], path[0], strings.NewReader(path[1]))
		if path[1] != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", method, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "real-secret-provider-token") || strings.Contains(response.Body.String(), "ciphertext") {
			t.Fatalf("%s leaked Provider credential: %s", method, response.Body.String())
		}
	}
	if service.configureInput.Token != "real-secret-provider-token" {
		t.Fatalf("Configure input token = %q", service.configureInput.Token)
	}
}

func TestProviderHTTPRequiresOwner(t *testing.T) {
	router := providerRouter("member", &fakeProviderOperations{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/provider", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestProviderHTTPQueryForwardsBackend(t *testing.T) {
	service := &fakeProviderOperations{}
	router := providerRouter("owner", service)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/provider/query?backend=elastic", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if service.queryBackend != "elastic" {
		t.Fatalf("query backend = %q", service.queryBackend)
	}
}

func TestProviderHTTPSummaryReturnsProviderList(t *testing.T) {
	router := providerRouter("owner", &fakeProviderOperations{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/provider", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"providers"`) || !strings.Contains(response.Body.String(), `"provider-id"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestProviderHTTPOperationErrorIsBadGateway(t *testing.T) {
	service := &fakeProviderOperations{err: &providerservice.OperationError{Operation: "GPU query", Cause: errors.New("dial failed")}}
	router := providerRouter("owner", service)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/provider/query", nil))
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "PROVIDER_QUERY_FAILED") {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
}
