package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/gin-gonic/gin"
)

func TestEmergencyStopRequiresServerSideSTOPConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{TenantID: 1, UserPublicID: "owner-1", Role: "owner"}})
		c.Next()
	})
	router.POST("/emergency", NewRuntimeHandlers(nil).EmergencyStop)
	request := httptest.NewRequest(http.MethodPost, "/emergency", strings.NewReader(`{"confirmation":"stop"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRuntimeAndNotificationOperationsRequireOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{TenantID: 1, UserPublicID: "member-1", Role: "member"}})
		c.Next()
	})
	runtimeHandlers := NewRuntimeHandlers(nil)
	notificationHandlers := NewNotificationHandlers(nil)
	router.GET("/managed", runtimeHandlers.List)
	router.GET("/runtime/status", runtimeHandlers.Status)
	router.POST("/emergency", runtimeHandlers.EmergencyStop)
	router.GET("/notifications", notificationHandlers.List)
	router.PUT("/notifications/settings", notificationHandlers.Configure)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/managed", nil),
		httptest.NewRequest(http.MethodGet, "/runtime/status", nil),
		httptest.NewRequest(http.MethodPost, "/emergency", nil),
		httptest.NewRequest(http.MethodGet, "/notifications", nil),
		httptest.NewRequest(http.MethodPut, "/notifications/settings", nil),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
}
