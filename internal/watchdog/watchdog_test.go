package watchdog

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/execution"
	_ "github.com/mattn/go-sqlite3"
)

type watchdogProvider struct {
	find         execution.Observation
	observe      execution.Observation
	observeCalls int
	findCalls    int
	stopCalls    int
	deleteCalls  int
	stopErr      error
}

func (p *watchdogProvider) Create(context.Context, *ent.ProviderAccount, execution.DeploymentSpec) (string, map[string]string, error) {
	return "", nil, errors.New("unexpected create")
}
func (p *watchdogProvider) Observe(context.Context, *ent.ProviderAccount, string) (execution.Observation, error) {
	p.observeCalls++
	return p.observe, nil
}
func (p *watchdogProvider) FindByName(context.Context, *ent.ProviderAccount, string) (execution.Observation, error) {
	p.findCalls++
	return p.find, nil
}
func (p *watchdogProvider) Stop(context.Context, *ent.ProviderAccount, string) (map[string]string, error) {
	p.stopCalls++
	return map[string]string{"stop": "req-stop"}, p.stopErr
}
func (p *watchdogProvider) Delete(context.Context, *ent.ProviderAccount, string) (map[string]string, error) {
	p.deleteCalls++
	return map[string]string{"delete": "req-delete"}, nil
}

type watchdogFixture struct {
	client     *ent.Client
	service    *Service
	provider   *watchdogProvider
	experiment *ent.Experiment
	resource   *ent.ProviderResource
	now        time.Time
}

func newWatchdogFixture(t *testing.T, providerID *string, owned bool, hardDeadline time.Time) *watchdogFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, fmt.Sprintf("file:%s-%d-%t?mode=memory&cache=shared&_fk=1", t.Name(), hardDeadline.UnixNano(), owned))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("tenant").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("project").SetSlug("project").SetMonthlyBudgetMilli(1000).SetMaxExperimentMilli(1000).Save(ctx)
	repositoryRecord, _ := client.Repository.Create().SetProjectID(project.ID).SetName("repository").SetSSHURL("git@github.com:o/r.git").SetSSHHost("github.com").SetDefaultBranch("main").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("env").SetImageUUID("image").Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("profile").SetRegion("private").SetGpuNames([]string{"GPU"}).SetCudaFrom(118).SetCudaTo(118).SetCPUFrom(1).SetCPUTo(2).SetMemoryFromGB(1).SetMemoryToGB(2).SetPriceFromMilli(1).SetPriceToMilli(1).Save(ctx)
	agent, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("agent").SetPrefix("prefix").SetTokenHash([]byte("hash")).Save(ctx)
	account, _ := client.ProviderAccount.Create().SetTenantID(tenant.ID).SetName("provider").SetBaseURL(autodl.PrivateBaseURL).SetBackend("private").SetStatus("active").SetCredentialCiphertext("cipher").Save(ctx)
	experimentRecord, err := client.Experiment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(agent.ID).
		SetRepositoryID(repositoryRecord.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetState("running").
		SetCommitSha("0123456789012345678901234567890123456789").SetCommand("true").SetMaxRuntimeSeconds(60).SetTimeoutExtensionSeconds(30).SetTerminationGraceSeconds(5).
		SetRepositorySnapshot(map[string]any{}).SetEnvironmentSnapshot(map[string]any{}).SetResourceSnapshot(map[string]any{}).
		SetOutputPath("/root/autodl-fs/test/").SetReservedCostMilli(100).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attemptRecord, err := client.Attempt.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetExperimentID(experimentRecord.ID).SetNumber(1).SetState("running").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	builder := client.ProviderResource.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetExperimentID(experimentRecord.ID).
		SetAttemptID(attemptRecord.ID).SetProviderAccountID(account.ID).SetName("gemcp-owned").SetOwned(owned).SetState(providerresource.StateActive).SetHardDeadlineAt(hardDeadline)
	if providerID != nil {
		builder.SetProviderID(*providerID)
	}
	resourceRecord, _ := builder.Save(ctx)
	provider := &watchdogProvider{}
	config := DefaultConfig()
	config.InstanceID = "watchdog-test"
	config.LeaseDuration = 30 * time.Second
	config.ReconcileDelay = time.Second
	service, err := New(client, provider, config)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &watchdogFixture{client: client, service: service, provider: provider, experiment: experimentRecord, resource: resourceRecord, now: time.Now().UTC().Truncate(time.Second)}
	service.now = func() time.Time { return fixture.now }
	return fixture
}

func TestWatchdogStopsAndDeletesOnlyOverdueOwnedResource(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	providerID := "deployment-1"
	f := newWatchdogFixture(t, &providerID, true, now.Add(-time.Second))
	f.now = now
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.provider.stopCalls != 1 || f.provider.deleteCalls != 1 {
		t.Fatalf("calls stop=%d delete=%d", f.provider.stopCalls, f.provider.deleteCalls)
	}
	resourceRecord, _ := f.client.ProviderResource.Get(context.Background(), f.resource.ID)
	experimentRecord, _ := f.client.Experiment.Get(context.Background(), f.experiment.ID)
	if resourceRecord.State != providerresource.StateDeleted || resourceRecord.StopReason == nil || *resourceRecord.StopReason != "timeout" || resourceRecord.LeaseOwner != nil {
		t.Fatalf("resource = %+v", resourceRecord)
	}
	if experimentRecord.State != "cancelling" {
		t.Fatalf("experiment state = %s", experimentRecord.State)
	}
	if count, _ := f.client.AuditEvent.Query().Count(context.Background()); count != 1 {
		t.Fatalf("audit count = %d", count)
	}
}

func TestWatchdogWaitsForConfirmedDeletion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	providerID := "deployment-1"
	f := newWatchdogFixture(t, &providerID, true, now.Add(-time.Second))
	f.now = now
	f.provider.observe = execution.Observation{Found: true, Deployment: autodl.Deployment{UUID: providerID, Status: "deleting"}}
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	resourceRecord, _ := f.client.ProviderResource.Get(context.Background(), f.resource.ID)
	if resourceRecord.State != providerresource.StateDeleting || resourceRecord.DeletedAt != nil || f.provider.deleteCalls != 1 {
		t.Fatalf("resource=%+v provider=%+v", resourceRecord, f.provider)
	}
	f.provider.observe = execution.Observation{}
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	resourceRecord, _ = f.client.ProviderResource.Get(context.Background(), f.resource.ID)
	if resourceRecord.State != providerresource.StateDeleted || resourceRecord.DeletedAt == nil || f.provider.deleteCalls != 1 {
		t.Fatalf("resource=%+v provider=%+v", resourceRecord, f.provider)
	}
}

func TestWatchdogRetiresUnattemptedCreateAfterProvisionDeadline(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	f := newWatchdogFixture(t, nil, true, now.Add(-time.Second))
	f.now = now
	_, _ = f.resource.Update().SetState(providerresource.StateCreating).Save(context.Background())
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	resourceRecord, _ := f.client.ProviderResource.Get(context.Background(), f.resource.ID)
	if f.provider.findCalls != 1 || f.provider.stopCalls != 0 || f.provider.deleteCalls != 0 || resourceRecord.State != providerresource.StateDeleted {
		t.Fatalf("provider=%+v resource=%+v", f.provider, resourceRecord)
	}
}

func TestWatchdogAdoptsAmbiguousCreateByOwnedNameBeforeStopping(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	f := newWatchdogFixture(t, nil, true, now.Add(-time.Second))
	f.now = now
	_, _ = f.resource.Update().SetState(providerresource.StateCreating).SetCreateAttempts(1).SetCreateAttemptedAt(now.Add(-2 * time.Second)).Save(context.Background())
	f.provider.find = execution.Observation{Found: true, Deployment: autodl.Deployment{UUID: "adopted-1", Name: "gemcp-owned", Status: "running"}}
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	resourceRecord, _ := f.client.ProviderResource.Get(context.Background(), f.resource.ID)
	if f.provider.findCalls != 1 || f.provider.stopCalls != 1 || f.provider.deleteCalls != 1 || resourceRecord.ProviderID == nil || *resourceRecord.ProviderID != "adopted-1" || resourceRecord.State != providerresource.StateDeleted {
		t.Fatalf("provider=%+v resource=%+v", f.provider, resourceRecord)
	}
}

func TestWatchdogAllowsTimeoutTerminationGrace(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	providerID := "deployment-1"
	f := newWatchdogFixture(t, &providerID, true, now.Add(time.Minute))
	f.now = now
	_, _ = f.resource.Update().SetStopRequestedAt(now).SetStopReason("timeout").Save(context.Background())
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.provider.stopCalls != 0 {
		t.Fatalf("watchdog stopped timeout resource before grace deadline")
	}
	f.now = now.Add(time.Minute)
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.provider.stopCalls != 1 {
		t.Fatalf("watchdog stop calls = %d, want 1", f.provider.stopCalls)
	}
}

func TestWatchdogIgnoresFutureAndUnownedResources(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	providerID := "deployment-1"
	future := newWatchdogFixture(t, &providerID, true, now.Add(time.Hour))
	future.now = now
	if err := future.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if future.provider.stopCalls != 0 {
		t.Fatalf("future resource was stopped")
	}
	unowned := newWatchdogFixture(t, &providerID, false, now.Add(-time.Hour))
	unowned.now = now
	if err := unowned.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if unowned.provider.stopCalls != 0 {
		t.Fatalf("unowned resource was stopped")
	}
}

func TestWatchdogKeepsRetryableCleanupStateAndReleasesLease(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	providerID := "deployment-1"
	f := newWatchdogFixture(t, &providerID, true, now.Add(-time.Second))
	f.now = now
	f.provider.stopErr = errors.New("temporary Provider failure")
	if err := f.service.Tick(context.Background()); err == nil {
		t.Fatal("Tick() accepted stop failure")
	}
	resourceRecord, _ := f.client.ProviderResource.Get(context.Background(), f.resource.ID)
	if resourceRecord.State != providerresource.StateStopping || resourceRecord.LastError == nil || resourceRecord.LeaseOwner != nil || f.provider.deleteCalls != 0 {
		t.Fatalf("resource=%+v provider=%+v", resourceRecord, f.provider)
	}
}
