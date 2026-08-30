package mcpserver

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/httpapi"
	"github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/runner"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/watchdog"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	livePaidConfirmation      = "I_ACCEPT_AUTODL_CHARGES"
	livePaidSpendCapMilli     = int64(350)
	livePaidPriceMilliPerHour = int64(1980)
	livePaidOwnedDuration     = 11 * time.Minute
	livePaidCleanupDuration   = 30 * time.Second
	livePaidProvisionTimeout  = 10 * time.Minute
	livePaidExperimentRuntime = 5
	livePaidShutdownReserve   = 30 * time.Second
	livePaidDatasetRoot       = "/root/autodl-fs/projects"
	livePaidDatasetName       = "live-mcp-output-root"
	livePaidResourceProfile   = "live-paid-public-elastic"
	livePaidEnvironment       = "live-paid-public-elastic-image"
	livePaidCloudflaredWait   = 45 * time.Second
	livePaidTerminalPoll      = 2 * time.Second
	livePaidExpectedLog       = "issue5-live-mcp-ok"
	livePaidTestVersion       = "issue5-live-paid-test"
)

var livePaidTunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
var livePaidHTTPSURL = regexp.MustCompile(`https://[a-zA-Z0-9.-]+(?::[0-9]+)?`)

func TestSelectLivePaidGPUChoices(t *testing.T) {
	names, totalIdle := selectLivePaidGPUChoices(autodl.GPUStock{
		"RTX 4090": {Idle: 1, Total: 2},
		"RTX 3090": {Idle: 4, Total: 8},
		"busy":     {Idle: 0, Total: 3},
		"  ":       {Idle: 2, Total: 2},
	})
	if !reflect.DeepEqual(names, []string{"RTX 3090", "RTX 4090"}) || totalIdle != 5 {
		t.Fatalf("selectLivePaidGPUChoices() = %v, %d", names, totalIdle)
	}
}

func TestLivePaidRunnerTunnelPreflight(t *testing.T) {
	if strings.TrimSpace(os.Getenv("GEMCP_TEST_TUNNEL_PREFLIGHT")) != "1" {
		t.Skip("GEMCP_TEST_TUNNEL_PREFLIGHT is not set")
	}
	localServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/runner/bootstrap" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer localServer.Close()
	tunnel := startLivePaidTunnel(t, findCloudflared(t), localServer.URL)
	defer tunnel.Close()
	t.Log("non-paid Runner tunnel preflight succeeded")
}

// TestLivePaidIssue5MCPWorkflow is intentionally skipped unless all three
// live-spend gates are present. It creates and deletes a real AutoDL Job.
func TestLivePaidIssue5MCPWorkflow(t *testing.T) {
	token := readLivePaidToken(t)
	requireLivePaidAuthorization(t)
	cloudflared := findCloudflared(t)
	t.Log("live spend gates accepted; no Provider resource exists yet")

	client := enttest.Open(t, dialect.SQLite, "file:mcp-issue5-live-paid?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancelSetup()

	tenant, err := client.Tenant.Create().SetName("Issue 5 live paid validation").Save(setupCtx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Issue 5 live paid validation").SetSlug("issue-5-live-paid").
		SetMonthlyBudgetMilli(2000).SetMaxExperimentMilli(1000).SetMaxRuntimeSeconds(300).
		SetTimeoutExtensionSeconds(0).SetTerminationGraceSeconds(0).Save(setupCtx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Repository.Create().
		SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").
		SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:live-paid-test").SetStatus("active").Save(setupCtx)
	if err != nil {
		t.Fatal(err)
	}
	region := strings.TrimSpace(os.Getenv("GEMCP_TEST_ELASTIC_REGION"))
	if region == "" {
		region = "westDC2"
	}
	inventoryClient, err := autodl.NewClient(autodl.DefaultBaseURL, token, autodl.WithUserAgent("Gemcp/"+livePaidTestVersion+" inventory"))
	if err != nil {
		t.Fatal(err)
	}
	stock, _, err := inventoryClient.ElasticGPUStock(setupCtx, region, nil)
	if err != nil {
		t.Fatalf("read Public Elastic GPU inventory before spend: %v", err)
	}
	gpuNames, totalIdle := selectLivePaidGPUChoices(stock)
	if len(gpuNames) == 0 {
		t.Fatalf("Public Elastic region %q has no idle GPU reported before spend", region)
	}
	t.Logf("live inventory preflight selected %d GPU types with %d idle cards before spend", len(gpuNames), totalIdle)
	_, err = client.ResourceProfile.Create().
		SetProjectID(project.ID).SetName(livePaidResourceProfile).SetBackend("autodl_elastic").SetRegion(region).
		SetGpuNames(gpuNames).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).
		SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
		SetPriceFromMilli(10).SetPriceToMilli(livePaidPriceMilliPerHour).SetIsDefault(true).Save(setupCtx)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := provider.EncryptCredential(box, token)
	clearString(&token)
	if err != nil {
		t.Fatal(err)
	}
	account, err := client.ProviderAccount.Create().
		SetTenantID(tenant.ID).SetName("AutoDL Public Elastic live paid").SetBackend("elastic").
		SetBaseURL(autodl.DefaultBaseURL).SetCredentialCiphertext(ciphertext).SetStatus("active").Save(setupCtx)
	if err != nil {
		t.Fatal(err)
	}
	rawAgentToken, prefix, err := secrets.RandomToken("gmc", 32)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.AgentToken.Create().SetProjectID(project.ID).SetLabel("issue-5-live-paid-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawAgentToken)).SetScopes([]string{"read", "submit", "cancel", "configure"}).Save(setupCtx)
	if err != nil {
		t.Fatal(err)
	}

	lifecycle := execution.NewProvider(box, livePaidTestVersion)
	providerService := provider.NewService(client, box, livePaidTestVersion)
	git := issue5Git{archive: livePaidArchive(t)}
	runnerHandlers := httpapi.NewRunnerHandlers(runner.NewService(client, box, git, runner.WithSourceMaxBytes(1<<20)))

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.GET("/api/v1/runner/bootstrap", runnerHandlers.Bootstrap)
	router.GET("/api/v1/runner/spec", runnerHandlers.Spec)
	router.GET("/api/v1/runner/source", runnerHandlers.Source)
	router.POST("/api/v1/runner/events", runnerHandlers.Event)
	localServer := httptest.NewServer(router)
	defer localServer.Close()
	tunnel := startLivePaidTunnel(t, cloudflared, localServer.URL)
	defer tunnel.Close()
	t.Log("temporary HTTPS Runner callback tunnel is ready")

	engineConfig := execution.DefaultConfig()
	engineConfig.Enabled = true
	engineConfig.PublicURL = tunnel.URL
	engineConfig.InstanceID = "issue5-live-paid-scheduler"
	engineConfig.GlobalConcurrency = 1
	engineConfig.PollInterval = livePaidTerminalPoll
	engineConfig.LeaseDuration = 30 * time.Second
	engineConfig.ProvisionTimeout = livePaidProvisionTimeout
	engineConfig.ReconcileDelay = livePaidTerminalPoll
	engineConfig.CallbackGrace = 5 * time.Second
	engineConfig.MaxAttempts = 1
	engineConfig.RunnerTokenExtraTTL = 0
	engine, err := execution.NewEngine(client, box, lifecycle, engineConfig)
	if err != nil {
		t.Fatal(err)
	}
	watchdogConfig := watchdog.DefaultConfig()
	watchdogConfig.InstanceID = "issue5-live-paid-watchdog"
	watchdogConfig.PollInterval = livePaidTerminalPoll
	watchdogConfig.LeaseDuration = 30 * time.Second
	watchdogConfig.ReconcileDelay = livePaidTerminalPoll
	watchdogService, err := watchdog.New(client, lifecycle, watchdogConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(setupCtx); err != nil {
		t.Fatalf("write scheduler heartbeat: %v", err)
	}
	if err := watchdogService.Tick(setupCtx); err != nil {
		t.Fatalf("write Watchdog heartbeat: %v", err)
	}

	runtimeOperations := execution.NewOperations(client, execution.WithRuntimeConfiguration(true, 1, true, true))
	experiments := experiment.NewService(client, box, git, experiment.WithPreparedExperiments(
		git, git, providerService, runtimeOperations, experiment.ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	mcpHandler := New(
		agentauth.NewService(client, box), experiments, livePaidTestVersion, nil,
		WithConfiguration(nil, nil, datasetcatalog.NewService(client)),
		WithEnvironments(environmentcatalog.NewService(client, providerService)),
	).Handler()
	mcpServer := httptest.NewServer(mcpHandler)
	defer mcpServer.Close()
	mcpHTTPClient := &http.Client{Transport: bearerTransport{token: rawAgentToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-issue5-live-paid", Version: livePaidTestVersion}, nil)
	session, err := mcpClient.Connect(setupCtx, &mcp.StreamableClientTransport{Endpoint: mcpServer.URL, HTTPClient: mcpHTTPClient}, nil)
	if err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	defer session.Close()

	optionsResult := callIssue5Tool(t, setupCtx, session, "get_project_options", map[string]any{})
	var options experiment.ProjectOptions
	decodeStructured(t, optionsResult.StructuredContent, &options)
	preferredImageUUID := strings.TrimSpace(os.Getenv("GEMCP_TEST_ELASTIC_IMAGE_UUID"))
	imageUUID := selectPrivateImage(options.ProviderImages, preferredImageUUID)
	if imageUUID == "" {
		if preferredImageUUID != "" {
			t.Fatal("the phase-zero validated image is not currently Provider-visible")
		}
		t.Fatal("get_project_options returned no usable Provider-visible private image")
	}
	if preferredImageUUID != "" {
		t.Log("MCP get_project_options confirmed the phase-zero validated private image")
	} else {
		t.Log("MCP get_project_options discovered a Provider-visible private image")
	}
	stockAvailable := false
	for _, stock := range options.ResourceProfiles {
		if stock.Name == livePaidResourceProfile && stock.Region == region {
			stockAvailable = true
			break
		}
	}
	if !stockAvailable || options.Readiness == nil || options.Readiness.Heartbeat.MonitorTool != "get_experiment" {
		t.Fatalf("get_project_options did not report the configured live runtime as ready: readiness=%+v", options.Readiness)
	}

	environmentResult := callIssue5Tool(t, setupCtx, session, "register_environment", map[string]any{
		"name": livePaidEnvironment, "backend": "autodl_elastic", "image_uuid": imageUUID, "set_default": true,
	})
	var environment environmentcatalog.View
	decodeStructured(t, environmentResult.StructuredContent, &environment)
	if environment.ImageUUID != imageUUID || environment.Backend != "autodl_elastic" {
		t.Fatalf("register_environment returned an unexpected environment: %+v", environment)
	}
	datasetResult := callIssue5Tool(t, setupCtx, session, "register_dataset_binding", map[string]any{
		"name": livePaidDatasetName, "backend": "autodl_elastic", "canonical_root": livePaidDatasetRoot,
	})
	var dataset datasetcatalog.View
	decodeStructured(t, datasetResult.StructuredContent, &dataset)
	if dataset.CanonicalRoot != livePaidDatasetRoot || dataset.Backend != "autodl_elastic" {
		t.Fatalf("register_dataset_binding returned an unexpected binding: %+v", dataset)
	}
	preparedResult := callIssue5Tool(t, setupCtx, session, "prepare_experiment", map[string]any{
		"argv": []string{"python", "smoke.py"}, "runtime_preset": "smoke", "max_runtime_seconds": livePaidExperimentRuntime,
		"environment": environment.Name, "resource_profile": livePaidResourceProfile, "dataset": dataset.Name,
	})
	var prepared experiment.PrepareResult
	decodeStructured(t, preparedResult.StructuredContent, &prepared)
	if prepared.Proposal == nil || !prepared.Proposal.Eligible || prepared.Proposal.Resource.Backend != "autodl_elastic" ||
		prepared.Proposal.Resource.Region != region || prepared.Proposal.Resource.Image != imageUUID ||
		prepared.Proposal.MaxRuntimeSeconds != livePaidExperimentRuntime || prepared.Proposal.ReservedCostMilli > livePaidSpendCapMilli {
		t.Fatalf("prepare_experiment did not preserve the live Public Elastic specification and conservative reservation: %+v", prepared.Proposal)
	}
	t.Logf("MCP proposal is eligible; conservative ledger reservation=%d milli-CNY", prepared.Proposal.ReservedCostMilli)

	liveCtx, cancelLive := context.WithTimeout(context.Background(), livePaidOwnedDuration)
	defer cancelLive()
	cleanupComplete := false
	defer func() {
		if cleanupComplete {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), livePaidCleanupDuration)
		defer cancel()
		if cleanupErr := forceLivePaidCleanup(cleanupCtx, client, lifecycle, account.ID); cleanupErr != nil {
			t.Errorf("compensating Provider cleanup failed: %v", cleanupErr)
		} else {
			t.Log("compensating Provider cleanup confirmed no owned deployment remains")
		}
	}()

	submittedResult := callIssue5Tool(t, liveCtx, session, "submit_prepared_experiment", map[string]any{
		"proposal_id": prepared.Proposal.ID, "confirmation_digest": prepared.Proposal.ConfirmationDigest,
	})
	var submitted experiment.SubmitPreparedResult
	decodeStructured(t, submittedResult.StructuredContent, &submitted)
	if submitted.Experiment.State != "queued" {
		t.Fatalf("submit_prepared_experiment state = %q, want queued", submitted.Experiment.State)
	}
	t.Log("MCP submission is queued; scheduler dispatch begins")
	experimentDatabaseID := experimentID(t, client, submitted.Experiment.ID)
	var resourceRecord *ent.ProviderResource
	for liveCtx.Err() == nil {
		if err := engine.Tick(liveCtx); err != nil {
			t.Fatalf("scheduler dispatch failed: %v", err)
		}
		resourceRecord, err = client.ProviderResource.Query().Where(
			providerresource.ExperimentIDEQ(experimentDatabaseID),
		).Order(ent.Desc(providerresource.FieldID)).First(liveCtx)
		if err == nil {
			break
		}
		if !ent.IsNotFound(err) {
			t.Fatalf("load managed resource after dispatch: %v", err)
		}
		monitorResult := callIssue5Tool(t, liveCtx, session, "get_experiment", map[string]any{"experiment_id": submitted.Experiment.ID})
		var dispatchView experiment.View
		decodeStructured(t, monitorResult.StructuredContent, &dispatchView)
		if dispatchView.State != "queued" {
			t.Fatalf("scheduler ended before Provider ownership: state=%q failure_code=%v failure_reason=%v", dispatchView.State, dispatchView.FailureCode, dispatchView.FailureReason)
		}
		select {
		case <-liveCtx.Done():
		case <-time.After(livePaidTerminalPoll):
		}
	}
	if liveCtx.Err() != nil {
		t.Fatalf("scheduler did not create a managed resource before the %s deadline", livePaidOwnedDuration)
	}
	t.Log("scheduler created and durably bound the managed Provider resource")
	select {
	case <-liveCtx.Done():
		t.Fatalf("live workflow ended before the first Provider observation: %v", liveCtx.Err())
	case <-time.After(livePaidTerminalPoll):
	}
	if err := engine.Tick(liveCtx); err != nil {
		t.Fatalf("scheduler Provider observation failed: %v", err)
	}
	t.Log("scheduler completed the first real Provider observation")

	var monitored experiment.View
	var resourceErr error
	observedBackend := false
	lastState := ""
	lastProviderState := ""
	nextProviderObservation := time.Time{}
	for liveCtx.Err() == nil {
		if err := watchdogService.Tick(liveCtx); err != nil {
			t.Fatalf("Watchdog tick failed: %v", err)
		}
		resourceRecord, resourceErr = client.ProviderResource.Query().Where(
			providerresource.ExperimentIDEQ(experimentDatabaseID),
		).Order(ent.Desc(providerresource.FieldID)).First(liveCtx)
		if resourceErr != nil {
			t.Fatalf("load managed resource: %v", resourceErr)
		}
		if resourceRecord.State == providerresource.StateDeleted {
			if err := engine.Tick(liveCtx); err != nil {
				t.Fatalf("scheduler finalization failed: %v", err)
			}
		}
		if resourceRecord.ProviderID != nil && !time.Now().Before(nextProviderObservation) {
			observation, observationErr := lifecycle.Observe(liveCtx, account, *resourceRecord.ProviderID)
			if observationErr == nil && observation.Found {
				providerState := observation.Deployment.Status + "/" + containerStatusSummary(observation)
				if providerState != lastProviderState {
					t.Logf("Provider observation deployment/containers=%s", providerState)
					lastProviderState = providerState
				}
			}
			nextProviderObservation = time.Now().Add(10 * time.Second)
		}
		monitorResult := callIssue5Tool(t, liveCtx, session, "get_experiment", map[string]any{"experiment_id": submitted.Experiment.ID})
		decodeStructured(t, monitorResult.StructuredContent, &monitored)
		observedBackend = observedBackend || monitored.BackendObservation != nil
		if monitored.State != lastState {
			t.Logf("MCP get_experiment state=%s failure_code=%s runner_stage=%s", monitored.State, optionalString(monitored.FailureCode), optionalString(monitored.RunnerStage))
			lastState = monitored.State
		}
		if isLivePaidTerminal(monitored.State) {
			break
		}
		select {
		case <-liveCtx.Done():
		case <-time.After(livePaidTerminalPoll):
		}
	}
	if liveCtx.Err() != nil {
		t.Fatalf("live workflow exceeded the %s owned-resource deadline", livePaidOwnedDuration)
	}
	if monitored.State != "succeeded" || monitored.ExitCode == nil || *monitored.ExitCode != 0 {
		t.Fatalf("terminal experiment state=%q exit_code=%s failure_code=%s failure_reason=%s runner_stage=%s", monitored.State, optionalInt(monitored.ExitCode), optionalString(monitored.FailureCode), optionalString(monitored.FailureReason), optionalString(monitored.RunnerStage))
	}
	if monitored.RunnerSourceDownloads == nil || *monitored.RunnerSourceDownloads < 1 || monitored.LogTail == nil || !strings.Contains(*monitored.LogTail, livePaidExpectedLog) {
		t.Fatalf("Runner evidence is incomplete: downloads=%v log_marker=%t", monitored.RunnerSourceDownloads, monitored.LogTail != nil && strings.Contains(*monitored.LogTail, livePaidExpectedLog))
	}
	if monitored.Metrics["issue5_live"] != true || monitored.Metrics["dataset_binding_visible"] != true {
		t.Fatalf("Runner metrics are incomplete: %+v", monitored.Metrics)
	}
	if !observedBackend || monitored.BackendObservation == nil || !monitored.BackendObservation.CleanupComplete {
		t.Fatalf("backend observation or cleanup evidence is incomplete: %+v", monitored.BackendObservation)
	}
	resourceRecord, err = client.ProviderResource.Query().Where(
		providerresource.ExperimentIDEQ(experimentDatabaseID),
	).Order(ent.Desc(providerresource.FieldID)).First(setupCtx)
	if err != nil || resourceRecord.State != providerresource.StateDeleted || resourceRecord.ProviderID == nil {
		t.Fatalf("managed resource was not durably deleted: state=%v error=%v", resourceRecordState(resourceRecord), err)
	}
	watchdogAudits, err := client.AuditEvent.Query().Where(
		auditevent.ActionEQ("watchdog.stop_enforced"), auditevent.TargetIDEQ(resourceRecord.PublicID.String()),
	).Count(setupCtx)
	if err != nil || watchdogAudits != 1 {
		t.Fatalf("Watchdog cleanup audit count=%d error=%v", watchdogAudits, err)
	}
	observation, err := lifecycle.Observe(setupCtx, account, *resourceRecord.ProviderID)
	if err != nil || observation.Found {
		t.Fatalf("Provider deletion confirmation failed: found=%t error=%v", observation.Found, err)
	}
	cleanupComplete = true
	t.Logf("live paid MCP workflow succeeded: state=%s source_downloads=%d cleanup=deleted estimated_cost_milli=%d", monitored.State, *monitored.RunnerSourceDownloads, monitored.EstimatedCostMilli)
}

func readLivePaidToken(t *testing.T) string {
	t.Helper()
	tokenFile := strings.TrimSpace(os.Getenv("GEMCP_TEST_ELASTIC_TOKEN_FILE"))
	if tokenFile == "" {
		t.Skip("GEMCP_TEST_ELASTIC_TOKEN_FILE is not set")
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read Token file: %v", err)
	}
	token := strings.TrimSpace(string(contents))
	for index := range contents {
		contents[index] = 0
	}
	if token == "" {
		t.Fatal("Token file is empty")
	}
	return token
}

func requireLivePaidAuthorization(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("GEMCP_TEST_LIVE_SPEND_CONFIRM")) != livePaidConfirmation {
		t.Fatalf("live paid test blocked: set GEMCP_TEST_LIVE_SPEND_CONFIRM=%s", livePaidConfirmation)
	}
	capMilli, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("GEMCP_TEST_LIVE_SPEND_CAP_MILLI")), 10, 64)
	if err != nil || capMilli != livePaidSpendCapMilli {
		t.Fatalf("live paid test requires GEMCP_TEST_LIVE_SPEND_CAP_MILLI=%d", livePaidSpendCapMilli)
	}
	maximumSeconds := int64(livePaidProvisionTimeout/time.Second) + livePaidExperimentRuntime + int64(livePaidShutdownReserve/time.Second)
	maximum := int64(math.Ceil(float64(livePaidPriceMilliPerHour*maximumSeconds) / 3600))
	if maximum > capMilli {
		t.Fatalf("live paid test estimate %d milli-CNY exceeds authorized cap %d", maximum, capMilli)
	}
}

type livePaidTunnel struct {
	URL     string
	command *exec.Cmd
	cancel  context.CancelFunc
	done    chan error
	cleanup func() error
}

func startLivePaidTunnel(t *testing.T, executable, origin string) *livePaidTunnel {
	t.Helper()
	if tailscale := findTailscale(); tailscale != "" {
		tunnel, err := tryLivePaidTailscaleTunnel(tailscale, origin)
		if err == nil {
			t.Log("temporary HTTPS tunnel uses Tailscale Funnel")
			return tunnel
		}
		t.Logf("Tailscale Funnel unavailable; falling back to Cloudflare Quick Tunnel: %v", err)
	}
	var failures []string
	for attempt := 1; attempt <= 3; attempt++ {
		tunnel, err := tryLivePaidTunnel(executable, origin)
		if err == nil {
			if attempt > 1 {
				t.Logf("temporary HTTPS tunnel succeeded on attempt %d", attempt)
			}
			return tunnel
		}
		failures = append(failures, fmt.Sprintf("attempt %d: %v", attempt, err))
		t.Logf("temporary HTTPS tunnel attempt %d failed before any Provider spend", attempt)
	}
	t.Fatalf("cloudflared tunnel preflight failed after 3 attempts: %s", strings.Join(failures, "; "))
	return nil
}

func tryLivePaidTailscaleTunnel(executable, origin string) (*livePaidTunnel, error) {
	status, err := exec.Command(executable, "funnel", "status", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("read Funnel status: %w", err)
	}
	var current struct {
		TCP map[string]json.RawMessage `json:"TCP"`
	}
	if err := json.Unmarshal(status, &current); err != nil {
		return nil, fmt.Errorf("decode Funnel status: %w", err)
	}
	port := ""
	for _, candidate := range []string{"8443", "10000"} {
		if _, occupied := current.TCP[candidate]; !occupied {
			port = candidate
			break
		}
	}
	if port == "" {
		return nil, fmt.Errorf("Funnel ports 8443 and 10000 are already configured")
	}

	output, err := exec.Command(executable, "funnel", "--bg", "--https="+port, "--yes", origin).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("start Funnel: %w: %s", err, strings.TrimSpace(string(output)))
	}
	tunnelURL := livePaidHTTPSURL.FindString(string(output))
	if tunnelURL == "" {
		_ = stopLivePaidTailscaleTunnel(executable, port)
		return nil, fmt.Errorf("Funnel did not publish an HTTPS URL")
	}
	tunnel := &livePaidTunnel{
		URL: tunnelURL,
		cleanup: func() error {
			return stopLivePaidTailscaleTunnel(executable, port)
		},
	}
	if err := waitForLivePaidTunnel(tunnel.URL, 30*time.Second); err != nil {
		tunnel.Close()
		return nil, fmt.Errorf("Funnel endpoint preflight: %w", err)
	}
	return tunnel, nil
}

func stopLivePaidTailscaleTunnel(executable, port string) error {
	output, err := exec.Command(executable, "funnel", "--https="+port, "off").CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop Funnel: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func tryLivePaidTunnel(executable, origin string) (*livePaidTunnel, error) {
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, executable, "tunnel", "--no-autoupdate", "--url", origin)
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	command.Stdout = io.Discard
	if err := command.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}
	done := make(chan error, 1)
	urls := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if value := livePaidTunnelURL.FindString(scanner.Text()); value != "" {
				select {
				case urls <- value:
				default:
				}
			}
		}
		done <- command.Wait()
	}()
	tunnel := &livePaidTunnel{command: command, cancel: cancel, done: done}
	select {
	case tunnel.URL = <-urls:
	case err := <-done:
		cancel()
		return nil, fmt.Errorf("cloudflared exited before publishing a tunnel: %w", err)
	case <-time.After(livePaidCloudflaredWait):
		tunnel.Close()
		return nil, fmt.Errorf("cloudflared did not publish a tunnel URL")
	}
	if err := waitForLivePaidTunnel(tunnel.URL, livePaidCloudflaredWait); err == nil {
		return tunnel, nil
	} else {
		tunnel.Close()
		return nil, err
	}
}

func waitForLivePaidTunnel(tunnelURL string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	httpClients := livePaidTunnelHTTPClients()
	lastStatus := 0
	lastError := ""
	for time.Now().Before(deadline) {
		for _, httpClient := range httpClients {
			response, requestErr := httpClient.Get(tunnelURL + "/api/v1/runner/bootstrap")
			if requestErr == nil {
				lastStatus = response.StatusCode
				_ = response.Body.Close()
				if response.StatusCode == http.StatusUnauthorized {
					return nil
				}
			} else {
				lastError = requestErr.Error()
			}
		}
		time.Sleep(time.Second)
	}
	if lastStatus != 0 {
		return fmt.Errorf("Runner endpoint returned HTTP status %d", lastStatus)
	}
	return fmt.Errorf("Runner endpoint request failed: %s", lastError)
}

func livePaidTunnelHTTPClients() []*http.Client {
	clients := []*http.Client{{Timeout: 10 * time.Second}}
	for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		server := server
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "udp", server)
			},
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Resolver: resolver}).DialContext
		clients = append(clients, &http.Client{Transport: transport, Timeout: 10 * time.Second})
	}
	return clients
}

func (tunnel *livePaidTunnel) Close() {
	if tunnel == nil {
		return
	}
	if tunnel.cleanup != nil {
		_ = tunnel.cleanup()
		tunnel.cleanup = nil
	}
	if tunnel.cancel == nil {
		return
	}
	tunnel.cancel()
	select {
	case <-tunnel.done:
	case <-time.After(5 * time.Second):
		if tunnel.command != nil && tunnel.command.Process != nil {
			_ = tunnel.command.Process.Kill()
		}
		select {
		case <-tunnel.done:
		case <-time.After(5 * time.Second):
		}
	}
	tunnel.cancel = nil
}

func findCloudflared(t *testing.T) string {
	t.Helper()
	if configured := strings.TrimSpace(os.Getenv("GEMCP_TEST_CLOUDFLARED")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured
		}
		t.Fatalf("GEMCP_TEST_CLOUDFLARED does not name a file")
	}
	if value, err := exec.LookPath("cloudflared"); err == nil {
		return value
	}
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		candidate := filepath.Join(root, "cloudflared", "cloudflared.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	t.Fatal("cloudflared is required for the live Runner callback tunnel")
	return ""
}

func findTailscale() string {
	if value, err := exec.LookPath("tailscale"); err == nil {
		return value
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		candidate := filepath.Join(root, "Tailscale", "tailscale.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func selectPrivateImage(images []experiment.ProviderImageOption, preferred string) string {
	for _, image := range images {
		if image.Source == "private" && strings.TrimSpace(image.UUID) != "" && (preferred == "" || image.UUID == preferred) {
			return image.UUID
		}
	}
	return ""
}

func containerStatusSummary(observation execution.Observation) string {
	counts := map[string]int{}
	for _, container := range observation.Containers {
		status := strings.ToLower(strings.TrimSpace(container.Status))
		if status == "" {
			status = "unknown"
		}
		counts[status]++
	}
	if len(counts) == 0 {
		return "none"
	}
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s:%d", status, counts[status]))
	}
	return strings.Join(parts, ",")
}

func selectLivePaidGPUChoices(stock autodl.GPUStock) ([]string, int) {
	type choice struct {
		name string
		idle int
	}
	choices := make([]choice, 0, len(stock))
	totalIdle := 0
	for name, entry := range stock {
		name = strings.TrimSpace(name)
		if name == "" || entry.Idle <= 0 {
			continue
		}
		choices = append(choices, choice{name: name, idle: entry.Idle})
		totalIdle += entry.Idle
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].idle == choices[j].idle {
			return choices[i].name < choices[j].name
		}
		return choices[i].idle > choices[j].idle
	})
	names := make([]string, len(choices))
	for index, candidate := range choices {
		names[index] = candidate.name
	}
	return names, totalIdle
}

func livePaidArchive(t *testing.T) []byte {
	t.Helper()
	content := []byte(`import json
import os

root = os.environ.get("GEMCP_DATASET_LIVE_MCP_OUTPUT_ROOT")
if root != "/root/autodl-fs/projects" or not os.path.isdir(root):
    raise SystemExit("dataset binding missing")
output = os.environ["GEMCP_OUTPUT_DIR"]
with open(os.path.join(output, "metrics.json"), "w", encoding="utf-8") as handle:
    json.dump({"issue5_live": True, "dataset_binding_visible": True}, handle)
print("issue5-live-mcp-ok", flush=True)
`)
	var result bytes.Buffer
	gzipWriter := gzip.NewWriter(&result)
	tarWriter := tar.NewWriter(gzipWriter)
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

func forceLivePaidCleanup(ctx context.Context, client *ent.Client, lifecycle *execution.Provider, accountID int) error {
	records, err := client.ProviderResource.Query().Where(
		providerresource.ProviderAccountIDEQ(accountID), providerresource.OwnedEQ(true),
	).WithProviderAccount().All(ctx)
	if err != nil {
		return err
	}
	var failures []string
	for _, record := range records {
		account, edgeErr := record.Edges.ProviderAccountOrErr()
		if edgeErr != nil {
			failures = append(failures, "load Provider account")
			continue
		}
		providerID := ""
		if record.ProviderID != nil {
			providerID = *record.ProviderID
		} else {
			observation, findErr := lifecycle.FindByName(ctx, account, record.Name)
			if findErr != nil {
				failures = append(failures, "reconcile owned deployment")
				continue
			}
			if observation.Found {
				providerID = observation.Deployment.UUID
			}
		}
		if providerID != "" {
			observation, observeErr := lifecycle.Observe(ctx, account, providerID)
			if observeErr != nil {
				failures = append(failures, "observe owned deployment")
				continue
			}
			if !observation.Found {
				_, _ = record.Update().SetState(providerresource.StateDeleted).SetDeletedAt(time.Now().UTC()).Save(ctx)
				continue
			}
			if _, stopErr := lifecycle.Stop(ctx, account, providerID); stopErr != nil {
				failures = append(failures, "stop owned deployment")
				continue
			}
			if _, deleteErr := lifecycle.Delete(ctx, account, providerID); deleteErr != nil {
				failures = append(failures, "delete owned deployment")
				continue
			}
			deleted := false
			for ctx.Err() == nil {
				observation, observeErr = lifecycle.Observe(ctx, account, providerID)
				if observeErr != nil {
					failures = append(failures, "confirm owned deployment deletion")
					break
				}
				if !observation.Found {
					deleted = true
					break
				}
				select {
				case <-ctx.Done():
				case <-time.After(livePaidTerminalPoll):
				}
			}
			if !deleted {
				failures = append(failures, "owned deployment deletion was not confirmed")
				continue
			}
		}
		_, _ = record.Update().SetState(providerresource.StateDeleted).SetDeletedAt(time.Now().UTC()).Save(ctx)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func experimentID(t *testing.T, client *ent.Client, publicID string) int {
	t.Helper()
	parsed, err := uuid.Parse(publicID)
	if err != nil {
		t.Fatalf("parse experiment ID: %v", err)
	}
	record, err := client.Experiment.Query().Where(entexperiment.PublicIDEQ(parsed)).Only(context.Background())
	if err != nil {
		t.Fatalf("load experiment: %v", err)
	}
	return record.ID
}

func isLivePaidTerminal(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "timed_out", "provider_error", "budget_stopped":
		return true
	default:
		return false
	}
}

func resourceRecordState(record *ent.ProviderResource) string {
	if record == nil {
		return "missing"
	}
	return string(record.State)
}

func clearString(value *string) {
	if value != nil {
		*value = ""
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}
