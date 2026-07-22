package finance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	_ "github.com/mattn/go-sqlite3"
)

type financeFixture struct {
	client     *ent.Client
	service    *Service
	tenant     *ent.Tenant
	project    *ent.Project
	experiment *ent.Experiment
	now        time.Time
}

func newFinanceFixture(t *testing.T) financeFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	tenant, err := client.Tenant.Create().SetName("Finance Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").
		SetMonthlyBudgetMilli(100_000).SetMaxExperimentMilli(50_000).SetTimezone("UTC").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("agent").SetPrefix("gmc_test").
		SetTokenHash([]byte(strings.Repeat("a", 32))).SetScopes([]string{"read", "submit"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := client.Repository.Create().SetProjectID(project.ID).SetName("repo").
		SetSSHURL("git@github.com:example/repo.git").SetSSHHost("github.com").SetDefaultBranch("main").SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := client.Environment.Create().SetProjectID(project.ID).SetName("autodl").SetImageUUID("image-id").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("autodl").SetRegion("private").
		SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).SetCPUFrom(1).SetCPUTo(8).
		SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(1).SetPriceToMilli(1000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := client.Experiment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
		SetRepositoryID(repository.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).
		SetCommitSha(strings.Repeat("0", 40)).SetCommand("python smoke_test.py").SetMaxRuntimeSeconds(600).
		SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"id": repository.PublicID.String()}).
		SetEnvironmentSnapshot(map[string]any{"backend": "autodl_private", "image_uuid": "image-id"}).
		SetResourceSnapshot(map[string]any{"backend": "autodl_private"}).
		SetOutputPath("/root/autodl-fs/test").SetReservedCostMilli(20_000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return financeFixture{
		client: client, service: NewService(client, WithClock(func() time.Time { return now })),
		tenant: tenant, project: project, experiment: experiment, now: now,
	}
}

func TestAdjustmentIsProjectScopedIdempotentAndAudited(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()
	input := AdjustmentInput{Direction: "credit", AmountMilli: 25_000, Reason: "Initial test credit", IdempotencyKey: "credit-20260722-001"}
	created, err := fixture.service.Adjust(ctx, fixture.tenant.ID, "owner-1", fixture.project.PublicID.String(), input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Idempotent || created.Entry.Direction != "credit" || created.Entry.AmountMilli != -25_000 || created.Entry.BalanceEffectMilli != 25_000 || created.Entry.ExperimentID != "" {
		t.Fatalf("created adjustment=%+v", created)
	}
	repeated, err := fixture.service.Adjust(ctx, fixture.tenant.ID, "owner-1", fixture.project.PublicID.String(), input)
	if err != nil || !repeated.Idempotent || repeated.Entry.ID != created.Entry.ID {
		t.Fatalf("repeated adjustment=%+v err=%v", repeated, err)
	}
	input.AmountMilli++
	if _, err := fixture.service.Adjust(ctx, fixture.tenant.ID, "owner-1", fixture.project.PublicID.String(), input); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting adjustment error=%v", err)
	}
	entry, err := fixture.client.BudgetEntry.Query().Only(ctx)
	if err != nil || entry.ExperimentID != nil || entry.IdempotencyKey == nil {
		t.Fatalf("stored adjustment=%+v err=%v", entry, err)
	}
	audit, err := fixture.client.AuditEvent.Query().Only(ctx)
	if err != nil || audit.Action != "budget.credit_recorded" || audit.ActorID != "owner-1" || audit.TargetID != entry.PublicID.String() {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
}

func TestDashboardAggregatesCreditsChargesBackendsAndLogs(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()
	if _, err := fixture.service.Adjust(ctx, fixture.tenant.ID, "owner-1", fixture.project.PublicID.String(), AdjustmentInput{
		Direction: "credit", AmountMilli: 25_000, Reason: "Monthly recharge", IdempotencyKey: "credit-20260722-002",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Adjust(ctx, fixture.tenant.ID, "owner-1", fixture.project.PublicID.String(), AdjustmentInput{
		Direction: "debit", AmountMilli: 5_000, Reason: "Balance correction", IdempotencyKey: "debit-20260722-001",
	}); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		kind   string
		amount int64
		desc   string
	}{
		{kind: "reservation", amount: 20_000, desc: "test reservation"},
		{kind: "release", amount: -20_000, desc: "test release"},
		{kind: "charge", amount: 8_000, desc: "test charge"},
	} {
		if _, err := fixture.client.BudgetEntry.Create().SetTenantID(fixture.tenant.ID).SetProjectID(fixture.project.ID).
			SetExperimentID(fixture.experiment.ID).SetPeriod("2026-07").SetKind(entry.kind).SetAmountMilli(entry.amount).
			SetDescription(entry.desc).SetCreatedAt(fixture.now).Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	dashboard, err := fixture.service.Dashboard(ctx, fixture.tenant.ID, "2026-07", "")
	if err != nil {
		t.Fatal(err)
	}
	want := Totals{
		BaseBudgetMilli: 100_000, ReservedMilli: 0, ChargedMilli: 8_000, CreditsMilli: 25_000,
		DebitsMilli: 5_000, CommittedMilli: -12_000, AvailableMilli: 112_000,
	}
	if dashboard.Totals != want {
		t.Fatalf("totals=%+v want=%+v", dashboard.Totals, want)
	}
	if len(dashboard.Projects) != 1 || dashboard.Projects[0].Totals != want {
		t.Fatalf("projects=%+v", dashboard.Projects)
	}
	if len(dashboard.Backends) != 1 || dashboard.Backends[0].Backend != "autodl_private" || dashboard.Backends[0].Experiments != 1 || dashboard.Backends[0].ChargedMilli != 8_000 {
		t.Fatalf("backends=%+v", dashboard.Backends)
	}
	if len(dashboard.Ledger) != 5 || len(dashboard.Audit) != 2 || dashboard.AuditScope != "organization" {
		t.Fatalf("ledger=%d audit=%d scope=%s", len(dashboard.Ledger), len(dashboard.Audit), dashboard.AuditScope)
	}
	if len(dashboard.Daily) == 0 {
		t.Fatal("daily analytics are empty")
	}
	filtered, err := fixture.service.Dashboard(ctx, fixture.tenant.ID, "2026-07", fixture.project.PublicID.String())
	if err != nil || len(filtered.Projects) != 1 {
		t.Fatalf("filtered dashboard=%+v err=%v", filtered, err)
	}
}

func TestFinanceValidationAndTenantIsolation(t *testing.T) {
	fixture := newFinanceFixture(t)
	ctx := context.Background()
	if _, err := fixture.service.Dashboard(ctx, fixture.tenant.ID, "2026-13", ""); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("period error=%v", err)
	}
	if _, err := fixture.service.Adjust(ctx, fixture.tenant.ID, "owner", fixture.project.PublicID.String(), AdjustmentInput{
		Direction: "credit", AmountMilli: 0, Reason: "bad", IdempotencyKey: "invalid-key-001",
	}); !errors.Is(err, ErrInvalidAdjustment) {
		t.Fatalf("adjustment error=%v", err)
	}
	otherTenant, err := fixture.client.Tenant.Create().SetName("Other").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Adjust(ctx, otherTenant.ID, "owner", fixture.project.PublicID.String(), AdjustmentInput{
		Direction: "credit", AmountMilli: 1_000, Reason: "Other tenant", IdempotencyKey: "other-tenant-001",
	}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("tenant isolation error=%v", err)
	}
}
