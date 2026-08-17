package research

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	_ "github.com/mattn/go-sqlite3"
)

func TestAgentMaintainsStudyPlanAndGraphWithoutStartingWorkloads(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-workspace?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("lab-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	experimentRecord, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)

	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "objbg-scan", Question: "Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?"},
		Plan: &PlanInput{
			Goal:       "Establish a reproducible OBJ-BG baseline before changing the traversal.",
			NextAction: "Record the current smoke-run accuracy as the first Graph result.",
			Rationale:  "The Owner should see the scientific question before any new reservation.",
			Steps:      []PlanStep{{Title: "Link the existing smoke Experiment", Detail: "Do not submit a new run yet."}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Study == nil || created.Study.Name != "objbg-scan" || created.Study.Plan == nil || created.Study.Plan.NextAction == "" || len(created.Study.Nodes) != 1 {
		t.Fatalf("created workspace = %+v", created.Study)
	}
	if count, _ := client.Experiment.Query().Count(ctx); count != 1 {
		t.Fatalf("update created an Experiment, count=%d", count)
	}

	metric := 86.4
	updated, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "result", Title: "OBJ-BG smoke accuracy", Summary: "The existing smoke Experiment reached 86.4 overall accuracy.",
			Status: "succeeded", MetricName: "overall_accuracy", MetricValue: &metric,
			ExperimentID: experimentRecord.PublicID.String(), FromNodeID: created.Study.Nodes[0].ID, Relation: "produced",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Study == nil || len(updated.Study.Nodes) != 2 || len(updated.Study.Edges) != 1 || updated.Study.Nodes[1].ExperimentID != experimentRecord.PublicID.String() {
		t.Fatalf("updated graph = %+v", updated.Study)
	}

	readOnly := principal
	readOnly.Scopes = []string{"read"}
	if _, err := service.AgentUpdate(ctx, readOnly, UpdateInput{Study: &StudyInput{Name: "other", Question: "Should this be rejected?"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only update error = %v", err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "secret", Question: "Use token gmc_secret_value_here to continue."},
	}); err == nil {
		t.Fatal("accepted a credential-like question")
	}

	otherProject, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Other").SetSlug("other").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	otherToken, _ := client.AgentToken.Create().SetProjectID(otherProject.ID).SetLabel("other").SetPrefix("gmc_oth").SetTokenHash([]byte("other-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	otherPrincipal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: otherProject.ID, ProjectPublicID: otherProject.PublicID.String(),
		TokenID: otherToken.ID, TokenPublicID: otherToken.PublicID.String(), Scopes: otherToken.Scopes,
	}
	if _, err := service.AgentUpdate(ctx, otherPrincipal, UpdateInput{
		Study: &StudyInput{Name: "cross", Question: "Should a second Project see the first Study?"},
		Node:  &NodeInput{Kind: "run", Title: "Steal run", ExperimentID: experimentRecord.PublicID.String()},
	}); err == nil {
		t.Fatal("accepted a cross-Project Experiment link")
	}
	owner, err := service.OwnerWorkspace(ctx, tenant.ID, project.PublicID.String(), "")
	if err != nil || owner.Study == nil || owner.Study.ID != created.Study.ID || owner.Study.Plan == nil {
		t.Fatalf("OwnerWorkspace() = %+v, %v", owner, err)
	}
	if audits, _ := client.AuditEvent.Query().Count(ctx); audits != 3 {
		t.Fatalf("audit count = %d", audits)
	}
}

func TestStudySelectorIsRequiredWhenMultipleStudiesExist(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-choice?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("choice-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{Study: &StudyInput{Name: "first", Question: "What is the first question to answer?"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{Study: &StudyInput{Name: "second", Question: "What is the second question to answer?"}}); err != nil {
		t.Fatal(err)
	}
	workspace, err := service.AgentWorkspace(ctx, principal, WorkspaceInput{})
	if err != nil || workspace.Study != nil || len(workspace.Studies) != 2 {
		t.Fatalf("AgentWorkspace() = %+v, %v", workspace, err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Plan: &PlanInput{Goal: "Need an explicit Study", NextAction: "Choose first or second before replacing the plan."},
	}); !errors.Is(err, ErrChoice) {
		t.Fatalf("AgentUpdate() without study selector error = %v", err)
	}
}
