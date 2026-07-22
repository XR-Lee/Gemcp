package experiment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

type fakeCommitVerifier struct {
	calls int
	err   error
	sha   string
}

func (f *fakeCommitVerifier) VerifyCommit(_ context.Context, _ int, sha string) error {
	f.calls++
	f.sha = sha
	return f.err
}

type fixture struct {
	client      *ent.Client
	service     *Service
	principal   agentauth.Principal
	project     *ent.Project
	repository  *ent.Repository
	environment *ent.Environment
	profile     *ent.ResourceProfile
	verifier    *fakeCommitVerifier
}

func newFixture(t *testing.T, monthlyBudget, experimentCap int64) fixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Project").SetSlug("project").
		SetMonthlyBudgetMilli(monthlyBudget).SetMaxExperimentMilli(experimentCap).
		SetMaxRuntimeSeconds(3600).SetTimeoutExtensionSeconds(3600).SetTerminationGraceSeconds(60).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := client.Repository.Create().
		SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").
		SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := client.Environment.Create().
		SetProjectID(project.ID).SetName("default").SetImageUUID("image-uuid").SetIsDefault(true).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := client.ResourceProfile.Create().
		SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
		SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, raw, err := createAgentToken(ctx, client, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = raw
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	verifier := &fakeCommitVerifier{}
	return fixture{
		client: client, service: NewService(client, box, verifier), project: project, repository: repository,
		environment: environment, profile: profile, verifier: verifier,
		principal: agentauth.Principal{
			TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), ProjectID: project.ID,
			ProjectPublicID: project.PublicID.String(), TokenID: token.ID, TokenPublicID: token.PublicID.String(),
			TokenLabel: token.Label, Scopes: []string{"submit", "read", "cancel"},
		},
	}
}

func createAgentToken(ctx context.Context, client *ent.Client, projectID int) (*ent.AgentToken, string, error) {
	raw, prefix, err := secrets.RandomToken("gmc", 32)
	if err != nil {
		return nil, "", err
	}
	record, err := client.AgentToken.Create().
		SetProjectID(projectID).SetLabel("test").SetPrefix(prefix).SetTokenHash([]byte(raw)).Save(ctx)
	return record, raw, err
}

func validSubmit(f fixture, key string) SubmitInput {
	return SubmitInput{
		RepositoryID: f.repository.PublicID.String(), CommitSHA: "0123456789012345678901234567890123456789",
		Command: "python train.py", MaxRuntimeSeconds: 3600, IdempotencyKey: key,
	}
}

func TestSubmitIsAtomicAndIdempotent(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	first, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0001"))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if first.Idempotent || first.Experiment.State != "queued" || first.Experiment.ReservedCostMilli != 6575 {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if first.Experiment.ProjectID != f.project.PublicID.String() || first.Experiment.EnvironmentID != f.environment.PublicID.String() {
		t.Fatalf("missing snapshot IDs: %+v", first.Experiment)
	}
	second, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0001"))
	if err != nil {
		t.Fatalf("idempotent Submit() error = %v", err)
	}
	if !second.Idempotent || second.Experiment.ID != first.Experiment.ID || f.verifier.calls != 1 {
		t.Fatalf("unexpected idempotent result: %+v, verifier calls=%d", second, f.verifier.calls)
	}
	if count, _ := f.client.Experiment.Query().Count(ctx); count != 1 {
		t.Fatalf("experiment count = %d", count)
	}
	if count, _ := f.client.BudgetEntry.Query().Count(ctx); count != 1 {
		t.Fatalf("budget entry count = %d", count)
	}

	conflict := validSubmit(f, "request-0001")
	conflict.Command = "python other.py"
	if _, err := f.service.Submit(ctx, f.principal, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting Submit() error = %v", err)
	}
}

func TestSubmitEnforcesHardBudget(t *testing.T) {
	f := newFixture(t, 7000, 7000)
	ctx := context.Background()
	if _, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0001")); err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	if _, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0002")); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("second Submit() error = %v", err)
	}
	if count, _ := f.client.Experiment.Query().Count(ctx); count != 1 {
		t.Fatalf("experiment count = %d", count)
	}
}

func TestSubmitSelfHostedIsUnmeteredAndUsesManagedOutput(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	environment, err := f.client.Environment.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").
		SetName("self-hosted").SetImageUUID("registry.example/train@sha256:" + strings.Repeat("a", 64)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := f.client.ResourceProfile.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").
		SetName("self-hosted").SetRegion("self_hosted").SetGpuNames([]string{"RTX 3090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(16).SetMemoryFromGB(1).SetMemoryToGB(64).
		SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input := validSubmit(f, "request-self-hosted")
	input.EnvironmentID = environment.PublicID.String()
	input.ResourceProfileID = profile.PublicID.String()
	result, err := f.service.Submit(ctx, f.principal, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Experiment.ReservedCostMilli != 0 || !strings.HasPrefix(result.Experiment.OutputPath, "managed://experiments/") {
		t.Fatalf("Self-hosted experiment = %+v", result.Experiment)
	}
	record, _ := f.client.Experiment.Query().Only(ctx)
	if record.EnvironmentSnapshot["backend"] != "self_hosted" || record.ResourceSnapshot["backend"] != "self_hosted" {
		t.Fatalf("snapshots environment=%v resource=%v", record.EnvironmentSnapshot, record.ResourceSnapshot)
	}
	entry, _ := f.client.BudgetEntry.Query().Only(ctx)
	if entry.AmountMilli != 0 {
		t.Fatalf("Self-hosted reservation = %d", entry.AmountMilli)
	}
}

func TestSubmitRejectsUnimplementedSecretInjection(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	input := validSubmit(f, "request-secret")
	input.SecretNames = []string{"GITHUB_TOKEN"}
	if _, err := f.service.Submit(context.Background(), f.principal, input); err == nil {
		t.Fatal("Submit() accepted secret_names without an injection implementation")
	}
	if f.verifier.calls != 0 {
		t.Fatalf("commit verifier called %d times", f.verifier.calls)
	}
}

func TestOwnerAttemptsExposeResultsWithoutRunnerCredentials(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	submitted, err := f.service.Submit(context.Background(), f.principal, validSubmit(f, "request-attempts"))
	if err != nil {
		t.Fatal(err)
	}
	experimentRecord, err := f.client.Experiment.Query().Only(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	_, err = f.client.Attempt.Create().SetTenantID(f.principal.TenantID).SetProjectID(f.principal.ProjectID).
		SetExperimentID(experimentRecord.ID).SetNumber(1).SetState("succeeded").SetProviderResourceID("deployment-1").
		SetRunnerTokenHash([]byte("must-not-leak")).SetRunnerTokenCiphertext("must-not-leak").SetExitCode(exitCode).
		SetLogTail("done\n").SetMetrics(map[string]any{"accuracy": 0.9}).Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := f.service.OwnerAttempts(context.Background(), f.principal.TenantID, f.project.PublicID.String(), submitted.Experiment.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	encoded := fmt.Sprintf("%+v", attempts[0])
	if attempts[0].ProviderResourceID == nil || *attempts[0].ProviderResourceID != "deployment-1" || attempts[0].LogTail == nil || strings.Contains(encoded, "must-not-leak") {
		t.Fatalf("attempt view = %+v", attempts[0])
	}
}

func TestQueuedCancellationReleasesBudgetOnce(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	submitted, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0001"))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.Cancel(ctx, f.principal, submitted.Experiment.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelled.State != "cancelled" || cancelled.FinishedAt == nil {
		t.Fatalf("unexpected cancelled experiment: %+v", cancelled)
	}
	if _, err := f.service.Cancel(ctx, f.principal, submitted.Experiment.ID); err != nil {
		t.Fatalf("second Cancel() error = %v", err)
	}
	if count, _ := f.client.BudgetEntry.Query().Count(ctx); count != 2 {
		t.Fatalf("budget entry count = %d, want 2", count)
	}
	cost, err := f.service.Cost(ctx, f.principal)
	if err != nil {
		t.Fatal(err)
	}
	if cost.ReservedMilli != 0 || cost.AvailableMilli != f.project.MonthlyBudgetMilli {
		t.Fatalf("unexpected cost: %+v", cost)
	}
}

func TestProjectCreditIncreasesAvailableSchedulingCapacity(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	before, err := f.service.Cost(ctx, f.principal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.BudgetEntry.Create().SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetPeriod(before.Period).SetKind("adjustment").SetAmountMilli(-25_000).
		SetDescription("approved test credit").SetIdempotencyKey("test-credit-001").Save(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.Cost(ctx, f.principal)
	if err != nil {
		t.Fatal(err)
	}
	if after.AdjustmentsMilli != -25_000 || after.CommittedMilli != -25_000 || after.AvailableMilli != 125_000 {
		t.Fatalf("cost after Project credit=%+v", after)
	}
}

func TestRunningCancellationDurablyRequestsOwnedResourceStop(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	submitted, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0001"))
	if err != nil {
		t.Fatal(err)
	}
	experimentRecord, _ := f.client.Experiment.Query().Only(ctx)
	_, _ = experimentRecord.Update().SetState("running").Save(ctx)
	providerAccount, err := f.client.ProviderAccount.Create().SetTenantID(f.principal.TenantID).SetName("provider").
		SetBaseURL("https://private.autodl.com").SetBackend("private").SetStatus("active").SetCredentialCiphertext("cipher").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attemptRecord, err := f.client.Attempt.Create().SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetExperimentID(experimentRecord.ID).SetNumber(1).SetState("running").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resourceRecord, err := f.client.ProviderResource.Create().SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetExperimentID(experimentRecord.ID).SetAttemptID(attemptRecord.ID).SetProviderAccountID(providerAccount.ID).
		SetName("gemcp-attempt").SetProviderID("deployment-1").SetState(providerresource.StateActive).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.Cancel(ctx, f.principal, submitted.Experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	resourceRecord, _ = f.client.ProviderResource.Get(ctx, resourceRecord.ID)
	if cancelled.State != "cancelling" || resourceRecord.StopRequestedAt == nil || resourceRecord.StopReason == nil || *resourceRecord.StopReason != "cancelled" {
		t.Fatalf("cancelled=%+v resource=%+v", cancelled, resourceRecord)
	}
	resourceRecord, _ = resourceRecord.Update().SetStopReason("emergency").Save(ctx)
	if _, err := f.service.Cancel(ctx, f.principal, submitted.Experiment.ID); err != nil {
		t.Fatalf("idempotent cancel: %v", err)
	}
	resourceRecord, _ = f.client.ProviderResource.Get(ctx, resourceRecord.ID)
	if resourceRecord.StopReason == nil || *resourceRecord.StopReason != "emergency" {
		t.Fatalf("repeated cancel downgraded stop reason: %+v", resourceRecord)
	}
}

func TestProjectBoundaryAndCommitFailure(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	other := newFixture(t, 100000, 20000)
	input := validSubmit(f, "request-0001")
	input.RepositoryID = other.repository.PublicID.String()
	if _, err := f.service.Submit(context.Background(), f.principal, input); !errors.Is(err, ErrOptionNotFound) {
		t.Fatalf("cross-project Submit() error = %v", err)
	}
	f.verifier.err = errors.New("not reachable")
	if _, err := f.service.Submit(context.Background(), f.principal, validSubmit(f, "request-0002")); !errors.Is(err, ErrCommitVerification) {
		t.Fatalf("commit failure error = %v", err)
	}
}

func TestReserveCostRoundsWithoutOverflow(t *testing.T) {
	cost, err := reserveCost(1, 1, 1)
	if err != nil || cost != 1 {
		t.Fatalf("reserveCost(1,1,1) = %d, %v", cost, err)
	}
	if _, err := reserveCost(int64(^uint64(0)>>1), 1, int64(^uint64(0)>>1)); err == nil {
		t.Fatal("reserveCost() accepted overflowing values")
	}
}

func TestOwnerQueriesEnforceTenantBoundary(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	submitted, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0001"))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := f.service.OwnerList(ctx, f.principal.TenantID, f.project.PublicID.String(), ListInput{})
	if err != nil || len(listed.Experiments) != 1 {
		t.Fatalf("OwnerList() = %+v, %v", listed, err)
	}
	view, err := f.service.OwnerGet(ctx, f.principal.TenantID, f.project.PublicID.String(), submitted.Experiment.ID)
	if err != nil || view.ID != submitted.Experiment.ID {
		t.Fatalf("OwnerGet() = %+v, %v", view, err)
	}
	otherTenant, _ := f.client.Tenant.Create().SetName("Other").Save(ctx)
	otherProject, _ := f.client.Project.Create().
		SetTenantID(otherTenant.ID).SetName("Other").SetSlug("other").
		SetMonthlyBudgetMilli(1000).SetMaxExperimentMilli(1000).Save(ctx)
	if _, err := f.service.OwnerList(ctx, f.principal.TenantID, otherProject.PublicID.String(), ListInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant OwnerList() error = %v", err)
	}
}

func TestOptionsAndList(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	options, err := f.service.Options(ctx, f.principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Repositories) != 1 || len(options.Environments) != 1 || len(options.ResourceProfiles) != 1 {
		t.Fatalf("unexpected options: %+v", options)
	}
	if _, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-0001")); err != nil {
		t.Fatal(err)
	}
	listed, err := f.service.List(ctx, f.principal, ListInput{States: []string{"queued"}})
	if err != nil || len(listed.Experiments) != 1 {
		t.Fatalf("List() = %+v, %v", listed, err)
	}
}
