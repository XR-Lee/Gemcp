package research

import (
	"context"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	_ "github.com/mattn/go-sqlite3"
)

func TestExportResearchPlanSyncDryRunAndRecord(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-plan-sync?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("plan-sync-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{
			Name: "dpm-route", Question: "Can a docs-only protocol branch stay separate from live experiment refs?",
			ProtocolBranch: "research-plan", ProtocolDocPath: "research-plan/STATUS.md", CodeRefPattern: "autoresearch/*",
		},
	})
	if err != nil || created.Study == nil || created.Study.Route == nil {
		t.Fatalf("create study = %+v, %v", created.Study, err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "hypothesis", Title: "Protocol docs stay off the training tree",
			FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "decision", Title: "Keep live code on autoresearch refs",
			FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to",
		},
	}); err != nil {
		t.Fatal(err)
	}

	dry, err := service.AgentExportPlanSync(ctx, principal, PlanSyncInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Recorded || dry.TargetBranch != "research-plan" || dry.TargetPath != "research-plan/STATUS.md" {
		t.Fatalf("dry-run identity = %+v", dry)
	}
	if !strings.Contains(dry.Markdown, "Gemcp research-plan amendment") ||
		!strings.Contains(dry.Markdown, "Keep live code on autoresearch refs") ||
		!strings.Contains(dry.Markdown, planSyncMetricsDisclaimer) ||
		!strings.Contains(dry.Markdown, "Do not force-push") ||
		strings.Contains(dry.Markdown, "export_research_plan_sync") {
		t.Fatalf("dry-run markdown = %s", dry.Markdown)
	}
	if !strings.Contains(dry.Warning, "does not push") {
		t.Fatalf("warning = %q", dry.Warning)
	}

	falseValue := false
	recorded, err := service.AgentExportPlanSync(ctx, principal, PlanSyncInput{DryRun: &falseValue})
	if err != nil || recorded.DryRun || !recorded.Recorded {
		t.Fatalf("recorded export = %+v, %v", recorded, err)
	}
	readOnly := principal
	readOnly.Scopes = []string{"read"}
	if _, err := service.AgentExportPlanSync(ctx, readOnly, PlanSyncInput{DryRun: &falseValue}); err != ErrForbidden {
		t.Fatalf("read-only record error = %v", err)
	}
	if _, err := service.AgentExportPlanSync(ctx, principal, PlanSyncInput{TargetPath: "gemcp.yaml"}); err == nil {
		t.Fatal("accepted frozen recipe path")
	}
}

func TestExportResearchPlanSyncRequiresStudy(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-plan-sync-empty?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("empty").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("empty-hash")).SetScopes([]string{"read"}).Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	_, err := NewService(client).AgentExportPlanSync(ctx, principal, PlanSyncInput{})
	if err == nil {
		t.Fatal("exported without a Study")
	}
}
