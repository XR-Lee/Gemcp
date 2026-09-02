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
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestAuthHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:httpauth?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	passwordHash, _ := auth.HashPassword("correct horse battery staple")
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	_, _ = client.User.Create().SetTenantID(tenant.ID).SetEmail("owner@example.com").SetPasswordHash(passwordHash).Save(ctx)

	handlers := NewAuthHandlers(auth.NewService(client, box, time.Hour), true)
	router := gin.New()
	router.POST("/login", handlers.Login)
	protected := router.Group("")
	protected.Use(handlers.RequireSession())
	protected.GET("/me", handlers.Me)
	protected.POST("/logout", handlers.Logout)

	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"owner@example.com","password":"correct horse battery staple"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResponse.Code, loginResponse.Body)
	}
	var payload struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	cookies := loginResponse.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
			if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("unsafe session cookie: %+v", cookie)
			}
		}
	}
	if sessionCookie == nil || payload.Data.CSRFToken == "" {
		t.Fatal("login did not return session and CSRF credentials")
	}

	meRequest := httptest.NewRequest(http.MethodGet, "/me", nil)
	meRequest.AddCookie(sessionCookie)
	meResponse := httptest.NewRecorder()
	router.ServeHTTP(meResponse, meRequest)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("me status = %d, body=%s", meResponse.Code, meResponse.Body)
	}

	logoutWithoutCSRF := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logoutWithoutCSRF.AddCookie(sessionCookie)
	logoutResponse := httptest.NewRecorder()
	router.ServeHTTP(logoutResponse, logoutWithoutCSRF)
	if logoutResponse.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF status = %d", logoutResponse.Code)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logoutRequest.AddCookie(sessionCookie)
	logoutRequest.Header.Set(csrfHeaderName, payload.Data.CSRFToken)
	logoutResponse = httptest.NewRecorder()
	router.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body=%s", logoutResponse.Code, logoutResponse.Body)
	}
}

func TestAuthHTTPSkipsPasswordWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:httpauth-skip?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	passwordHash, _ := auth.HashPassword("correct horse battery staple")
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	_, _ = client.User.Create().SetTenantID(tenant.ID).SetEmail("owner@example.com").SetPasswordHash(passwordHash).Save(ctx)

	handlers := NewAuthHandlers(auth.NewService(client, box, time.Hour, auth.WithSkipPassword(true)), true)
	router := gin.New()
	router.POST("/login", handlers.Login)
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"owner@example.com"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("skip-password login status = %d, body=%s", loginResponse.Code, loginResponse.Body)
	}
}
