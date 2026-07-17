package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type allowCommitVerifier struct{}

func (allowCommitVerifier) VerifyCommit(context.Context, int, string) error { return nil }

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func TestStreamableHTTPToolsWithAgentToken(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	repository, _ := client.Repository.Create().
		SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").
		SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	_, _ = client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	_, _ = client.ResourceProfile.Create().
		SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
		SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	rawToken, prefix, _ := secrets.RandomToken("gmc", 32)
	_, _ = client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("test-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).Save(ctx)

	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}),
		"test", nil,
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	unauthorized, _ := http.NewRequest(http.MethodPost, httpServer.URL, nil)
	unauthorizedResponse, err := http.DefaultClient.Do(unauthorized)
	if err != nil {
		t.Fatal(err)
	}
	unauthorizedResponse.Body.Close()
	if unauthorizedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorizedResponse.StatusCode)
	}

	httpClient := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: httpClient, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) != 7 {
		t.Fatalf("tool count = %d, want 7", len(tools.Tools))
	}
	options, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_project_options", Arguments: map[string]any{}})
	if err != nil || options.IsError {
		t.Fatalf("get_project_options = %+v, %v", options, err)
	}
	var optionsOutput experiment.ProjectOptions
	decodeStructured(t, options.StructuredContent, &optionsOutput)
	if optionsOutput.Project.ID != project.PublicID.String() || len(optionsOutput.Repositories) != 1 {
		t.Fatalf("unexpected project options: %+v", optionsOutput)
	}

	submitted, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "submit_experiment", Arguments: map[string]any{
		"repository_id": repository.PublicID.String(),
		"commit_sha":    "0123456789012345678901234567890123456789",
		"command":       "python train.py", "max_runtime_seconds": 3600, "idempotency_key": "mcp-request-0001",
	}})
	if err != nil || submitted.IsError {
		t.Fatalf("submit_experiment = %+v, %v", submitted, err)
	}
	var submitOutput experiment.SubmitResult
	decodeStructured(t, submitted.StructuredContent, &submitOutput)
	if submitOutput.Experiment.State != "queued" || submitOutput.Experiment.ReservedCostMilli != 3500 {
		t.Fatalf("unexpected submit output: %+v", submitOutput)
	}
}

func decodeStructured(t *testing.T, value any, target any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatal(err)
	}
}
