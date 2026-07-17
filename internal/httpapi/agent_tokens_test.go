package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentaccess"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestOwnerAgentTokenHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:agent-token-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).
		SetName("Research").
		SetSlug("research").
		SetMonthlyBudgetMilli(100_000).
		SetMaxExperimentMilli(20_000).
		Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	handlers := NewAgentTokenHandlers(agentaccess.NewService(client, box, "https://gemcp.example.com"))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), UserPublicID: "owner-id", Role: "owner",
		}})
		c.Next()
	})
	router.GET("/projects/:id/agent-tokens", handlers.List)
	router.POST("/projects/:id/agent-tokens", handlers.Issue)
	router.DELETE("/projects/:id/agent-tokens/:tokenID", handlers.Revoke)

	issueRequest := httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/agent-tokens", strings.NewReader(`{
		"label":"third-party-agent","scopes":["read","submit"],"expires_in_days":90
	}`))
	issueRequest.Header.Set("Content-Type", "application/json")
	issueResponse := httptest.NewRecorder()
	router.ServeHTTP(issueResponse, issueRequest)
	if issueResponse.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	var issued struct {
		Data agentaccess.IssueResult `json:"data"`
	}
	if err := json.Unmarshal(issueResponse.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Data.AgentToken, "gmc_") || issued.Data.MCPConfig.MCPServers["gemcp-research"].Headers["Authorization"] != "Bearer "+issued.Data.AgentToken {
		t.Fatalf("issued response = %+v", issued.Data)
	}

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/projects/"+project.PublicID.String()+"/agent-tokens", nil))
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), issued.Data.AgentToken) {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listed struct {
		Data agentaccess.ListResult `json:"data"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data.Tokens) != 1 || listed.Data.ConfigTemplate == nil || listed.Data.MCPURL != "https://gemcp.example.com/mcp" {
		t.Fatalf("list response = %+v", listed.Data)
	}

	revokeResponse := httptest.NewRecorder()
	router.ServeHTTP(revokeResponse, httptest.NewRequest(http.MethodDelete,
		"/projects/"+project.PublicID.String()+"/agent-tokens/"+issued.Data.Token.ID, nil))
	if revokeResponse.Code != http.StatusOK || !strings.Contains(revokeResponse.Body.String(), `"status":"revoked"`) {
		t.Fatalf("revoke status=%d body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}
}

func TestAgentTokenIssueRequiresConfiguredPublicURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:agent-token-no-url?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1000).SetMaxExperimentMilli(1000).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	handlers := NewAgentTokenHandlers(agentaccess.NewService(client, box, ""))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{TenantID: tenant.ID, UserPublicID: "owner", Role: "owner"}})
		c.Next()
	})
	router.POST("/projects/:id/agent-tokens", handlers.Issue)
	request := httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/agent-tokens", strings.NewReader(`{"label":"agent"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "MCP_PUBLIC_URL_UNAVAILABLE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
