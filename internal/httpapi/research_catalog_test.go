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
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestOwnerCatalogHTTPReturnsRegistrationAndRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:http-catalog?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("DynamicPointMamba").SetSlug("dpm").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab").SetPrefix("gmc_lab").SetTokenHash([]byte("http-catalog")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repo, _ := client.Repository.Create().
		SetProjectID(project.ID).SetName("DynamicPointMamba").
		SetSSHURL(research.DynamicPointMambaSSHURL()).SetSSHHost("github.com").
		SetDefaultBranch("main").SetStatus("active").Save(ctx)
	service := research.NewService(client)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	if _, err := service.AgentRecordCatalog(ctx, principal, research.CatalogRecordInput{
		RepositoryID: repo.PublicID.String(),
		Rows: []research.CatalogRowInput{{
			Branch: "autoresearch/sprint-beat-sast-20260817", Setting: "G2 U-spec mean",
			Method: "U-spec", Implementation: "SPRINT_G2_DECISION.md", Metric: "90.4114",
			Result: "promote_U-spec", Link: "SPRINT_G2_DECISION.md", Hash: "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{TenantID: tenant.ID, UserPublicID: "owner-1", Role: "owner"}})
		c.Next()
	})
	handlers := NewResearchHandlers(service)
	router.GET("/projects/:id/experiment-catalog", handlers.Catalog)

	request := httptest.NewRequest(http.MethodGet, "/projects/"+project.PublicID.String()+"/experiment-catalog", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, research.DynamicPointMambaSSHURL()) || !strings.Contains(body, "90.4114") ||
		!strings.Contains(body, "autoresearch/sprint-beat-sast-20260817") || !strings.Contains(body, "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0") {
		t.Fatalf("catalog HTTP body missing rows: %s", body)
	}
	var payload struct {
		Data research.CatalogView `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Repositories) != 1 || len(payload.Data.Repositories[0].Rows) != 1 {
		t.Fatalf("payload = %+v", payload.Data)
	}
}
