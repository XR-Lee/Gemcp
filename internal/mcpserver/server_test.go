package mcpserver

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
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/guides"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	repositoryservice "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/XR-Lee/Gemcp/internal/workspacecatalog"
	"github.com/google/uuid"
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

func TestPrimeStandaloneSSEWritesCommentBeforeFlush(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := primeStandaloneSSE(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
	}))
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if got := recorder.Body.String(); got != ssePrimer {
		t.Fatalf("primer body = %q, want %q", got, ssePrimer)
	}
	if !recorder.Flushed {
		t.Fatal("response was not flushed")
	}
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
		SetTokenHash(box.Digest("agent-token", rawToken)).SetScopes([]string{"read", "submit", "cancel", "configure"}).Save(ctx)
	node, _ := client.SelfHostedNode.Create().SetTenantID(tenant.ID).SetLabel("usb-pc").SetTokenPrefix("gmn_test").SetTokenHash([]byte("mcp-node-hash")).
		SetStatus("active").SetObservedState("online").SetInstallationID(uuid.NewString()).SetMachineFingerprint("fingerprint").SetHostname("node").
		SetOperatingSystem("linux").SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion("1").Save(ctx)
	_, _ = client.NodeProjectAccess.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetNodeID(node.ID).
		SetExecutionPolicy(nodeprojectaccess.ExecutionPolicyTrustedWorkspace).SetWorkspacePath("/srv/gemcp-workspace").Save(ctx)

	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}),
		"test", nil, WithConfiguration(repositoryservice.NewService(client, box, nil), workspacecatalog.NewService(client), datasetcatalog.NewService(client)),
		WithResearch(research.NewService(client)),
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
		Endpoint: httpServer.URL, HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) != 28 {
		t.Fatalf("tool count = %d, want 28", len(tools.Tools))
	}
	if !strings.Contains(serverInstructions, "never call submit_prepared_experiment until the Owner explicitly confirms that digest") || !strings.Contains(serverInstructions, "submit scope are limits and technical capabilities, not financial approval") {
		t.Fatal("MCP server instructions omit the exact-digest Owner approval boundary")
	}
	toolNames := map[string]bool{}
	for _, tool := range tools.Tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["get_research_workspace"] || !toolNames["update_research_workspace"] || !toolNames["get_next_actions"] || !toolNames["close_run"] ||
		!toolNames["report_agent_activity"] || !toolNames["prepare_experiment"] || !toolNames["submit_prepared_experiment"] || !toolNames["submit_experiment"] ||
		!toolNames["register_repository"] || !toolNames["verify_repository"] || !toolNames["register_workspace_dataset"] ||
		!toolNames["list_dataset_bindings"] || !toolNames["register_dataset_binding"] || !toolNames["remove_dataset_binding"] ||
		!toolNames["register_environment"] || !toolNames["remove_environment"] {
		t.Fatalf("prepared and Advanced tools are not all registered: %+v", toolNames)
	}
	usage, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_usage_guide", Arguments: map[string]any{}})
	if err != nil || usage.IsError {
		t.Fatalf("get_usage_guide = %+v, %v", usage, err)
	}
	var usageOutput UsageGuide
	decodeStructured(t, usage.StructuredContent, &usageOutput)
	if usageOutput.ProjectID != project.PublicID.String() || usageOutput.ResourceURI != guides.AgentResourceURI || !strings.Contains(usageOutput.Markdown, "Non-negotiable rules") {
		t.Fatalf("unexpected usage guide: %+v", usageOutput)
	}

	resources, err := session.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 1 || resources.Resources[0].URI != guides.AgentResourceURI {
		t.Fatalf("ListResources() = %+v, %v", resources, err)
	}
	resource, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: guides.AgentResourceURI})
	if err != nil || len(resource.Contents) != 1 || !strings.Contains(resource.Contents[0].Text, "Required workflow") {
		t.Fatalf("ReadResource() = %+v, %v", resource, err)
	}
	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil || len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != guides.AgentPromptName {
		t.Fatalf("ListPrompts() = %+v, %v", prompts, err)
	}
	prompt, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: guides.AgentPromptName})
	if err != nil || len(prompt.Messages) != 1 {
		t.Fatalf("GetPrompt() = %+v, %v", prompt, err)
	}
	promptText, ok := prompt.Messages[0].Content.(*mcp.TextContent)
	if !ok || !strings.Contains(promptText.Text, "Wait for explicit human approval") {
		t.Fatalf("unexpected prompt content: %+v", prompt.Messages[0].Content)
	}

	options, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_project_options", Arguments: map[string]any{}})
	if err != nil || options.IsError {
		t.Fatalf("get_project_options = %+v, %v", options, err)
	}
	var optionsOutput experiment.ProjectOptions
	decodeStructured(t, options.StructuredContent, &optionsOutput)
	if optionsOutput.Project.ID != project.PublicID.String() || len(optionsOutput.Repositories) != 1 || optionsOutput.SelfHostedNodes == nil {
		t.Fatalf("unexpected project options: %+v", optionsOutput)
	}
	registeredRepository, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_repository", Arguments: map[string]any{
		"name": "dynamic-point-mamba", "ssh_url": "git@github.com:zhangtianyu00824/DynamicPointMamba.git", "default_branch": "main",
	}})
	if err != nil || registeredRepository.IsError {
		t.Fatalf("register_repository = %+v, %v", registeredRepository, err)
	}
	var repositoryOutput repositoryservice.View
	decodeStructured(t, registeredRepository.StructuredContent, &repositoryOutput)
	if repositoryOutput.Status != "pending_key" || !strings.HasPrefix(repositoryOutput.DeployPublicKey, "ssh-ed25519 ") {
		t.Fatalf("registered repository = %+v", repositoryOutput)
	}
	registeredDataset, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_workspace_dataset", Arguments: map[string]any{
		"name": "scanobjectnn-objbg", "relative_path": "data/ScanObjectNN/main_split",
	}})
	if err != nil || registeredDataset.IsError {
		t.Fatalf("register_workspace_dataset = %+v, %v", registeredDataset, err)
	}
	var datasetOutput workspacecatalog.View
	decodeStructured(t, registeredDataset.StructuredContent, &datasetOutput)
	if datasetOutput.EnvironmentVariable != "GEMCP_DATASET_SCANOBJECTNN_OBJBG" || datasetOutput.ContainerPath != "/gemcp/workspace/data/ScanObjectNN/main_split" {
		t.Fatalf("registered dataset = %+v", datasetOutput)
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
	if submitOutput.Experiment.State != "queued" || submitOutput.Experiment.ReservedCostMilli != 6575 {
		t.Fatalf("unexpected submit output: %+v", submitOutput)
	}

	forbidden, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_ssh_cloud_node", Arguments: map[string]any{
		"ssh": "ssh ubuntu@203.0.113.10", "auth_method": "password", "password": "super-secret-password",
	}})
	if err != nil {
		t.Fatalf("register_ssh_cloud_node without operate_nodes err=%v", err)
	}
	encodedForbidden, _ := json.Marshal(forbidden)
	if !forbidden.IsError || !strings.Contains(strings.ToLower(string(encodedForbidden)), "scope") {
		t.Fatalf("register_ssh_cloud_node without operate_nodes = %s", encodedForbidden)
	}
	if strings.Contains(string(encodedForbidden), "super-secret-password") {
		t.Fatal("forbidden tool result leaked the password")
	}
}

func TestRegisterSSHCloudNodeRequiresOperateNodesAndNeverReturnsSecret(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp-ssh?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	rawToken, prefix, _ := secrets.RandomToken("gmc", 32)
	_, _ = client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("node-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).SetScopes([]string{"read", "submit", "operate_nodes"}).Save(ctx)
	config := sshcloud.DefaultConfig()
	config.Enabled = true
	config.InstanceID = "test"
	sshService, err := sshcloud.NewService(client, box, config)
	if err != nil {
		t.Fatal(err)
	}
	sshService.WithDial(func(context.Context, sshcloud.Target, sshcloud.Credential, string) (sshcloud.Conn, string, error) {
		return nil, "", errors.New("probe should stay in the background")
	})

	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}),
		"test", nil, WithSSHCloud(sshService),
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	httpClient := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	registered, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_ssh_cloud_node", Arguments: map[string]any{
		"ssh": "ssh ubuntu@203.0.113.10", "auth_method": "password", "password": "super-secret-password", "label": "lab",
	}})
	if err != nil || registered.IsError {
		t.Fatalf("register_ssh_cloud_node = %+v, %v", registered, err)
	}
	encoded, _ := json.Marshal(registered.StructuredContent)
	if !strings.Contains(string(encoded), "203.0.113.10") || !strings.Contains(string(encoded), "ubuntu") {
		t.Fatalf("registered node = %s", encoded)
	}
	if strings.Contains(string(encoded), "super-secret-password") || strings.Contains(strings.ToLower(string(encoded)), "ciphertext") {
		t.Fatalf("register leaked credential: %s", encoded)
	}
}

func TestRegisterEnvironmentSSHCloudHostAfterLocalNode(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp-local-env?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	rawToken, prefix, _ := secrets.RandomToken("gmc", 32)
	_, _ = client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("local-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).
		SetScopes([]string{"read", "submit", "configure", "operate_nodes"}).Save(ctx)
	config := sshcloud.DefaultConfig()
	config.Enabled = true
	config.LocalProcessEnabled = true
	config.InstanceID = "test"
	sshService, err := sshcloud.NewService(client, box, config)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}),
		"test", nil, WithSSHCloud(sshService), WithEnvironments(environmentcatalog.NewService(client, nil)),
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	httpClient := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	registered, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_ssh_cloud_node", Arguments: map[string]any{
		"host": "127.0.0.1",
	}})
	if err != nil || registered.IsError {
		t.Fatalf("register_ssh_cloud_node without invented secrets = %+v, %v", registered, err)
	}
	environment, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_environment", Arguments: map[string]any{
		"name": "local-host", "image_uuid": "cpu",
	}})
	if err != nil || environment.IsError {
		t.Fatalf("register_environment = %+v, %v", environment, err)
	}
	encoded, _ := json.Marshal(environment.StructuredContent)
	if !strings.Contains(string(encoded), "ssh_cloud") || !strings.Contains(string(encoded), "host") {
		t.Fatalf("environment = %s", encoded)
	}
}

func TestRegisterEnvironmentInvisibleImageFallsBackToLocalCPU(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp-local-fallback?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	rawToken, prefix, _ := secrets.RandomToken("gmc", 32)
	_, _ = client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("local-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).
		SetScopes([]string{"read", "submit", "configure"}).Save(ctx)
	config := sshcloud.DefaultConfig()
	config.Enabled = true
	config.LocalProcessEnabled = true
	config.InstanceID = "test"
	sshService, err := sshcloud.NewService(client, box, config)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}, experiment.WithSSHCloud(sshService)),
		"test", nil, WithSSHCloud(sshService), WithEnvironments(environmentcatalog.NewService(client, nil)),
		WithConfiguration(nil, nil, datasetcatalog.NewService(client)),
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	httpClient := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	environment, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_environment", Arguments: map[string]any{
		"name": "torch-train", "image_uuid": "image-visible",
	}})
	if err != nil || environment.IsError {
		t.Fatalf("invisible image should fall back to local CPU: %+v, %v", environment, err)
	}
	dataset, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_dataset_binding", Arguments: map[string]any{
		"catalog": "modelnet40-mini",
	}})
	if err != nil || dataset.IsError {
		t.Fatalf("catalog dataset = %+v, %v", dataset, err)
	}
}

func TestRegisterLocalCPUStubAllowsSubmitWithoutOperateNodes(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp-local-submit?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	rawToken, prefix, _ := secrets.RandomToken("gmc", 32)
	_, _ = client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("smoke-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).
		SetScopes([]string{"read", "submit", "cancel"}).Save(ctx)
	config := sshcloud.DefaultConfig()
	config.Enabled = true
	config.LocalProcessEnabled = true
	config.InstanceID = "test"
	sshService, err := sshcloud.NewService(client, box, config)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}, experiment.WithSSHCloud(sshService), experiment.WithLocalCPUDataset(datasetcatalog.NewService(client))),
		"test", nil, WithSSHCloud(sshService), WithEnvironments(environmentcatalog.NewService(client, nil)),
		WithConfiguration(nil, nil, datasetcatalog.NewService(client)),
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	httpClient := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	node, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_ssh_cloud_node", Arguments: map[string]any{}})
	if err != nil || node.IsError {
		t.Fatalf("loopback CPU stub with submit = %+v, %v", node, err)
	}
	environment, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_environment", Arguments: map[string]any{
		"name": "cpu", "image_uuid": "host",
	}})
	if err != nil || environment.IsError {
		t.Fatalf("host environment with submit = %+v, %v", environment, err)
	}
	dataset, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_dataset_binding", Arguments: map[string]any{
		"catalog": "modelnet40-mini",
	}})
	if err != nil || dataset.IsError {
		t.Fatalf("modelnet40-mini with submit = %+v, %v", dataset, err)
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
