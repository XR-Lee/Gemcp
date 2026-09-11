package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestOwnerCanUpdateProjectPolicyAndDatasetBindings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:project-runtime-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Lab").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Research").SetSlug("research").
		SetMonthlyBudgetMilli(100_000).SetMaxExperimentMilli(20_000).
		SetMaxConcurrency(1).SetMaxRuntimeSeconds(3600).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), UserPublicID: "owner-1", Role: "owner",
		}})
		c.Next()
	})
	router.PATCH("/projects/:id", NewProjectHandlers(client).Update)
	bindings := NewDatasetBindingHandlers(datasetcatalog.NewService(client))
	router.GET("/projects/:id/dataset-bindings", bindings.List)
	router.POST("/projects/:id/dataset-bindings", bindings.Create)
	router.GET("/projects/:id/dataset-sources", bindings.Sources)
	router.DELETE("/projects/:id/dataset-bindings/:bindingID", bindings.Remove)
	environments := NewEnvironmentHandlers(environmentcatalog.NewService(client, nil))
	router.GET("/projects/:id/environments", environments.List)
	router.POST("/projects/:id/environments", environments.Create)
	router.DELETE("/projects/:id/environments/:environmentID", environments.Remove)

	patch := httptest.NewRecorder()
	patchRequest := httptest.NewRequest(http.MethodPatch, "/projects/"+project.PublicID.String(), strings.NewReader(
		`{"max_runtime_seconds":57600,"max_experiment_milli":40000}`,
	))
	patchRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(patch, patchRequest)
	if patch.Code != http.StatusOK {
		t.Fatalf("policy patch status=%d body=%s", patch.Code, patch.Body.String())
	}
	var updated struct {
		Data projectView `json:"data"`
	}
	if err := json.Unmarshal(patch.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Data.MaxRuntimeSeconds != 57600 || updated.Data.MaxExperimentMilli != 40000 {
		t.Fatalf("updated policy = %+v", updated.Data)
	}

	create := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/dataset-bindings", bytes.NewReader([]byte(
		`{"catalog":"scanobjectnn-objbg","sources":[{"url":"https://huggingface.co/datasets/example/resolve/main/train.h5","relative_path":"main_split/train.h5"}]}`,
	)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(create, request)
	if create.Code != http.StatusCreated {
		t.Fatalf("create binding status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		Data datasetcatalog.View `json:"data"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.EnvironmentVariable != "GEMCP_DATASET_SCANOBJECTNN_OBJBG" || created.Data.Backend != datasetcatalog.BackendElastic ||
		len(created.Data.Sources) != 1 {
		t.Fatalf("created binding = %+v", created.Data)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/projects/"+project.PublicID.String()+"/dataset-bindings", nil))
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(created.Data.ID)) {
		t.Fatalf("list bindings status=%d body=%s", list.Code, list.Body.String())
	}

	remove := httptest.NewRecorder()
	router.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/projects/"+project.PublicID.String()+"/dataset-bindings/"+created.Data.ID, nil))
	if remove.Code != http.StatusOK || !bytes.Contains(remove.Body.Bytes(), []byte(`"status":"disabled"`)) {
		t.Fatalf("remove binding status=%d body=%s", remove.Code, remove.Body.String())
	}

	sources := httptest.NewRecorder()
	router.ServeHTTP(sources, httptest.NewRequest(http.MethodGet, "/projects/"+project.PublicID.String()+"/dataset-sources", nil))
	if sources.Code != http.StatusOK || !bytes.Contains(sources.Body.Bytes(), []byte("scanobjectnn-objbg")) {
		t.Fatalf("dataset sources status=%d body=%s", sources.Code, sources.Body.String())
	}

	createEnv := httptest.NewRecorder()
	envRequest := httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/environments", bytes.NewReader([]byte(
		`{"name":"official-base","backend":"autodl_elastic","image_uuid":"image-6c15b8aad2","set_default":true}`,
	)))
	envRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(createEnv, envRequest)
	if createEnv.Code != http.StatusCreated {
		t.Fatalf("create environment status=%d body=%s", createEnv.Code, createEnv.Body.String())
	}
	var createdEnv struct {
		Data environmentcatalog.View `json:"data"`
	}
	if err := json.Unmarshal(createEnv.Body.Bytes(), &createdEnv); err != nil {
		t.Fatal(err)
	}
	if createdEnv.Data.ImageUUID != "image-6c15b8aad2" || createdEnv.Data.Backend != environmentcatalog.BackendElastic {
		t.Fatalf("created environment = %+v", createdEnv.Data)
	}
	listEnv := httptest.NewRecorder()
	router.ServeHTTP(listEnv, httptest.NewRequest(http.MethodGet, "/projects/"+project.PublicID.String()+"/environments", nil))
	if listEnv.Code != http.StatusOK || !bytes.Contains(listEnv.Body.Bytes(), []byte(createdEnv.Data.ID)) {
		t.Fatalf("list environments status=%d body=%s", listEnv.Code, listEnv.Body.String())
	}
	removeEnv := httptest.NewRecorder()
	router.ServeHTTP(removeEnv, httptest.NewRequest(http.MethodDelete, "/projects/"+project.PublicID.String()+"/environments/"+createdEnv.Data.ID, nil))
	if removeEnv.Code != http.StatusOK || !bytes.Contains(removeEnv.Body.Bytes(), []byte(`"status":"disabled"`)) {
		t.Fatalf("remove environment status=%d body=%s", removeEnv.Code, removeEnv.Body.String())
	}
}

func TestOwnerPreparedSubmitRequiresExplicitConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:owner-confirm-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: 1, UserPublicID: "owner-1", Role: "owner",
		}})
		c.Next()
	})
	router.POST("/projects/:id/experiment-proposals", NewExperimentHandlers(experiment.NewService(client, box, nil)).Prepare)
	router.POST("/projects/:id/experiment-proposals/:proposalID/submit", NewExperimentHandlers(experiment.NewService(client, box, nil)).SubmitPrepared)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/projects/project-id/experiment-proposals/proposal-id/submit", strings.NewReader(
		`{"confirmation_digest":"sha256:`+strings.Repeat("ab", 32)+`"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("EXPERIMENT_CONFIRMATION_REQUIRED")) {
		t.Fatalf("unconfirmed submit status=%d body=%s", response.Code, response.Body.String())
	}

	prepare := httptest.NewRecorder()
	prepareRequest := httptest.NewRequest(http.MethodPost, "/projects/project-id/experiment-proposals", strings.NewReader(`{`))
	prepareRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(prepare, prepareRequest)
	if prepare.Code != http.StatusBadRequest || !bytes.Contains(prepare.Body.Bytes(), []byte("INVALID_EXPERIMENT_PREPARE")) {
		t.Fatalf("invalid prepare status=%d body=%s", prepare.Code, prepare.Body.String())
	}
}
