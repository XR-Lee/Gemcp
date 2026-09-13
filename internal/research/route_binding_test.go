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

func TestWorkspaceKeepsRequestedRefAndDoesNotInventDefaultBranch(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-ref-identity?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("ref-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("DynamicPointMamba").SetSSHURL("git@github.com:XR-Lee/DynamicPointMamba.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	live, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").
		SetCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{
			"id": repository.PublicID.String(), "name": "DynamicPointMamba",
			"ssh_url": "git@github.com:XR-Lee/DynamicPointMamba.git", "default_branch": "main",
			"requested_ref": "autoresearch/objbg-baseline",
		}).
		SetEnvironmentSnapshot(map[string]any{"name": "default"}).SetResourceSnapshot(map[string]any{"name": "default"}).
		SetOutputPath("/outputs").SetReservedCostMilli(0).Save(ctx)
	orphan, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").
		SetCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "DynamicPointMamba", "default_branch": "main"}).
		SetEnvironmentSnapshot(map[string]any{"name": "default"}).SetResourceSnapshot(map[string]any{"name": "default"}).
		SetOutputPath("/outputs").SetReservedCostMilli(0).Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{
			Name: "dpm-scan", Question: "Does a live autoresearch ref stay distinct from the repository default branch?",
			RepositoryID: repository.PublicID.String(), ProtocolBranch: "research-plan", CodeRefPattern: "autoresearch/*",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Live code stays on autoresearch", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "run", Title: "OBJ-BG live", ExperimentID: live.PublicID.String(), FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to"},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "run", Title: "Imported without a requested ref", ExperimentID: orphan.PublicID.String(), FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Study == nil || second.Study.Route == nil || second.Study.Route.CodeRefPattern != "autoresearch/*" {
		t.Fatalf("route = %+v", second.Study)
	}
	if len(second.Study.Hypotheses) != 1 || len(second.Study.Hypotheses[0].Experiments) != 2 {
		t.Fatalf("hypotheses = %+v", second.Study.Hypotheses)
	}
	liveRec := second.Study.Hypotheses[0].Experiments[0]
	if liveRec.Branch != "autoresearch/objbg-baseline" || liveRec.GitIdentity == nil || liveRec.GitIdentity.DefaultBranch != "main" {
		t.Fatalf("live identity = %+v", liveRec)
	}
	orphanRec := second.Study.Hypotheses[0].Experiments[1]
	if orphanRec.Branch != "" {
		t.Fatalf("invented default branch for a run without requested_ref: %+v", orphanRec)
	}
	if second.Study.Hypotheses[0].Branch != "autoresearch/objbg-baseline" {
		t.Fatalf("hypothesis branch collapsed = %q", second.Study.Hypotheses[0].Branch)
	}
}

func TestNextActionsCarryRouteAndSuggestPlanSync(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-route-actions?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("route-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{
			Name: "route-study", Question: "Should prepare name the allowed live ref family?",
			ProtocolBranch: "research-plan", ProtocolDocPath: "research-plan/STATUS.md", CodeRefPattern: "autoresearch/*",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Only autoresearch refs may spend", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	prepare := hypothesis.NextActions[0]
	if prepare.Kind != "prepare_experiment" || prepare.AllowedRefPattern != "autoresearch/*" || prepare.ProtocolBranch != "research-plan" {
		t.Fatalf("prepare action = %+v", prepare)
	}
	if !strings.Contains(prepare.Detail, "autoresearch/*") || !strings.Contains(prepare.Detail, "Do not omit ref") {
		t.Fatalf("prepare detail = %q", prepare.Detail)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "decision", Title: "Export the protocol amendment next", FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to"},
	}); err != nil {
		t.Fatal(err)
	}
	actions, err := service.AgentNextActions(ctx, principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !containsActionKind(actions.Actions, "export_plan_sync") {
		t.Fatalf("next actions = %+v", actions.Actions)
	}
}
