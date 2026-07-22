package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/finance"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestFinanceHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Finance HTTP").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").
		SetMonthlyBudgetMilli(100_000).SetMaxExperimentMilli(50_000).SetTimezone("UTC").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	service := finance.NewService(client, finance.WithClock(func() time.Time {
		return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	}))
	handlers := NewFinanceHandlers(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: tenant.ID, UserPublicID: "owner-1", Role: "owner",
		}})
		c.Next()
	})
	router.GET("/finance", handlers.Dashboard)
	router.POST("/projects/:id/budget-adjustments", handlers.Adjust)

	body := `{"direction":"credit","amount_milli":25000,"reason":"Initial AutoDL test credit","idempotency_key":"finance-http-001"}`
	request := httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/budget-adjustments", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"direction":"credit"`) {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/projects/"+project.PublicID.String()+"/budget-adjustments", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"idempotent":true`) {
		t.Fatalf("repeat status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/finance?period=2026-07&project_id="+project.PublicID.String(), nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"credits_milli":25000`) || !strings.Contains(response.Body.String(), `"available_milli":125000`) {
		t.Fatalf("dashboard status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/finance?period=bad", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid period status=%d body=%s", response.Code, response.Body.String())
	}
}
