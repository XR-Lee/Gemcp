package imagebake

import (
	"context"
	"errors"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/XR-Lee/Gemcp/internal/provider"
	_ "github.com/mattn/go-sqlite3"
)

type bakeFixture struct {
	client     *ent.Client
	provider   *FakeProvider
	service    *Service
	envService *environmentcatalog.Service
	tenantID   int
	project    *ent.Project
	repository *ent.Repository
	principal  agentauth.Principal
}

func newBakeFixture(t *testing.T) *bakeFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("lab").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().SetTenantID(tenant.ID).SetName("p").SetSlug("p").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := client.Repository.Create().SetProjectID(project.ID).SetName("source").
		SetSSHURL("git@github.com:owner/repository.git").SetSSHHost("github.com").SetDefaultBranch("main").
		SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake := NewFakeProvider("image-baked12345")
	service := NewService(client, fake, nil)
	envService := environmentcatalog.NewService(client, fakeImages{})
	return &bakeFixture{
		client: client, provider: fake, service: service, envService: envService,
		tenantID: tenant.ID, project: project, repository: repository,
		principal: agentauth.Principal{
			TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
			TokenPublicID: "token-1", Scopes: []string{"read", "configure"},
		},
	}
}

type fakeImages struct {
	snapshot provider.ResourceSnapshot
}

func (f fakeImages) QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error) {
	return f.snapshot, nil
}

func (f *bakeFixture) requestInput() RequestInput {
	return RequestInput{
		Name: "torch-mamba", BaseImageUUID: "image-base12345",
		CommitSHA: strings.Repeat("a", 40), RecipePath: "requirements.gemcp.txt",
	}
}

func TestConfigureRequestIsZeroCostAndDoesNotProvision(t *testing.T) {
	f := newBakeFixture(t)
	ctx := context.Background()
	submitOnly := f.principal
	submitOnly.Scopes = []string{"read", "submit"}
	if _, err := f.service.Request(ctx, submitOnly, f.requestInput()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("submit-only request error = %v", err)
	}
	if f.provider.CreateCount() != 0 {
		t.Fatalf("submit-only request created Pro instances: %d", f.provider.CreateCount())
	}

	view, err := f.service.Request(ctx, f.principal, f.requestInput())
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "requested" || view.ConfirmationDigest == "" || view.ImageUUID != "" || view.EstimatedCostMilli != 0 {
		t.Fatalf("requested view = %+v", view)
	}
	if f.provider.CreateCount() != 0 {
		t.Fatalf("request provisioned Pro: creates=%d", f.provider.CreateCount())
	}
	experiments, nodes, budget, err := f.service.SideEffectCounts(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if experiments != 0 || nodes != 0 || budget != 0 {
		t.Fatalf("request side effects experiments=%d nodes=%d budget=%d", experiments, nodes, budget)
	}

	readOnly := f.principal
	readOnly.Scopes = []string{"read"}
	listed, err := f.service.List(ctx, readOnly)
	if err != nil || len(listed.Bakes) != 1 || listed.Bakes[0].ID != view.ID {
		t.Fatalf("List() = %+v, %v", listed, err)
	}
	got, err := f.service.Get(ctx, readOnly, view.ID)
	if err != nil || got.ConfirmationDigest != view.ConfirmationDigest || got.Status != "requested" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if _, err := f.service.Request(ctx, readOnly, f.requestInput()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only request error = %v", err)
	}

	if _, err := f.service.OwnerConfirm(ctx, f.tenantID, "owner-1", f.project.PublicID.String(), view.ID, "sha256:deadbeef"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("wrong digest error = %v", err)
	}
	if f.provider.CreateCount() != 0 {
		t.Fatalf("wrong digest started Pro: %d", f.provider.CreateCount())
	}

	finished, err := f.service.OwnerConfirm(ctx, f.tenantID, "owner-1", f.project.PublicID.String(), view.ID, view.ConfirmationDigest)
	if err != nil {
		t.Fatal(err)
	}
	if f.provider.CreateCount() != 1 {
		t.Fatalf("confirm creates = %d, want 1", f.provider.CreateCount())
	}
	if finished.Status != "finished" || finished.ImageUUID != "image-baked12345" || finished.InstanceUUID == "" {
		t.Fatalf("finished view = %+v", finished)
	}
	experiments, nodes, budget, err = f.service.SideEffectCounts(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if experiments != 0 || nodes != 0 || budget != 0 {
		t.Fatalf("confirm side effects experiments=%d nodes=%d budget=%d", experiments, nodes, budget)
	}

	registered, err := f.envService.Register(ctx, f.principal, environmentcatalog.RegisterInput{
		Name: "baked-torch", ImageUUID: finished.ImageUUID,
	})
	if err != nil || registered.ImageUUID != finished.ImageUUID {
		t.Fatalf("register_environment from bake = %+v, %v", registered, err)
	}
}

func TestFailedBakeWritesLabStatusOnly(t *testing.T) {
	f := newBakeFixture(t)
	ctx := context.Background()
	f.provider.SaveErr = errors.New("save rejected")
	view, err := f.service.Request(ctx, f.principal, f.requestInput())
	if err != nil {
		t.Fatal(err)
	}
	failed, err := f.service.OwnerConfirm(ctx, f.tenantID, "owner-1", f.project.PublicID.String(), view.ID, view.ConfirmationDigest)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.ImageUUID != "" || failed.FailureReason == "" {
		t.Fatalf("failed view = %+v", failed)
	}
	experiments, nodes, budget, err := f.service.SideEffectCounts(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if experiments != 0 || nodes != 0 || budget != 0 {
		t.Fatalf("failed bake side effects experiments=%d nodes=%d budget=%d", experiments, nodes, budget)
	}
}

func TestOwnerCancelRequestedDoesNotProvision(t *testing.T) {
	f := newBakeFixture(t)
	ctx := context.Background()
	view, err := f.service.Request(ctx, f.principal, f.requestInput())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.OwnerCancel(ctx, f.tenantID, "owner-1", f.project.PublicID.String(), view.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel = %+v, %v", cancelled, err)
	}
	if f.provider.CreateCount() != 0 {
		t.Fatalf("cancel provisioned Pro: %d", f.provider.CreateCount())
	}
}

func TestFailClosedConfirmDoesNotInventAnImage(t *testing.T) {
	f := newBakeFixture(t)
	f.service.provider = NewFailClosedProvider()
	ctx := context.Background()
	view, err := f.service.Request(ctx, f.principal, f.requestInput())
	if err != nil {
		t.Fatal(err)
	}
	failed, err := f.service.OwnerConfirm(ctx, f.tenantID, "owner-1", f.project.PublicID.String(), view.ID, view.ConfirmationDigest)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.ImageUUID != "" {
		t.Fatalf("fail-closed confirm = %+v", failed)
	}
}
