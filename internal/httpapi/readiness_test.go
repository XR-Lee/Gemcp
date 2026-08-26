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
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestOwnerAgentReadinessHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:agent-readiness-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Research").SetSlug("research").
		SetMonthlyBudgetMilli(100_000).SetMaxExperimentMilli(20_000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("training-agent").
		SetPrefix("gmc_abcd123").SetTokenHash([]byte("readiness-token")).
		SetScopes([]string{"read", "submit"}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewExperimentHandlers(experiment.NewService(client, box, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), UserPublicID: "owner-id", Role: "owner",
		}})
		c.Next()
	})
	router.GET("/projects/:id/agent-readiness", handlers.AgentReadiness)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/projects/"+project.PublicID.String()+"/agent-readiness", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data experiment.AgentReadiness `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Status != experiment.ReadinessWaitingCompute || len(payload.Data.Agents) != 1 ||
		payload.Data.Agents[0].Label != "training-agent" || payload.Data.Instructions.InspectTool != "get_project_options" {
		t.Fatalf("readiness = %+v", payload.Data)
	}
	if strings.Contains(response.Body.String(), "readiness-token") || strings.Contains(response.Body.String(), "BEGIN") {
		t.Fatalf("response leaked secret: %s", response.Body.String())
	}
}
