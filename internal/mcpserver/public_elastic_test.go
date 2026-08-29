package mcpserver

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const issue5Commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type issue5Git struct{ archive []byte }

func (g issue5Git) VerifyCommit(context.Context, int, string) error { return nil }

func (g issue5Git) ResolveRef(context.Context, int, string) (string, error) {
	return issue5Commit, nil
}

func (g issue5Git) ArchiveCommit(context.Context, int, string, int64) (gitrepository.Archive, error) {
	return &issue5Archive{data: append([]byte(nil), g.archive...)}, nil
}

type issue5Archive struct{ data []byte }

func (a *issue5Archive) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.data)), nil
}

func (a *issue5Archive) Close() error     { return nil }
func (a *issue5Archive) SizeBytes() int64 { return int64(len(a.data)) }

type issue5Provider struct{}

func (issue5Provider) QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error) {
	return provider.ResourceSnapshot{
		Provider: provider.Summary{Name: "AutoDL Public Elastic", Backend: "elastic", Status: "active"},
		Wallet:   &provider.Wallet{Assets: 1000},
		GPUStock: []provider.GPUStock{{Region: "westDC2", Name: "RTX 4090", Idle: 1, Total: 2}},
		PrivateImages: []provider.Image{{
			UUID: "private-image-visible", Name: "PyTorch 2.1", Status: "finished", Source: "private",
		}},
	}, nil
}

type issue5Runtime struct{}

func (issue5Runtime) Status(context.Context) (execution.RuntimeStatus, error) {
	return execution.RuntimeStatus{
		SchedulerEnabled: true, SchedulerHealthy: true, WatchdogHealthy: true,
		PublicURLConfigured: true, PublicURLHTTPS: true, GlobalConcurrency: 2,
	}, nil
}

func TestIssue5PublicElasticMCPWorkflow(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp-issue5-elastic?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()

	tenant, err := client.Tenant.Create().SetName("Issue 5 validation").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Public Elastic research").SetSlug("public-elastic-research").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Repository.Create().
		SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").
		SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ResourceProfile.Create().
		SetProjectID(project.ID).SetName("public-elastic-4090").SetBackend("autodl_elastic").SetRegion("westDC2").
		SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).
		SetCPUFrom(1).SetCPUTo(16).SetMemoryFromGB(1).SetMemoryToGB(64).
		SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	rawToken, prefix, err := secrets.RandomToken("gmc", 32)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("issue-5-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).
		SetScopes([]string{"read", "submit", "cancel", "configure"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	providerReader := issue5Provider{}
	git := issue5Git{archive: issue5ProposalArchive(t)}
	experiments := experiment.NewService(client, box, git, experiment.WithPreparedExperiments(
		git, git, providerReader, issue5Runtime{}, experiment.ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	handler := New(
		agentauth.NewService(client, box), experiments, "test", nil,
		WithConfiguration(nil, nil, datasetcatalog.NewService(client)),
		WithEnvironments(environmentcatalog.NewService(client, providerReader)),
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	httpClient := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-issue5-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	environmentResult := callIssue5Tool(t, ctx, session, "register_environment", map[string]any{
		"name": "public-elastic-image", "backend": "autodl_elastic", "image_uuid": "private-image-visible", "set_default": true,
	})
	var environment environmentcatalog.View
	decodeStructured(t, environmentResult.StructuredContent, &environment)
	if environment.Backend != "autodl_elastic" || environment.ImageUUID != "private-image-visible" || !environment.IsDefault {
		t.Fatalf("registered environment = %+v", environment)
	}

	datasetResult := callIssue5Tool(t, ctx, session, "register_dataset_binding", map[string]any{
		"catalog": "scanobjectnn-objbg",
		"sources": []map[string]any{
			{"url": "https://huggingface.co/datasets/example/resolve/main/train.h5", "relative_path": "main_split/training_objectdataset_augmentedrot_scale75.h5"},
			{"url": "https://huggingface.co/datasets/example/resolve/main/test.h5", "relative_path": "main_split/test_objectdataset_augmentedrot_scale75.h5"},
		},
	})
	var dataset datasetcatalog.View
	decodeStructured(t, datasetResult.StructuredContent, &dataset)
	if dataset.Backend != "autodl_elastic" || dataset.CanonicalRoot != "/root/autodl-fs/datasets/ScanObjectNN" || len(dataset.Sources) != 2 {
		t.Fatalf("registered dataset = %+v", dataset)
	}

	optionsResult := callIssue5Tool(t, ctx, session, "get_project_options", map[string]any{})
	var options experiment.ProjectOptions
	decodeStructured(t, optionsResult.StructuredContent, &options)
	if len(options.Environments) != 1 || len(options.DatasetBindings) != 1 || len(options.ProviderImages) != 1 ||
		options.ProviderImages[0].UUID != environment.ImageUUID || options.Readiness == nil || options.Readiness.Heartbeat.MonitorTool != "get_experiment" {
		t.Fatalf("Public Elastic project options = %+v", options)
	}

	preparedResult := callIssue5Tool(t, ctx, session, "prepare_experiment", map[string]any{
		"runtime_preset": "provision", "environment": environment.Name,
		"resource_profile": "public-elastic-4090", "dataset": dataset.Name,
	})
	var prepared experiment.PrepareResult
	decodeStructured(t, preparedResult.StructuredContent, &prepared)
	if prepared.Proposal == nil || !prepared.Proposal.Eligible || prepared.Proposal.RuntimePreset != "provision" ||
		prepared.Proposal.Resource.Backend != "autodl_elastic" || prepared.Proposal.Resource.Region != "westDC2" ||
		prepared.Proposal.Resource.Image != environment.ImageUUID || len(prepared.Proposal.Resource.DatasetBindings) != 1 ||
		len(prepared.Proposal.Resource.DatasetBindings[0].Sources) != 2 {
		t.Fatalf("prepared Public Elastic proposal = %+v", prepared)
	}

	submittedResult := callIssue5Tool(t, ctx, session, "submit_prepared_experiment", map[string]any{
		"proposal_id": prepared.Proposal.ID, "confirmation_digest": prepared.Proposal.ConfirmationDigest,
	})
	var submitted experiment.SubmitPreparedResult
	decodeStructured(t, submittedResult.StructuredContent, &submitted)
	if submitted.Experiment.State != "queued" || submitted.Experiment.ExecutionContext.Backend != "autodl_elastic" {
		t.Fatalf("submitted Public Elastic experiment = %+v", submitted)
	}

	monitorResult := callIssue5Tool(t, ctx, session, "get_experiment", map[string]any{"experiment_id": submitted.Experiment.ID})
	var monitored experiment.View
	decodeStructured(t, monitorResult.StructuredContent, &monitored)
	if monitored.State != "queued" || monitored.ExecutionMode != "argv" || len(monitored.Argv) != 1 || monitored.Argv[0] != "gemcp-dataset-provision" ||
		monitored.ExecutionContext.Backend != "autodl_elastic" || monitored.ExecutionContext.Region != "westDC2" ||
		monitored.ExecutionContext.Image != environment.ImageUUID || monitored.ExecutionContext.ResourceProfileName != "public-elastic-4090" ||
		monitored.ExecutionContext.ContainerOutputPath != monitored.OutputPath {
		t.Fatalf("monitored Public Elastic experiment = %+v", monitored)
	}
}

func callIssue5Tool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result.IsError {
		t.Fatalf("%s = %+v, %v", name, result, err)
	}
	return result
}

func issue5ProposalArchive(t *testing.T) []byte {
	t.Helper()
	var result bytes.Buffer
	gzipWriter := gzip.NewWriter(&result)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("print('issue 5')\n")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "smoke.py", Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}
