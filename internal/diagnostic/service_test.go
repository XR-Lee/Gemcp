package diagnostic

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

type testArchive struct {
	data   []byte
	closed bool
}

func (a *testArchive) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.data)), nil
}
func (a *testArchive) Close() error     { a.closed = true; return nil }
func (a *testArchive) SizeBytes() int64 { return int64(len(a.data)) }

type testArchiver struct {
	data  []byte
	calls int
}

func (a *testArchiver) ArchiveCommit(_ context.Context, _ int, _ string, _ int64) (gitrepository.Archive, error) {
	a.calls++
	return &testArchive{data: append([]byte(nil), a.data...)}, nil
}

type testProvider struct {
	snapshot provider.ResourceSnapshot
	err      error
}

func (p *testProvider) QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error) {
	return p.snapshot, p.err
}

type testRuntime struct{ status execution.RuntimeStatus }

func (r *testRuntime) Status(context.Context) (execution.RuntimeStatus, error) { return r.status, nil }

type diagnosticFixture struct {
	client      *ent.Client
	service     *Service
	project     *ent.Project
	repository  *ent.Repository
	environment *ent.Environment
	profile     *ent.ResourceProfile
	archiver    *testArchiver
	provider    *testProvider
	now         time.Time
	tenantID    int
	actorID     string
}

func newDiagnosticFixture(t *testing.T) *diagnosticFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("tenant").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projectRecord, err := client.Project.Create().SetTenantID(tenant.ID).SetName("project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(10000).SetMaxConcurrency(2).SetMaxRuntimeSeconds(3600).
		SetTimeoutExtensionSeconds(600).SetTerminationGraceSeconds(60).SetTimezone("UTC").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repositoryRecord, err := client.Repository.Create().SetProjectID(projectRecord.ID).SetName("source").
		SetSSHURL("git@github.com:owner/repository.git").SetSSHHost("github.com").SetDefaultBranch("main").
		SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	environmentRecord, err := client.Environment.Create().SetProjectID(projectRecord.ID).SetName("torch").
		SetBackend("autodl_private").SetImageUUID("image-test").SetStatus("approved").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profileRecord, err := client.ResourceProfile.Create().SetProjectID(projectRecord.ID).SetName("rtx3090").
		SetBackend("autodl_private").SetRegion("private").SetGpuNames([]string{"RTX 3090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(118).SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).
		SetPriceFromMilli(10).SetPriceToMilli(1000).SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	archiver := &testArchiver{data: makeArchive(t, map[string]string{"README.md": "diagnostic source"})}
	providerReader := &testProvider{snapshot: provider.ResourceSnapshot{
		Provider:      provider.Summary{Name: "AutoDL", Backend: "private", Status: "active"},
		GPUStock:      []provider.GPUStock{{Name: "RTX 3090", Idle: 1, Total: 1}},
		PrivateImages: []provider.Image{{UUID: "image-test", Name: "torch", Source: "private"}},
	}}
	runtimeReader := &testRuntime{status: execution.RuntimeStatus{
		SchedulerEnabled: true, SchedulerHealthy: true, WatchdogHealthy: true, SelfHostedEnabled: true,
		GlobalConcurrency: 2, PublicURLConfigured: true,
	}}
	experimentService := experiment.NewService(client, box, nil)
	service := NewService(client, box, archiver, providerReader, runtimeReader, experimentService, Config{SelfHostedEnabled: true})
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	return &diagnosticFixture{
		client: client, service: service, project: projectRecord, repository: repositoryRecord,
		environment: environmentRecord, profile: profileRecord, archiver: archiver, provider: providerReader,
		now: now, tenantID: tenant.ID, actorID: "11111111-1111-4111-8111-111111111111",
	}
}

func (f *diagnosticFixture) input() PreflightInput {
	return PreflightInput{
		Backend: BackendAutoDL, Suite: SuiteGPUConnectivity, RepositoryID: f.repository.PublicID.String(),
		EnvironmentID: f.environment.PublicID.String(), ResourceProfileID: f.profile.PublicID.String(),
		CommitSHA: strings.Repeat("a", 40),
	}
}

func TestAutoDLPreflightAndOwnerSubmissionAreBoundedAndIdempotent(t *testing.T) {
	f := newDiagnosticFixture(t)
	ctx := context.Background()
	options, err := f.service.Options(ctx, f.tenantID, f.project.PublicID.String())
	if err != nil || len(options.Repositories) != 1 || len(options.Suites) != 2 {
		t.Fatalf("Options() = %+v, %v", options, err)
	}
	preflight, err := f.service.Preflight(ctx, f.tenantID, f.project.PublicID.String(), f.input())
	if err != nil {
		t.Fatal(err)
	}
	if !preflight.Eligible || !preflight.RequiresConfirmation || !preflight.Proposal.Billable || preflight.Proposal.ReservedCostMilli != 234 ||
		!strings.HasPrefix(preflight.ConfirmationDigest, "sha256:") || preflight.Proposal.ReuseContainer ||
		!strings.Contains(preflight.Proposal.Command, "expected_gpu_count=1") || strings.Contains(preflight.Proposal.Command, expectedGPUCountPlaceholder) {
		t.Fatalf("Preflight() = %+v", preflight)
	}
	submit := SubmitInput{PreflightInput: f.input(), IdempotencyKey: "diagnostic-test-0001"}
	if _, err := f.service.Submit(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), submit); err != ErrConfirmationRequired {
		t.Fatalf("unconfirmed Submit() error = %v", err)
	}
	submit.Confirmed = true
	submit.ConfirmationDigest = preflight.ConfirmationDigest
	created, err := f.service.Submit(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), submit)
	if err != nil {
		t.Fatal(err)
	}
	if created.Idempotent || created.Run.Experiment.State != "queued" || created.Run.Assessment.Classification != "waiting_for_scheduler" {
		t.Fatalf("created diagnostic = %+v", created)
	}
	repeated, err := f.service.Submit(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), submit)
	if err != nil || !repeated.Idempotent || repeated.Run.ID != created.Run.ID {
		t.Fatalf("repeated diagnostic = %+v, %v", repeated, err)
	}
	experimentRecord, err := f.client.Experiment.Query().Only(ctx)
	if err != nil || experimentRecord.AgentTokenID != nil || experimentRecord.TimeoutExtensionSeconds != 0 || experimentRecord.ReservedCostMilli != 234 {
		t.Fatalf("diagnostic Experiment = %+v, %v", experimentRecord, err)
	}
	if count, _ := f.client.DiagnosticRun.Query().Count(ctx); count != 1 {
		t.Fatalf("DiagnosticRun count = %d", count)
	}
	listed, err := f.service.List(ctx, f.tenantID, f.project.PublicID.String(), 25)
	if err != nil || len(listed.Runs) != 1 || listed.Runs[0].ID != created.Run.ID || listed.Runs[0].Experiment.State != "queued" {
		t.Fatalf("List() summary = %+v, %v", listed, err)
	}
	if count, _ := f.client.BudgetEntry.Query().Count(ctx); count != 1 {
		t.Fatalf("BudgetEntry count = %d", count)
	}
	otherTenant, err := f.client.Tenant.Create().SetName("other-tenant").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Get(ctx, otherTenant.ID, f.project.PublicID.String(), created.Run.ID); err != ErrNotFound {
		t.Fatalf("cross-tenant Get() error = %v", err)
	}
	queuedPreflight, err := f.service.Preflight(ctx, f.tenantID, f.project.PublicID.String(), f.input())
	if err != nil || checkStatus(queuedPreflight.Checks, "project_concurrency") != CheckPass || checkStatus(queuedPreflight.Checks, "global_concurrency") != CheckPass {
		t.Fatalf("queued Experiment must not consume execution concurrency: %+v, %v", queuedPreflight.Checks, err)
	}
	conflict := submit
	conflict.Suite = SuitePyTorchCUDA
	if _, err := f.service.Submit(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), conflict); err != ErrIdempotencyConflict {
		t.Fatalf("conflicting Submit() error = %v", err)
	}
	cancelled, err := f.service.Cancel(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), created.Run.ID)
	if err != nil || cancelled.Experiment.State != "cancelled" || !cancelled.Assessment.CleanupComplete {
		t.Fatalf("Cancel() = %+v, %v", cancelled, err)
	}
	if count, _ := f.client.BudgetEntry.Query().Count(ctx); count != 2 {
		t.Fatalf("BudgetEntry count after cancellation = %d", count)
	}
	cancelAudit, err := f.client.AuditEvent.Query().Where(auditevent.ActionEQ("experiment.cancel_requested")).Only(ctx)
	if err != nil || cancelAudit.ActorType != auditevent.ActorTypeUser || cancelAudit.ActorID != f.actorID {
		t.Fatalf("cancellation audit = %+v, %v", cancelAudit, err)
	}
}

func TestPreflightRejectsUnsafeArchiveAndUnavailableCapacity(t *testing.T) {
	f := newDiagnosticFixture(t)
	f.archiver.data = makeArchive(t, map[string]string{"../secret": "unsafe"})
	f.provider.snapshot.GPUStock[0].Idle = 0
	preflight, err := f.service.Preflight(context.Background(), f.tenantID, f.project.PublicID.String(), f.input())
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Eligible || checkStatus(preflight.Checks, "source_archive") != CheckFail || checkStatus(preflight.Checks, "gpu_capacity") != CheckFail {
		t.Fatalf("Preflight() = %+v", preflight)
	}
	submit := SubmitInput{PreflightInput: f.input(), IdempotencyKey: "diagnostic-test-0002", Confirmed: true}
	if _, err := f.service.Submit(context.Background(), f.tenantID, f.actorID, f.project.PublicID.String(), submit); err != ErrPreflightFailed {
		t.Fatalf("failed-preflight Submit() error = %v", err)
	}
	if count, _ := f.client.Experiment.Query().Count(context.Background()); count != 0 {
		t.Fatalf("failed preflight created %d Experiments", count)
	}
}

func TestSubmissionRejectsConfigurationDriftAfterOwnerConfirmation(t *testing.T) {
	f := newDiagnosticFixture(t)
	ctx := context.Background()
	preflight, err := f.service.Preflight(ctx, f.tenantID, f.project.PublicID.String(), f.input())
	if err != nil || !preflight.Eligible || preflight.ConfirmationDigest == "" {
		t.Fatalf("Preflight() = %+v, %v", preflight, err)
	}
	if _, err := f.profile.Update().SetCPUTo(f.profile.CPUTo + 1).Save(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.Submit(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), SubmitInput{
		PreflightInput: f.input(), IdempotencyKey: "diagnostic-drift-0001",
		ConfirmationDigest: preflight.ConfirmationDigest, Confirmed: true,
	})
	if err != ErrProposalChanged {
		t.Fatalf("drifted Submit() error = %v", err)
	}
	if count, _ := f.client.Experiment.Query().Count(ctx); count != 0 {
		t.Fatalf("drifted submission created %d Experiments", count)
	}
}

func TestCompletedDiagnosticRegistersManagedReportArtifact(t *testing.T) {
	f := newDiagnosticFixture(t)
	ctx := context.Background()
	preflight, err := f.service.Preflight(ctx, f.tenantID, f.project.PublicID.String(), f.input())
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.service.Submit(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), SubmitInput{
		PreflightInput: f.input(), IdempotencyKey: "diagnostic-artifact-0001",
		ConfirmationDigest: preflight.ConfirmationDigest, Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	experimentRecord, err := f.client.Experiment.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := experimentRecord.Update().SetExitCode(0).SetMetrics(map[string]any{"diagnostic_passed": true}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	artifacts, err := f.service.experiments.Artifacts(ctx, agentauth.Principal{
		TenantID: f.tenantID, ProjectID: f.project.ID, Scopes: []string{"read"},
	}, created.Run.Experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range artifacts.Artifacts {
		if name == "diagnostic-report.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Artifacts() = %+v", artifacts)
	}
}

func TestSelfHostedDiagnosticUsesAuthorizedOnlineNodeAndZeroReservation(t *testing.T) {
	f := newDiagnosticFixture(t)
	ctx := context.Background()
	environmentRecord, err := f.client.Environment.Create().SetProjectID(f.project.ID).SetName("node-runtime").SetBackend("self_hosted").
		SetImageUUID("registry.example/runtime@sha256:" + strings.Repeat("b", 64)).SetStatus("approved").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profileRecord, err := f.client.ResourceProfile.Create().SetProjectID(f.project.ID).SetName("node-3090").SetBackend("self_hosted").
		SetRegion("self_hosted").SetGpuNames([]string{"NVIDIA GeForce RTX 3090"}).SetGpuNum(1).
		SetCudaFrom(1).SetCudaTo(1).SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).
		SetPriceFromMilli(0).SetPriceToMilli(0).SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	node, err := f.client.SelfHostedNode.Create().SetTenantID(f.tenantID).SetLabel("node-a").SetTokenPrefix("gnc_test").
		SetTokenHash([]byte("node-token-hash")).SetStatus("active").SetObservedState("online").SetInstallationID("installation-a").
		SetMachineFingerprint("machine-a").SetHostname("gpu-node").SetOperatingSystem("linux").SetArchitecture("amd64").
		SetAgentVersion("test").SetProtocolVersion("v1").SetLastSeenAt(f.now).
		SetCapabilities(map[string]any{"gpus": []any{map[string]any{"uuid": "GPU-11111111-1111-1111-1111-111111111111", "name": "NVIDIA GeForce RTX 3090"}}}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.NodeProjectAccess.Create().SetTenantID(f.tenantID).SetNodeID(node.ID).SetProjectID(f.project.ID).SetStatus("active").Save(ctx); err != nil {
		t.Fatal(err)
	}
	input := PreflightInput{
		Backend: BackendSelfHosted, Suite: SuiteGPUConnectivity, RepositoryID: f.repository.PublicID.String(),
		EnvironmentID: environmentRecord.PublicID.String(), ResourceProfileID: profileRecord.PublicID.String(), CommitSHA: strings.Repeat("c", 40),
	}
	preflight, err := f.service.Preflight(ctx, f.tenantID, f.project.PublicID.String(), input)
	if err != nil || !preflight.Eligible || preflight.Proposal.ReservedCostMilli != 0 || checkStatus(preflight.Checks, "node") != CheckPass {
		t.Fatalf("Self-hosted Preflight() = %+v, %v", preflight, err)
	}
	created, err := f.service.Submit(ctx, f.tenantID, f.actorID, f.project.PublicID.String(), SubmitInput{
		PreflightInput: input, IdempotencyKey: "diagnostic-node-0001", ConfirmationDigest: preflight.ConfirmationDigest, Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Run.Experiment.ReservedCostMilli != 0 || !strings.HasPrefix(created.Run.Experiment.OutputPath, "managed://experiments/") {
		t.Fatalf("Self-hosted diagnostic = %+v", created)
	}
	if _, err := profileRecord.Update().SetGpuNum(2).Save(ctx); err != nil {
		t.Fatal(err)
	}
	blocked, err := f.service.Preflight(ctx, f.tenantID, f.project.PublicID.String(), input)
	if err != nil || blocked.Eligible || checkStatus(blocked.Checks, "resource_profile") != CheckFail {
		t.Fatalf("multi-GPU Self-hosted Preflight() = %+v, %v", blocked, err)
	}
}

func TestAssessmentExplainsFrameworkAndCleanupFailures(t *testing.T) {
	stage := "source_extracted"
	code := "command_failed"
	reason := "diagnostic command exited with code 70"
	view := experiment.View{State: "failed", FailureCode: &code, FailureReason: &reason, RunnerStage: &stage, Metrics: map[string]any{"error_type": "ModuleNotFoundError"}}
	assessment := assess(view, []AttemptObservation{{Number: 1}}, false)
	if assessment.Status != "failed" || assessment.Classification != "command_failed" || len(assessment.Recommendations) < 3 || !strings.Contains(assessment.Recommendations[0], "PyTorch") {
		t.Fatalf("assessment = %+v", assessment)
	}
	cleanupPending := assess(experiment.View{State: "succeeded", Metrics: map[string]any{"diagnostic_passed": true}}, []AttemptObservation{{Number: 1}}, false)
	if cleanupPending.Status != "running" || cleanupPending.Classification != "cleanup_pending" {
		t.Fatalf("cleanup-pending assessment = %+v", cleanupPending)
	}
	gpuCount := assess(experiment.View{
		State: "failed", FailureCode: &code, FailureReason: &reason, Metrics: map[string]any{"error_type": "unexpected_gpu_count"},
	}, []AttemptObservation{{Number: 1}}, true)
	if len(gpuCount.Recommendations) == 0 || !strings.Contains(gpuCount.Recommendations[0], "GPU count") {
		t.Fatalf("GPU-count assessment = %+v", gpuCount)
	}
}

func checkStatus(checks []Check, id string) string {
	for _, check := range checks {
		if check.ID == id {
			return check.Status
		}
	}
	return ""
}

func makeArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
