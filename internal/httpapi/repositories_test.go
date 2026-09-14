package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/auth"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

type httpGitVerifier struct {
	httpsToken []byte
}

func (httpGitVerifier) ProbePublicHTTPS(context.Context, string) error {
	return errors.New("not a public GitHub repository")
}

func (v *httpGitVerifier) VerifyAccess(_ context.Context, auth gitrepository.FetchAuth) error {
	v.httpsToken = append([]byte(nil), auth.HTTPSToken...)
	return nil
}

func (httpGitVerifier) VerifyCommit(context.Context, gitrepository.FetchAuth, string) error {
	return nil
}

func TestRepositoryHTTPSTokenHTTPOmitsSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:repo-https-token-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Research").SetSlug("research").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	verifier := &httpGitVerifier{}
	handlers := NewRepositoryHandlers(gitrepository.NewService(client, box, verifier))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), UserPublicID: "owner-id", Role: "owner",
		}})
		c.Next()
	})
	router.POST("/repositories", handlers.Create)
	router.POST("/repositories/:id/verify", handlers.Verify)
	router.GET("/repositories", handlers.List)

	create := jsonRequest(t, http.MethodPost, "/repositories", gitrepository.Input{
		ProjectID: project.PublicID.String(), URL: "https://github.com/XR-Lee/DynamicPointMamba", DefaultBranch: "main",
	})
	createdResponse := httptest.NewRecorder()
	router.ServeHTTP(createdResponse, create)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		Data gitrepository.View `json:"data"`
	}
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	const token = "github_pat_http_secret_token_value"
	verify := jsonRequest(t, http.MethodPost, "/repositories/"+created.Data.ID+"/verify", gitrepository.OwnerVerifyInput{HTTPSToken: token})
	verifiedResponse := httptest.NewRecorder()
	router.ServeHTTP(verifiedResponse, verify)
	if verifiedResponse.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifiedResponse.Code, verifiedResponse.Body.String())
	}
	body := verifiedResponse.Body.String()
	for _, forbidden := range []string{token, "ciphertext", "https_token_ciphertext"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("verify response leaked %q: %s", forbidden, body)
		}
	}
	var verified struct {
		Data gitrepository.View `json:"data"`
	}
	if err := json.Unmarshal(verifiedResponse.Body.Bytes(), &verified); err != nil {
		t.Fatal(err)
	}
	if verified.Data.Status != "active" || verified.Data.Access != gitrepository.AccessHTTPSToken || !verified.Data.HTTPSTokenConfigured {
		t.Fatalf("verified = %+v", verified.Data)
	}
	if string(verifier.httpsToken) != token {
		t.Fatalf("captured token = %q", verifier.httpsToken)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/repositories?project_id="+project.PublicID.String(), nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), token) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
}
