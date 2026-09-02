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
		Config: config.Config{Environment: "test", Address: ":0", PublicURL: "https://gemcp.example.com"},
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

func TestPublicMCPGuides(t *testing.T) {
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/docs/agent-mcp.md", want: "Non-negotiable rules"},
		{path: "/docs/owner-mcp.md", want: "Owner onboarding checklist"},
	} {
		response := httptest.NewRecorder()
		testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK || !containsAll(response.Body.String(), test.want, "get_project_options", "get_project_cost") {
			t.Fatalf("GET %s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
			t.Fatalf("GET %s Content-Type=%q", test.path, got)
		}
		if got := response.Header().Get("Cache-Control"); got != "public, max-age=300" {
			t.Fatalf("GET %s Cache-Control=%q", test.path, got)
		}
	}
}

func TestPiSetupAssets(t *testing.T) {
	for _, test := range []struct {
		path        string
		contentType string
		want        []string
	}{
		{path: "/agent/setup", contentType: "text/markdown; charset=utf-8", want: []string{"Gemcp MCP Setup", "https://gemcp.example.com/agent/setup/install.mjs", "Codex", "OpenCode", "Claude Code", "Grok"}},
		{path: "/agent/setup/install.mjs", contentType: "text/javascript; charset=utf-8", want: []string{"GEMCP_PI_SETUP_INSTALLER_V1", "const trustedOrigin = 'https://gemcp.example.com'"}},
		{path: "/agent/setup/gemcp-tool.mjs", contentType: "text/javascript; charset=utf-8", want: []string{"GEMCP_TOOL_HELPER_V1", "verifyConfiguredServer"}},
		{path: "/node/setup", contentType: "text/markdown; charset=utf-8", want: []string{"Gemcp Node Setup", "https://gemcp.example.com/node/setup?lang=en#code="}},
	} {
		response := httptest.NewRecorder()
		testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK || !containsAll(response.Body.String(), test.want...) {
			t.Fatalf("GET %s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
		if containsAll(response.Body.String(), "{{GEMCP_PUBLIC_URL}}") {
			t.Fatalf("GET %s retained the public URL placeholder", test.path)
		}
		if got := response.Header().Get("Content-Type"); got != test.contentType {
			t.Fatalf("GET %s Content-Type=%q", test.path, got)
		}
		if got := response.Header().Get("Cache-Control"); got != "public, max-age=300" {
			t.Fatalf("GET %s Cache-Control=%q", test.path, got)
		}
	}
}

func TestNodeSetupLanguageSelection(t *testing.T) {
	for _, test := range []struct {
		path           string
		acceptLanguage string
		wantLanguage   string
		wantContent    string
	}{
		{path: "/node/setup?lang=en", wantLanguage: "en", wantContent: "Gemcp Node Setup"},
		{path: "/node/setup?lang=zh", wantLanguage: "zh-CN", wantContent: "Gemcp 节点部署"},
		{path: "/node/setup", acceptLanguage: "zh-CN,zh;q=0.9,en;q=0.8", wantLanguage: "zh-CN", wantContent: "Gemcp 节点部署"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.acceptLanguage != "" {
			request.Header.Set("Accept-Language", test.acceptLanguage)
		}
		response := httptest.NewRecorder()
		testServer(fakeDatabase{}).ServeHTTP(response, request)
		if response.Code != http.StatusOK || !containsAll(response.Body.String(), test.wantContent, "abc123") {
			t.Fatalf("GET %s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Language"); got != test.wantLanguage {
			t.Fatalf("GET %s Content-Language=%q, want %q", test.path, got, test.wantLanguage)
		}
		if got := response.Header().Get("Vary"); got != "Accept-Language" {
			t.Fatalf("GET %s Vary=%q", test.path, got)
		}
	}

	response := httptest.NewRecorder()
	testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/node/setup?lang=fr", nil))
	if response.Code != http.StatusBadRequest || !containsAll(response.Body.String(), "INVALID_LANGUAGE") {
		t.Fatalf("invalid language status=%d body=%s", response.Code, response.Body.String())
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

func TestImageBakeEndpointsRequireOwnerSession(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/projects/project-id/image-bakes/options"},
		{method: http.MethodGet, path: "/api/v1/projects/project-id/image-bakes"},
		{method: http.MethodPost, path: "/api/v1/projects/project-id/image-bakes"},
		{method: http.MethodGet, path: "/api/v1/projects/project-id/image-bakes/bake-id"},
		{method: http.MethodPost, path: "/api/v1/projects/project-id/image-bakes/bake-id/confirm"},
		{method: http.MethodPost, path: "/api/v1/projects/project-id/image-bakes/bake-id/cancel"},
	} {
		response := httptest.NewRecorder()
		testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != http.StatusUnauthorized || !containsAll(response.Body.String(), "UNAUTHENTICATED") {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s %s Cache-Control=%q", test.method, test.path, got)
		}
	}
}

func TestDiagnosticEndpointsRequireOwnerSession(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/projects/project-id/diagnostics/options"},
		{method: http.MethodPost, path: "/api/v1/projects/project-id/diagnostics/preflight"},
		{method: http.MethodGet, path: "/api/v1/projects/project-id/diagnostics"},
		{method: http.MethodPost, path: "/api/v1/projects/project-id/diagnostics"},
		{method: http.MethodGet, path: "/api/v1/projects/project-id/diagnostics/run-id"},
		{method: http.MethodPost, path: "/api/v1/projects/project-id/diagnostics/run-id/cancel"},
	} {
		response := httptest.NewRecorder()
		testServer(fakeDatabase{}).ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != http.StatusUnauthorized || !containsAll(response.Body.String(), "UNAUTHENTICATED") {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s %s Cache-Control=%q", test.method, test.path, got)
		}
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
