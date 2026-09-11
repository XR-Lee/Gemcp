package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestOwnerRepositoryReadinessHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:repo-readiness-http?mode=memory&cache=shared&_fk=1")
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
	record, err := client.Repository.Create().
		SetProjectID(project.ID).SetName("private-lab").
		SetSSHURL("git@github.com:research/private-lab.git").SetSSHHost("github.com").
		SetDefaultBranch("main").SetDeployPublicKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake gemcp-test").
		SetStatus(repository.StatusPendingKey).Save(ctx)
	if err != nil {
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
	router.GET("/repositories/:id/readiness", handlers.RepositoryReadiness)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repositories/"+record.PublicID.String()+"/readiness?project_id="+project.PublicID.String(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data experiment.RepositoryReadiness `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Ready || payload.Data.DeployKeySettingsURL != "https://github.com/research/private-lab/settings/keys" {
		t.Fatalf("readiness = %+v", payload.Data)
	}
	if len(payload.Data.Blockers) == 0 || payload.Data.Blockers[0].Kind != "deploy_key_required" {
		t.Fatalf("blockers = %+v", payload.Data.Blockers)
	}
}
