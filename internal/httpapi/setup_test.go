package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetupRequiresBootstrapToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSetupHandlers(nil, "gmb_expected_secret")
	router := gin.New()
	router.POST("/setup", handler.Initialize)

	for _, provided := range []string{"", "wrong"} {
		request := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		if provided != "" {
			request.Header.Set(bootstrapTokenHeader, provided)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("provided=%q status=%d, want %d", provided, response.Code, http.StatusUnauthorized)
		}
	}
}
