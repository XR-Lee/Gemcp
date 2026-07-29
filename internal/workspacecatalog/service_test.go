package workspacecatalog

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestProjectAgentRegistersOnlyRelativePathsBelowTrustedWorkspace(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:workspace-catalog?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("config-agent").SetPrefix("gmc_config").SetTokenHash([]byte("config-hash")).SetScopes([]string{"read", "configure"}).Save(ctx)
	node, _ := client.SelfHostedNode.Create().SetTenantID(tenant.ID).SetLabel("usb-pc").SetTokenPrefix("gmn_test").SetTokenHash([]byte("node-hash")).
		SetStatus("active").SetInstallationID(uuid.NewString()).SetMachineFingerprint("fingerprint").SetHostname("node").SetOperatingSystem("linux").
		SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion("1").Save(ctx)
	_, _ = client.NodeProjectAccess.Create().SetTenantID(tenant.ID).SetNodeID(node.ID).SetProjectID(project.ID).
		SetExecutionPolicy(nodeprojectaccess.ExecutionPolicyTrustedWorkspace).SetWorkspacePath("/srv/gemcp-workspace").Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	input := RegisterInput{Name: "scanobjectnn-objbg", RelativePath: "data/ScanObjectNN/main_split"}
	registered, err := service.Register(ctx, principal, input)
	if err != nil {
		t.Fatal(err)
	}
	if registered.NodeID != node.PublicID.String() || registered.ContainerPath != "/gemcp/workspace/data/ScanObjectNN/main_split" ||
		registered.EnvironmentVariable != "GEMCP_DATASET_SCANOBJECTNN_OBJBG" || registered.Status != "active" {
		t.Fatalf("registered dataset = %+v", registered)
	}
	retried, err := service.Register(ctx, principal, input)
	if err != nil || retried.ID != registered.ID {
		t.Fatalf("idempotent register = %+v, %v", retried, err)
	}
	if count, _ := client.WorkspaceDataset.Query().Count(ctx); count != 1 {
		t.Fatalf("dataset count = %d", count)
	}
	for _, invalidPath := range []string{"/etc/passwd", "../outside", "data//nested", "data/../outside", "."} {
		if _, err := service.Register(ctx, principal, RegisterInput{Name: "invalid-" + uuid.NewString()[:8], RelativePath: invalidPath}); err == nil {
			t.Fatalf("Register() accepted %q", invalidPath)
		}
	}
	withoutConfigure := principal
	withoutConfigure.Scopes = []string{"read"}
	if _, err := service.Register(ctx, withoutConfigure, RegisterInput{Name: "other", RelativePath: "data/other"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Register() without configure error = %v", err)
	}
	listed, err := service.List(ctx, principal)
	if err != nil || len(listed.Datasets) != 1 || listed.Datasets[0].ID != registered.ID {
		t.Fatalf("List() = %+v, %v", listed, err)
	}
	disabled, err := service.Remove(ctx, principal, RemoveInput{DatasetID: registered.ID})
	if err != nil || disabled.Status != "disabled" {
		t.Fatalf("Remove() = %+v, %v", disabled, err)
	}
	if audits, _ := client.AuditEvent.Query().Count(ctx); audits != 3 {
		t.Fatalf("audit count = %d", audits)
	}
}

func TestWorkspaceDatasetCannotCrossProject(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:workspace-catalog-isolation?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	first, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("First").SetSlug("first").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	second, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Second").SetSlug("second").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	node, _ := client.SelfHostedNode.Create().SetTenantID(tenant.ID).SetLabel("node").SetTokenPrefix("gmn_test").SetTokenHash([]byte("node-hash-2")).
		SetStatus("active").SetInstallationID(uuid.NewString()).SetMachineFingerprint("fingerprint").SetHostname("node").SetOperatingSystem("linux").
		SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion("1").Save(ctx)
	_, _ = client.NodeProjectAccess.Create().SetTenantID(tenant.ID).SetNodeID(node.ID).SetProjectID(first.ID).
		SetExecutionPolicy(nodeprojectaccess.ExecutionPolicyTrustedWorkspace).SetWorkspacePath("/srv/first").Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(second.ID).SetLabel("second-agent").SetPrefix("gmc_second").SetTokenHash([]byte("second-hash")).SetScopes([]string{"read", "configure"}).Save(ctx)
	principal := agentauth.Principal{TenantID: tenant.ID, ProjectID: second.ID, ProjectPublicID: second.PublicID.String(), TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes}
	if _, err := NewService(client).Register(ctx, principal, RegisterInput{Name: "private", RelativePath: "data/private", Workspace: node.PublicID.String()}); !errors.Is(err, ErrTrustedWorkspace) {
		t.Fatalf("cross-project Register() error = %v", err)
	}
}
