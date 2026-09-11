package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestOwnerCloseRunHTTPWritesResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:http-close-run?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab").SetPrefix("gmc_lab").SetTokenHash([]byte("http-close")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repo, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	finished, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repo.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("succeeded").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		SetMetrics(map[string]any{"overall_accuracy": 86.4}).
		Save(ctx)
	_, _ = client.ExperimentProposal.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
		SetRepositoryID(repo.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).
		SetExperimentID(finished.ID).SetStatus("submitted").SetRequestedRef("main").
		SetCommitSha(finished.CommitSha).SetExecutionMode("argv").SetArgv([]string{"python", "train.py"}).
		SetDisplayCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).
		SetTerminationGraceSeconds(30).SetProjectSnapshot(map[string]any{"expected_metric": "overall_accuracy"}).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetChecks([]map[string]any{}).
		SetReservedCostMilli(0).SetConfirmationDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").
		SetExpiresAt(time.Now().UTC().Add(time.Hour)).Save(ctx)
	service := research.NewService(client)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	created, err := service.AgentUpdate(ctx, principal, research.UpdateInput{
		Study: &research.StudyInput{Name: "http-close", Question: "Can the Owner HTTP path close a run?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "hypothesis", Title: "HTTP close works", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindPreparedRun(ctx, principal, hypothesis.Study.Nodes[1].ID, finished.PublicID.String(), "http smoke"); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{TenantID: tenant.ID, UserPublicID: "owner-1", Role: "owner"}})
		c.Next()
	})
	router.POST("/projects/:id/research/close-run", NewResearchHandlers(service).CloseRun)
	request := httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/research/close-run", strings.NewReader(
		`{"experiment_id":"`+finished.PublicID.String()+`","title":"HTTP-closed smoke"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "HTTP-closed smoke") || !strings.Contains(response.Body.String(), "overall_accuracy") {
		t.Fatalf("close-run HTTP body = %s", response.Body.String())
	}
}
