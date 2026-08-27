package datasetcatalog

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	_ "github.com/mattn/go-sqlite3"
)

func TestRegisterRejectsForeignRootsAndRequiresConfigure(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:datasetcatalog?mode=memory&cache=shared&_fk=1")
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
	service := NewService(client)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: 1, TokenPublicID: "token", Scopes: []string{"read", "submit"},
	}
	input := RegisterInput{Name: "scanobjectnn-objbg", CanonicalRoot: "/root/autodl-fs/datasets/ScanObjectNN"}
	if _, err := service.Register(ctx, principal, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing configure scope error = %v", err)
	}
	principal.Scopes = []string{"configure", "read"}
	if _, err := service.Register(ctx, principal, RegisterInput{Name: "scanobjectnn-objbg", CanonicalRoot: "/root/autodl-tmp/data"}); err == nil {
		t.Fatal("expected foreign root to fail")
	}
	view, err := service.OwnerRegister(ctx, tenant.ID, "owner-1", project.PublicID.String(), RegisterInput{
		Catalog: "scanobjectnn-objbg",
		Sources: []SourceFile{{
			URL: "https://huggingface.co/datasets/example/resolve/main/train.h5", RelativePath: "main_split/train.h5",
		}},
	})
	if err != nil || view.EnvironmentVariable != "GEMCP_DATASET_SCANOBJECTNN_OBJBG" || view.Backend != BackendElastic ||
		view.CanonicalRoot != "/root/autodl-fs/datasets/ScanObjectNN" || len(view.Sources) != 1 {
		t.Fatalf("OwnerRegister() = %+v, %v", view, err)
	}
	listed, err := service.OwnerList(ctx, tenant.ID, project.PublicID.String())
	if err != nil || len(listed.Bindings) != 1 {
		t.Fatalf("OwnerList() = %+v, %v", listed, err)
	}
}
