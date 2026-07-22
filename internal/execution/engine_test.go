package execution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	providerservice "github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/runner"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/selfhosted"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type fakeLifecycle struct {
	createCalls int
	findCalls   int
	stopCalls   int
	deleteCalls int
	createErr   error
	createID    string
	find        Observation
	observe     Observation
	observeErr  error
}

func (f *fakeLifecycle) Create(context.Context, *ent.ProviderAccount, DeploymentSpec) (string, map[string]string, error) {
	f.createCalls++
	if f.createErr != nil {
		return "", map[string]string{"create": "req-create"}, f.createErr
	}
	if f.createID == "" {
		f.createID = "deployment-1"
	}
	return f.createID, map[string]string{"create": "req-create"}, nil
}

func (f *fakeLifecycle) Observe(context.Context, *ent.ProviderAccount, string) (Observation, error) {
	return f.observe, f.observeErr
}

func (f *fakeLifecycle) FindByName(context.Context, *ent.ProviderAccount, string) (Observation, error) {
	f.findCalls++
	return f.find, nil
}

func (f *fakeLifecycle) Stop(context.Context, *ent.ProviderAccount, string) (map[string]string, error) {
	f.stopCalls++
	return map[string]string{"stop": "req-stop"}, nil
}

func (f *fakeLifecycle) Delete(context.Context, *ent.ProviderAccount, string) (map[string]string, error) {
	f.deleteCalls++
	return map[string]string{"delete": "req-delete"}, nil
}

type executionFixture struct {
	client      *ent.Client
	box         *secrets.Box
	engine      *Engine
	provider    *fakeLifecycle
	tenant      *ent.Tenant
	project     *ent.Project
	repository  *ent.Repository
	environment *ent.Environment
	profile     *ent.ResourceProfile
	agentToken  *ent.AgentToken
	now         time.Time
}

func newExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := client.Tenant.Create().SetName("test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(10000).SetMaxConcurrency(1).
		SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(5).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("repository").
		SetSSHURL("git@github.com:owner/repository.git").SetSSHHost("github.com").SetDefaultBranch("main").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image-1").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("private").
		SetGpuNames([]string{"RTX 3090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(118).
		SetCPUFrom(1).SetCPUTo(16).SetMemoryFromGB(1).SetMemoryToGB(64).
		SetPriceFromMilli(100).SetPriceToMilli(1000).SetIsDefault(true).Save(ctx)
	agentToken, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("agent").SetPrefix("gmc_test").SetTokenHash([]byte("hash")).Save(ctx)
	ciphertext, err := providerservice.EncryptCredential(box, "provider-token-that-is-long-enough-for-tests")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ProviderAccount.Create().SetTenantID(tenant.ID).SetName("private").SetBaseURL(autodl.PrivateBaseURL).
		SetBackend("private").SetStatus("active").SetCredentialCiphertext(ciphertext).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeLifecycle{}
	config := DefaultConfig()
	config.Enabled = true
	config.PublicURL = "https://gemcp.example.com"
	config.InstanceID = "scheduler-test"
	config.GlobalConcurrency = 1
	config.LeaseDuration = 30 * time.Second
	config.ProvisionTimeout = time.Minute
	config.ReconcileDelay = 2 * time.Second
	config.CallbackGrace = time.Second
	config.RunnerTokenExtraTTL = time.Hour
	engine, err := NewEngine(client, box, provider, config)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &executionFixture{
		client: client, box: box, engine: engine, provider: provider, tenant: tenant, project: project,
		repository: repository, environment: environment, profile: profile, agentToken: agentToken,
		now: time.Now().UTC().Truncate(time.Second),
	}
	engine.now = func() time.Time { return fixture.now }
	return fixture
}

func (f *executionFixture) addExperiment(t *testing.T) *ent.Experiment {
	return f.addExperimentWithSecrets(t, nil)
}

func (f *executionFixture) addExperimentWithSecrets(t *testing.T, secretNames []string) *ent.Experiment {
	return f.addExperimentWithReservation(t, secretNames, 1000)
}

func (f *executionFixture) addExperimentWithReservation(t *testing.T, secretNames []string, reservation int64) *ent.Experiment {
	t.Helper()
	ctx := context.Background()
	publicID := uuid.New()
	outputPath := "/root/autodl-fs/projects/" + f.project.PublicID.String() + "/experiments/" + publicID.String() + "/"
	record, err := f.client.Experiment.Create().SetPublicID(publicID).SetTenantID(f.tenant.ID).SetProjectID(f.project.ID).
		SetAgentTokenID(f.agentToken.ID).SetRepositoryID(f.repository.ID).SetEnvironmentID(f.environment.ID).SetResourceProfileID(f.profile.ID).
		SetCommitSha("0123456789012345678901234567890123456789").SetCommand("echo trained").
		SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(5).
		SetRepositorySnapshot(map[string]any{"id": f.repository.PublicID.String(), "project_id": f.project.PublicID.String()}).
		SetEnvironmentSnapshot(map[string]any{"id": f.environment.PublicID.String(), "image_uuid": "image-1"}).
		SetResourceSnapshot(map[string]any{
			"id": f.profile.PublicID.String(), "region": "private", "gpu_names": []string{"RTX 3090"}, "gpu_num": 1,
			"cuda_from": 118, "cuda_to": 118, "cpu_from": 1, "cpu_to": 16,
			"memory_from_gb": 1, "memory_to_gb": 64, "price_from_milli": 100, "price_to_milli": 1000,
			"reuse_container": true,
		}).SetSecretNames(secretNames).SetOutputPath(outputPath).SetReservedCostMilli(reservation).SetNextAttemptAt(f.now).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.BudgetEntry.Create().SetTenantID(f.tenant.ID).SetProjectID(f.project.ID).SetExperimentID(record.ID).
		SetPeriod("2026-07").SetKind("reservation").SetAmountMilli(reservation).SetDescription("test reservation").Save(ctx); err != nil {
		t.Fatal(err)
	}
	return record
}

func (f *executionFixture) attemptAndToken(t *testing.T, experimentID int) (*ent.Attempt, string) {
	t.Helper()
	record, err := f.client.Attempt.Query().Where(attempt.ExperimentIDEQ(experimentID)).Order(ent.Desc(attempt.FieldID)).First(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := f.box.Decrypt(record.RunnerTokenCiphertext, runner.TokenAADPrefix+record.PublicID.String())
	if err != nil {
		t.Fatal(err)
	}
	return record, string(plaintext)
}

func TestDisabledDispatchStillRunsReconcilerHeartbeat(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	f.engine.config.Enabled = false
	if err := f.engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	experimentRecord, _ = f.client.Experiment.Get(context.Background(), experimentRecord.ID)
	if experimentRecord.State != "queued" || f.provider.createCalls != 0 {
		t.Fatalf("experiment=%+v create calls=%d", experimentRecord, f.provider.createCalls)
	}
	if count, _ := f.client.ServiceHeartbeat.Query().Count(context.Background()); count != 1 {
		t.Fatalf("scheduler heartbeats = %d", count)
	}
}

func TestEngineDispatchesSelfHostedProfileWithoutAutoDL(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	environment, err := f.client.Environment.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").SetName("node-env").
		SetImageUUID("registry.example/train@sha256:" + strings.Repeat("a", 64)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := f.client.ResourceProfile.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").SetName("node-profile").
		SetRegion("self_hosted").SetGpuNames([]string{"RTX 3090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).
		SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	node, err := f.client.SelfHostedNode.Create().SetTenantID(f.tenant.ID).SetLabel("node").SetTokenPrefix("gmn_node").SetTokenHash([]byte("node-hash")).
		SetStatus("active").SetObservedState("online").SetInstallationID("installation").SetMachineFingerprint(strings.Repeat("b", 64)).
		SetHostname("node").SetOperatingSystem("linux").SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion("1").
		SetCapabilities(map[string]any{"gpus": []any{map[string]any{"uuid": "GPU-test", "name": "RTX 3090"}}}).SetLastSeenAt(f.now).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.NodeProjectAccess.Create().SetTenantID(f.tenant.ID).SetNodeID(node.ID).SetProjectID(f.project.ID).Save(ctx); err != nil {
		t.Fatal(err)
	}
	config := selfhosted.DefaultConfig()
	config.Enabled = true
	config.InstanceID = "scheduler-test"
	service, err := selfhosted.NewService(f.client, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	f.engine.selfHosted = service
	record, err := f.client.Experiment.Create().SetTenantID(f.tenant.ID).SetProjectID(f.project.ID).SetAgentTokenID(f.agentToken.ID).
		SetRepositoryID(f.repository.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha(strings.Repeat("0", 40)).
		SetCommand("echo trained").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(5).
		SetRepositorySnapshot(map[string]any{"project_id": f.project.PublicID.String()}).
		SetEnvironmentSnapshot(map[string]any{"backend": "self_hosted", "image_uuid": environment.ImageUUID}).
		SetResourceSnapshot(map[string]any{"backend": "self_hosted"}).SetOutputPath("managed://experiments/test/outputs").
		SetReservedCostMilli(0).SetNextAttemptAt(f.now).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.BudgetEntry.Create().SetTenantID(f.tenant.ID).SetProjectID(f.project.ID).SetExperimentID(record.ID).
		SetPeriod("2026-07").SetKind("reservation").SetAmountMilli(0).SetDescription("unmetered").Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	record, _ = f.client.Experiment.Get(ctx, record.ID)
	if record.State != "provisioning" || f.provider.createCalls != 0 {
		t.Fatalf("experiment=%+v AutoDL create calls=%d", record, f.provider.createCalls)
	}
	if assignments, _ := f.client.NodeAssignment.Query().Count(ctx); assignments != 1 {
		t.Fatalf("Node Assignments=%d", assignments)
	}
	if commands, _ := f.client.NodeCommand.Query().Count(ctx); commands != 1 {
		t.Fatalf("Node Commands=%d", commands)
	}
}

func TestDisabledDispatchDoesNotIssuePendingCreate(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	dispatched, err := f.engine.dispatchOne(context.Background(), experimentRecord.ID, f.now)
	if err != nil || !dispatched {
		t.Fatalf("dispatchOne() = %t, %v", dispatched, err)
	}
	f.engine.config.Enabled = false
	f.now = f.now.Add(31 * time.Second)
	if err := f.engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.provider.createCalls != 0 {
		t.Fatalf("disabled dispatch issued %d Provider create calls", f.provider.createCalls)
	}
	resourceRecord, _ := f.client.ProviderResource.Query().Where(providerresource.ExperimentIDEQ(experimentRecord.ID)).Only(context.Background())
	if resourceRecord.CreateAttempts != 0 || resourceRecord.State != providerresource.StateCreating {
		t.Fatalf("resource = %+v", resourceRecord)
	}
}

func TestDispatchRejectsHistoricalQueuedSecretRequests(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperimentWithSecrets(t, []string{"LEGACY_TOKEN"})
	if err := f.engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	experimentRecord, _ = f.client.Experiment.Get(context.Background(), experimentRecord.ID)
	if experimentRecord.State != "provider_error" || experimentRecord.FailureCode == nil || *experimentRecord.FailureCode != "secret_injection_unavailable" || f.provider.createCalls != 0 {
		t.Fatalf("experiment=%+v create calls=%d", experimentRecord, f.provider.createCalls)
	}
	entries, _ := f.client.BudgetEntry.Query().Where(budgetentry.ExperimentIDEQ(experimentRecord.ID)).All(context.Background())
	releases, _ := f.client.BudgetEntry.Query().Where(budgetentry.ExperimentIDEQ(experimentRecord.ID), budgetentry.KindEQ("release")).Count(context.Background())
	if len(entries) != 2 || releases != 1 {
		t.Fatalf("budget entries = %+v", entries)
	}
}

func TestDispatchRejectsHistoricalUnderReservedExperiment(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperimentWithReservation(t, nil, 100)
	if err := f.engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	experimentRecord, _ = f.client.Experiment.Get(context.Background(), experimentRecord.ID)
	if experimentRecord.State != "provider_error" || experimentRecord.FailureCode == nil || *experimentRecord.FailureCode != "reservation_policy_outdated" || f.provider.createCalls != 0 {
		t.Fatalf("experiment=%+v create calls=%d", experimentRecord, f.provider.createCalls)
	}
}

func TestEngineRunsCallbackLifecycleAndFinalizesBudget(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	ctx := context.Background()
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("dispatch tick: %v", err)
	}
	if f.provider.createCalls != 1 {
		t.Fatalf("create calls = %d", f.provider.createCalls)
	}
	attemptRecord, token := f.attemptAndToken(t, experimentRecord.ID)
	runnerService := runner.NewService(f.client, f.box, nil, runner.WithClock(func() time.Time { return f.now }))
	if _, err := runnerService.Event(ctx, token, runner.EventInput{Type: "started"}); err != nil {
		t.Fatalf("started callback: %v", err)
	}
	started := f.now
	f.provider.observe = Observation{
		Found: true, Deployment: autodl.Deployment{UUID: "deployment-1", Status: "running", RunningNum: 1},
		Containers: []autodl.Container{{UUID: "container-1", PriceMilli: 1000, StartedAt: &started}},
	}
	f.now = f.now.Add(10 * time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("running observation tick: %v", err)
	}
	exitCode := 0
	if _, err := runnerService.Event(ctx, token, runner.EventInput{Type: "finished", ExitCode: &exitCode, Reason: "completed", LogTail: "done\n", Metrics: map[string]any{"accuracy": 0.9}}); err != nil {
		t.Fatalf("finished callback: %v", err)
	}
	f.now = f.now.Add(time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("cleanup request tick: %v", err)
	}
	pending, _ := f.client.Experiment.Get(ctx, experimentRecord.ID)
	resourceRecord, _ := f.client.ProviderResource.Query().Where(providerresource.ExperimentIDEQ(experimentRecord.ID)).Only(ctx)
	if pending.State != "collecting" || resourceRecord.State != providerresource.StateDeleting || resourceRecord.DeletedAt != nil {
		t.Fatalf("cleanup finalized before deletion was confirmed: experiment=%+v resource=%+v", pending, resourceRecord)
	}
	f.provider.observe = Observation{}
	f.now = f.now.Add(time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("cleanup confirmation tick: %v", err)
	}
	final, _ := f.client.Experiment.Get(ctx, experimentRecord.ID)
	if final.State != "succeeded" || final.BudgetFinalizedAt == nil || final.EstimatedCostMilli != 4 {
		t.Fatalf("final experiment = %+v", final)
	}
	attemptRecord, _ = f.client.Attempt.Get(ctx, attemptRecord.ID)
	if attemptRecord.State != "succeeded" || len(attemptRecord.RunnerTokenHash) != 0 || attemptRecord.RunnerTokenCiphertext != "" || attemptRecord.ExitCode == nil || *attemptRecord.ExitCode != 0 {
		t.Fatalf("final attempt = %+v", attemptRecord)
	}
	if f.provider.stopCalls != 1 || f.provider.deleteCalls != 1 {
		t.Fatalf("cleanup calls stop=%d delete=%d", f.provider.stopCalls, f.provider.deleteCalls)
	}
	entries, _ := f.client.BudgetEntry.Query().Where(budgetentry.ExperimentIDEQ(experimentRecord.ID)).All(ctx)
	if len(entries) != 3 {
		t.Fatalf("budget entries = %d, want reservation/release/charge", len(entries))
	}
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("idempotent terminal tick: %v", err)
	}
	entries, _ = f.client.BudgetEntry.Query().Where(budgetentry.ExperimentIDEQ(experimentRecord.ID)).All(ctx)
	if len(entries) != 3 {
		t.Fatalf("duplicate budget finalization: %d entries", len(entries))
	}
}

func TestAmbiguousCreateCreatesANewAttemptOnlyAfterReconciliation(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	f.provider.createErr = errors.New("connection reset after request")
	ctx := context.Background()
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if f.provider.createCalls != 1 {
		t.Fatalf("create calls = %d", f.provider.createCalls)
	}
	_, oldToken := f.attemptAndToken(t, experimentRecord.ID)
	f.now = f.now.Add(3 * time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("reconciliation tick: %v", err)
	}
	record, _ := f.client.Experiment.Get(ctx, experimentRecord.ID)
	if record.State != "queued" || f.provider.findCalls != 1 {
		t.Fatalf("after reconciliation state=%s find=%d", record.State, f.provider.findCalls)
	}
	attempts, _ := f.client.Attempt.Query().Where(attempt.ExperimentIDEQ(experimentRecord.ID)).All(ctx)
	if len(attempts) != 1 || attempts[0].State != "failed" || len(attempts[0].RunnerTokenHash) != 0 || attempts[0].RunnerTokenCiphertext != "" {
		t.Fatalf("attempts after first create = %+v", attempts)
	}
	oldRunner := runner.NewService(f.client, f.box, nil, runner.WithClock(func() time.Time { return f.now }))
	if _, err := oldRunner.Spec(ctx, oldToken); err != runner.ErrUnauthenticated {
		t.Fatalf("retired Runner token error = %v", err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("retry dispatch tick: %v", err)
	}
	attempts, _ = f.client.Attempt.Query().Where(attempt.ExperimentIDEQ(experimentRecord.ID)).All(ctx)
	if len(attempts) != 2 || f.provider.createCalls != 2 {
		t.Fatalf("attempts=%d create calls=%d", len(attempts), f.provider.createCalls)
	}
}

func TestTimeoutWaitsForRunnerGraceBeforeProviderCleanup(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	ctx := context.Background()
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	_, token := f.attemptAndToken(t, experimentRecord.ID)
	runnerService := runner.NewService(f.client, f.box, nil, runner.WithClock(func() time.Time { return f.now }))
	if _, err := runnerService.Event(ctx, token, runner.EventInput{Type: "started"}); err != nil {
		t.Fatal(err)
	}
	f.provider.observe = Observation{Found: true, Deployment: autodl.Deployment{UUID: "deployment-1", Status: "running", RunningNum: 1}}
	experimentRecord, _ = f.client.Experiment.Get(ctx, experimentRecord.ID)
	f.now = *experimentRecord.DeadlineAt
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	experimentRecord, _ = f.client.Experiment.Get(ctx, experimentRecord.ID)
	if experimentRecord.TimeoutExtendedAt == nil {
		t.Fatal("initial timeout did not grant the configured extension")
	}
	f.now = *experimentRecord.DeadlineAt
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	resourceRecord, _ := f.client.ProviderResource.Query().Where(providerresource.ExperimentIDEQ(experimentRecord.ID)).Only(ctx)
	if resourceRecord.StopReason == nil || *resourceRecord.StopReason != "timeout" || resourceRecord.HardDeadlineAt == nil || f.provider.stopCalls != 0 {
		t.Fatalf("resource=%+v stop calls=%d", resourceRecord, f.provider.stopCalls)
	}
	f.now = *resourceRecord.HardDeadlineAt
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.provider.stopCalls != 1 || f.provider.deleteCalls != 1 {
		t.Fatalf("cleanup calls stop=%d delete=%d", f.provider.stopCalls, f.provider.deleteCalls)
	}
}

func TestRunningWithFinishedCountRequiresCallbackThenFailsSafely(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	ctx := context.Background()
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	f.provider.observe = Observation{Found: true, Deployment: autodl.Deployment{
		UUID: "deployment-1", Status: "running", RunningNum: 0, FinishedNum: 1,
	}}
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("terminal observation: %v", err)
	}
	record, _ := f.client.Experiment.Get(ctx, experimentRecord.ID)
	if record.State != "provisioning" {
		t.Fatalf("state before callback grace = %s", record.State)
	}
	f.now = f.now.Add(2 * time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("callback grace tick: %v", err)
	}
	f.provider.observe = Observation{}
	f.now = f.now.Add(time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatalf("deletion confirmation tick: %v", err)
	}
	record, _ = f.client.Experiment.Get(ctx, experimentRecord.ID)
	if record.State != "provider_error" || record.FailureCode == nil || *record.FailureCode != "runner_callback_missing" {
		t.Fatalf("terminal experiment = %+v", record)
	}
	if f.provider.stopCalls != 1 || f.provider.deleteCalls != 1 {
		t.Fatalf("terminal cleanup stop=%d delete=%d", f.provider.stopCalls, f.provider.deleteCalls)
	}
}

func TestProviderOOMIsAUserFailureAndIsNotRetried(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	ctx := context.Background()
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	f.provider.observe = Observation{
		Found:      true,
		Deployment: autodl.Deployment{UUID: "deployment-1", Status: "failed", FailedNum: 1},
		Events:     []autodl.ContainerEvent{{ContainerUUID: "container-1", Status: "OOMKilled", CreatedAt: f.now}},
	}
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(2 * time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	f.provider.observe = Observation{}
	f.now = f.now.Add(time.Second)
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	record, _ := f.client.Experiment.Get(ctx, experimentRecord.ID)
	if record.State != "failed" || record.FailureCode == nil || *record.FailureCode != "oom" {
		t.Fatalf("experiment = %+v", record)
	}
	if count, _ := f.client.Attempt.Query().Where(attempt.ExperimentIDEQ(experimentRecord.ID)).Count(ctx); count != 1 {
		t.Fatalf("OOM created %d attempts", count)
	}
}

func TestRunnerTokenTTLIncludesBoundedProvisioningOverhead(t *testing.T) {
	record := &ent.Experiment{MaxRuntimeSeconds: int((30 * 24 * time.Hour) / time.Second)}
	config := DefaultConfig()
	if ttl, err := runnerTokenTTL(record, config); err != nil || ttl <= 31*24*time.Hour || ttl > 32*24*time.Hour {
		t.Fatalf("runnerTokenTTL = %s, %v", ttl, err)
	}
	record.MaxRuntimeSeconds = 60
	record.TimeoutExtensionSeconds = 30
	if ttl, err := runnerTokenTTL(record, config); err != nil || ttl <= 24*time.Hour {
		t.Fatalf("runnerTokenTTL = %s, %v", ttl, err)
	}
}

func TestPermanentCreateErrorKeepsCapacityRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		permanent bool
	}{
		{name: "permission", err: &autodl.ProviderError{HTTPStatus: 403, Code: "FORBIDDEN"}, permanent: true},
		{name: "invalid image", err: &autodl.ProviderError{HTTPStatus: 400, Code: "INVALID_IMAGE"}, permanent: true},
		{name: "capacity", err: &autodl.ProviderError{HTTPStatus: 400, Code: "NO_STOCK", Message: "no GPU stock available"}},
		{name: "rate limit", err: &autodl.ProviderError{HTTPStatus: 429, Code: "RATE_LIMIT"}},
		{name: "transport", err: errors.New("connection reset")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := permanentCreateError(test.err); got != test.permanent {
				t.Fatalf("permanentCreateError() = %t, want %t", got, test.permanent)
			}
		})
	}
}

func TestTerminalResultClassifiesOwnerStopAndRunnerFailure(t *testing.T) {
	exitCode := 143
	ownerStop := "owner_stop"
	state, attemptState, code, _ := terminalResult(&ent.Experiment{}, &ent.Attempt{ExitCode: &exitCode}, &ent.ProviderResource{StopReason: &ownerStop})
	if state != "cancelled" || attemptState != "cancelled" || code != "cancelled" {
		t.Fatalf("owner stop result = %s %s %s", state, attemptState, code)
	}
	runnerFailure := "runner_error"
	state, attemptState, code, _ = terminalResult(&ent.Experiment{}, &ent.Attempt{ExitCode: &exitCode}, &ent.ProviderResource{StopReason: &runnerFailure})
	if state != "failed" || attemptState != "failed" || code != "runner_error" {
		t.Fatalf("Runner failure result = %s %s %s", state, attemptState, code)
	}
	bootstrapFailure := "runner_bootstrap_failed"
	failureReason := "Runner bootstrap failed during source extract (ReadError)"
	state, attemptState, code, reason := terminalResult(&ent.Experiment{FailureReason: &failureReason}, &ent.Attempt{}, &ent.ProviderResource{StopReason: &bootstrapFailure})
	if state != "provider_error" || attemptState != "failed" || code != "runner_bootstrap_failed" || reason != failureReason {
		t.Fatalf("Runner bootstrap result = %s %s %s %q", state, attemptState, code, reason)
	}
}

func TestDispatchRollsQueuedReservationIntoCurrentProjectPeriod(t *testing.T) {
	f := newExecutionFixture(t)
	f.now = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	experimentRecord := f.addExperiment(t)
	if err := f.engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := f.client.BudgetEntry.Query().Where(budgetentry.ExperimentIDEQ(experimentRecord.ID)).Order(ent.Asc(budgetentry.FieldID)).All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Period != "2026-07" || entries[1].Kind != "release" || entries[1].Period != "2026-07" || entries[2].Kind != "reservation" || entries[2].Period != "2026-08" {
		t.Fatalf("rollover entries = %+v", entries)
	}
}

func TestDispatchHonorsGlobalAndProjectConcurrency(t *testing.T) {
	f := newExecutionFixture(t)
	first := f.addExperiment(t)
	second := f.addExperiment(t)
	if err := f.engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstRecord, _ := f.client.Experiment.Get(context.Background(), first.ID)
	secondRecord, _ := f.client.Experiment.Get(context.Background(), second.ID)
	states := []string{firstRecord.State, secondRecord.State}
	if !((states[0] == "provisioning" && states[1] == "queued") || (states[1] == "provisioning" && states[0] == "queued")) {
		t.Fatalf("states = %v", states)
	}
	if f.provider.createCalls != 1 {
		t.Fatalf("create calls = %d", f.provider.createCalls)
	}
}
