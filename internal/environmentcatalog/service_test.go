package environmentcatalog

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/provider"
	_ "github.com/mattn/go-sqlite3"
)

type fakeImages struct {
	snapshot provider.ResourceSnapshot
}

func (f fakeImages) QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error) {
	return f.snapshot, nil
}

func TestRegisterAcceptsVisibleImageAndRejectsUnknown(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:environmentcatalog?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
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
	service := NewService(client, fakeImages{snapshot: provider.ResourceSnapshot{
		PrivateImages: []provider.Image{{UUID: "image-visible1234", Name: "torch"}},
	}})
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenPublicID: "token", Scopes: []string{"read", "submit"},
	}
	input := RegisterInput{Name: "torch-mamba", ImageUUID: "image-visible1234"}
	if _, err := service.Register(ctx, principal, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing configure scope error = %v", err)
	}
	principal.Scopes = []string{"configure", "read"}
	if _, err := service.Register(ctx, principal, RegisterInput{Name: "torch-mamba", ImageUUID: "image-unknown9999"}); !errors.Is(err, ErrImage) {
		t.Fatalf("unknown image error = %v", err)
	}
	view, err := service.Register(ctx, principal, input)
	if err != nil || view.ImageUUID != "image-visible1234" || view.Backend != BackendElastic {
		t.Fatalf("Register() = %+v, %v", view, err)
	}
	owner, err := service.OwnerRegister(ctx, tenant.ID, "owner-1", project.PublicID.String(), RegisterInput{
		Name: "official-base", ImageUUID: "image-6c15b8aad2",
	})
	if err != nil || owner.ImageUUID != "image-6c15b8aad2" {
		t.Fatalf("OwnerRegister official image = %+v, %v", owner, err)
	}
}
