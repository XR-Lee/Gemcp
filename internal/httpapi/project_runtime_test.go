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
	router.DELETE("/projects/:id/dataset-bindings/:bindingID", bindings.Remove)

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
		`{"name":"scanobjectnn-objbg","backend":"autodl_elastic","canonical_root":"/root/autodl-fs/datasets/ScanObjectNN","required_markers":["main_split/train.h5"]}`,
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
	if created.Data.EnvironmentVariable != "GEMCP_DATASET_SCANOBJECTNN_OBJBG" || created.Data.Backend != datasetcatalog.BackendElastic {
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
}
