package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	router.PATCH("/projects/:id/agent-tokens/:tokenID", handlers.UpdateScopes)
	router.DELETE("/projects/:id/agent-tokens/:tokenID", handlers.Revoke)
	router.POST("/projects/:id/agent-enrollments", handlers.IssueEnrollment)
	router.DELETE("/projects/:id/agent-enrollments/:enrollmentID", handlers.RevokeEnrollment)
	router.POST("/agent-enrollments/claim", handlers.ClaimEnrollment)
	router.POST("/agent-enrollments/complete", handlers.CompleteEnrollment)

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
	updateRequest := httptest.NewRequest(http.MethodPatch, "/projects/"+project.PublicID.String()+"/agent-tokens/"+issued.Data.Token.ID, strings.NewReader(`{"scopes":["read","submit","configure"]}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"configure"`) || strings.Contains(updateResponse.Body.String(), issued.Data.AgentToken) {
		t.Fatalf("scope update status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}

	enrollmentRequest := httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/agent-enrollments", strings.NewReader(`{
		"label":"pi-agent","scopes":["read","submit","cancel"],"expires_in_days":30,"setup_expires_in_minutes":15
	}`))
	enrollmentRequest.Header.Set("Content-Type", "application/json")
	enrollmentResponse := httptest.NewRecorder()
	router.ServeHTTP(enrollmentResponse, enrollmentRequest)
	if enrollmentResponse.Code != http.StatusCreated {
		t.Fatalf("enrollment status=%d body=%s", enrollmentResponse.Code, enrollmentResponse.Body.String())
	}
	var enrollment struct {
		Data agentaccess.EnrollmentIssueResult `json:"data"`
	}
	if err := json.Unmarshal(enrollmentResponse.Body.Bytes(), &enrollment); err != nil {
		t.Fatal(err)
	}
	setupURL, err := url.Parse(enrollment.Data.SetupURL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, _ := url.ParseQuery(setupURL.Fragment)
	setupCode := fragment.Get("code")
	if !strings.HasPrefix(setupCode, "gme_") || setupURL.RawQuery != "" {
		t.Fatalf("setup URL = %q", enrollment.Data.SetupURL)
	}

	claimRequest := httptest.NewRequest(http.MethodPost, "/agent-enrollments/claim", strings.NewReader(`{"code":"`+setupCode+`"}`))
	claimRequest.Header.Set("Content-Type", "application/json")
	claimResponse := httptest.NewRecorder()
	router.ServeHTTP(claimResponse, claimRequest)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimResponse.Code, claimResponse.Body.String())
	}
	var claim struct {
		Data agentaccess.EnrollmentClaimResult `json:"data"`
	}
	if err := json.Unmarshal(claimResponse.Body.Bytes(), &claim); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(claim.Data.AgentToken, "gmc_") || claim.Data.PiConfig.BearerToken != claim.Data.AgentToken || len(claim.Data.PiConfig.DirectTools) != 35 {
		t.Fatalf("claim response = %+v", claim.Data)
	}

	completeRequest := httptest.NewRequest(http.MethodPost, "/agent-enrollments/complete", strings.NewReader(`{
		"code":"`+setupCode+`","client":"pi-mcp-adapter/2.10.0","tool_count":35,
		"checks":["tools","guide","options","cost"]
	}`))
	completeRequest.Header.Set("Content-Type", "application/json")
	completeResponse := httptest.NewRecorder()
	router.ServeHTTP(completeResponse, completeRequest)
	if completeResponse.Code != http.StatusOK || !strings.Contains(completeResponse.Body.String(), `"status":"completed"`) {
		t.Fatalf("complete status=%d body=%s", completeResponse.Code, completeResponse.Body.String())
	}

	replayedRequest := httptest.NewRequest(http.MethodPost, "/agent-enrollments/claim", strings.NewReader(`{"code":"`+setupCode+`"}`))
	replayedRequest.Header.Set("Content-Type", "application/json")
	replayedResponse := httptest.NewRecorder()
	router.ServeHTTP(replayedResponse, replayedRequest)
	if replayedResponse.Code != http.StatusGone || !strings.Contains(replayedResponse.Body.String(), "AGENT_SETUP_INVALID") {
		t.Fatalf("replayed claim status=%d body=%s", replayedResponse.Code, replayedResponse.Body.String())
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
