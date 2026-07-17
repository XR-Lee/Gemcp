package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/buildinfo"
	"github.com/XR-Lee/Gemcp/internal/config"
)

type fakeDatabase struct{ err error }

func (f fakeDatabase) Ping(context.Context) error { return f.err }

func testServer(db Database) http.Handler {
	return New(Dependencies{
		Config: config.Config{Environment: "test", Address: ":0"},
		Build:  buildinfo.New("test", "abc123", "now"),
		DB:     db,
	}).Handler
}

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestReadinessFailure(t *testing.T) {
	response := httptest.NewRecorder()
	testServer(fakeDatabase{err: errors.New("down")}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestVersion(t *testing.T) {
	response := httptest.NewRecorder()
	testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body == "" || !containsAll(body, "Gemcp", "abc123") {
		t.Fatalf("unexpected body: %s", body)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestRunnerEndpointsRequireBearerAndDisableCaching(t *testing.T) {
	response := httptest.NewRecorder()
	testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/runner/spec", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		found := false
		for i := 0; i+len(part) <= len(value); i++ {
			if value[i:i+len(part)] == part {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
