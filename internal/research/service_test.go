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
	if len(created.NextActions) != 1 || created.NextActions[0].Kind != "record_hypothesis" {
		t.Fatalf("created next actions = %+v", created.NextActions)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "result", Title: "illegal produced", FromNodeID: created.Study.Nodes[0].ID, Relation: "produced",
		},
	}); err == nil {
		t.Fatal("accepted produced from a question node")
	}

	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "hypothesis", Title: "Cleaner traversal raises accuracy", Summary: "Background points currently enter the local neighborhood.",
			FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsActionKind(hypothesis.NextActions, "record_hypothesis") || !containsActionKind(hypothesis.NextActions, "record_observation") {
		t.Fatalf("thin graph next actions = %+v", hypothesis.NextActions)
	}
	observation, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "observation", Title: "README reports 86.4 on OBJ-BG", Summary: "Paper table lists overall accuracy 86.4.",
			FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(observation.Study.Edges, hypothesis.Study.Nodes[1].ID, observation.Study.Nodes[2].ID, "leads_to") {
		t.Fatalf("observation was not hung off the hypothesis: %+v", observation.Study.Edges)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "observation", Title: "Illegal question evidence", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	}); err == nil {
		t.Fatal("accepted observation hanging off the question")
	}
	orphan, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "observation", Title: "Unlinked table row", Summary: "Created without an edge first."},
	})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			ID: orphan.Study.Nodes[len(orphan.Study.Nodes)-1].ID, Kind: "observation",
			Title: "Unlinked table row", Summary: "Now attached to the hypothesis.",
			FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(linked.Study.Edges, hypothesis.Study.Nodes[1].ID, orphan.Study.Nodes[len(orphan.Study.Nodes)-1].ID, "leads_to") {
		t.Fatalf("existing observation was not attached: %+v", linked.Study.Edges)
	}
	run, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "run", Title: "OBJ-BG smoke", Status: "succeeded",
			ExperimentID: experimentRecord.PublicID.String(), FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	runID := nodeIDByExperiment(run.Study.Nodes, experimentRecord.PublicID.String())
	if runID == "" {
		t.Fatalf("run node missing: %+v", run.Study.Nodes)
	}
	metric := 86.4
	updated, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "result", Title: "OBJ-BG smoke accuracy", Summary: "The existing smoke Experiment reached 86.4 overall accuracy.",
			Status: "succeeded", MetricName: "overall_accuracy", MetricValue: &metric,
			FromNodeID: runID, Relation: "produced",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Study == nil || len(updated.Study.Nodes) != 6 || len(updated.Study.Edges) != 5 || nodeIDByExperiment(updated.Study.Nodes, experimentRecord.PublicID.String()) == "" {
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
	if audits, _ := client.AuditEvent.Query().Count(ctx); audits != 8 {
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
	if err != nil || workspace.Study == nil || workspace.Study.Name != "second" || len(workspace.Studies) != 2 {
		t.Fatalf("AgentWorkspace() = %+v, %v", workspace, err)
	}
	owner, err := service.OwnerWorkspace(ctx, tenant.ID, project.PublicID.String(), "")
	if err != nil || owner.Study == nil || owner.Study.Name != "second" {
		t.Fatalf("OwnerWorkspace() = %+v, %v", owner, err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Plan: &PlanInput{Goal: "Need an explicit Study", NextAction: "Choose first or second before replacing the plan."},
	}); !errors.Is(err, ErrChoice) {
		t.Fatalf("AgentUpdate() without study selector error = %v", err)
	}
}

func TestCloseRunRequiresTerminalExperimentAndWritesProducedResult(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-close-run?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("close-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	queued, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	finished, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("succeeded").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
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
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Background noise caps accuracy", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindPreparedRun(ctx, principal, hypothesis.Study.Nodes[1].ID, queued.PublicID.String(), "queued smoke"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AgentCloseRun(ctx, principal, CloseRunInput{ExperimentID: queued.PublicID.String(), Title: "too early"}); err == nil {
		t.Fatal("closed a queued Experiment")
	}
	runID, err := service.BindPreparedRun(ctx, principal, hypothesis.Study.Nodes[1].ID, finished.PublicID.String(), "finished smoke")
	if err != nil || runID == "" {
		t.Fatalf("BindPreparedRun() = %q, %v", runID, err)
	}
	metric := 86.4
	closed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "OBJ-BG smoke accuracy",
		Summary: "The smoke Experiment reached 86.4 overall accuracy.", MetricName: "overall_accuracy", MetricValue: &metric,
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Study == nil || len(closed.Study.Nodes) != 5 || len(closed.Study.Edges) != 4 {
		t.Fatalf("closed graph = %+v", closed.Study)
	}
	again, err := service.AgentCloseRun(ctx, principal, CloseRunInput{ExperimentID: finished.PublicID.String(), Title: "OBJ-BG smoke accuracy"})
	if err != nil || len(again.Study.Nodes) != 5 {
		t.Fatalf("idempotent close_run = %+v, %v", again.Study, err)
	}
	actions, err := service.AgentNextActions(ctx, principal, WorkspaceInput{})
	if err != nil || len(actions.Actions) == 0 || actions.Actions[0].Kind == "close_run" {
		t.Fatalf("next actions after close = %+v, %v", actions, err)
	}
}

func TestCreateStudyBindsExistingRepository(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-import-repo?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	otherProject, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Other").SetSlug("other").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("import-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("dynamic-point-mamba").SetSSHURL("git@github.com:research/dynamic-point-mamba.git").SetSSHHost("github.com").SetDefaultBranch("main").SetStatus("active").Save(ctx)
	foreign, _ := client.Repository.Create().SetProjectID(otherProject.ID).SetName("foreign").SetSSHURL("git@github.com:research/foreign.git").SetSSHHost("github.com").SetDefaultBranch("main").SetStatus("active").Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)

	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{
			Name: "dynamic-point-mamba", Question: "What should this Study learn from dynamic-point-mamba?",
			RepositoryID: repository.PublicID.String(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Study == nil || created.Study.Repository == nil {
		t.Fatalf("created study missing repository = %+v", created.Study)
	}
	if created.Study.Repository.ID != repository.PublicID.String() || created.Study.Repository.Name != "dynamic-point-mamba" || created.Study.Repository.Status != "active" {
		t.Fatalf("bound repository = %+v", created.Study.Repository)
	}

	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "cross", Question: "Should a foreign repository bind here?", RepositoryID: foreign.PublicID.String()},
	}); err == nil {
		t.Fatal("accepted a cross-Project repository")
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "bad-id", Question: "Should a garbage repository ID fail?", RepositoryID: "not-a-uuid"},
	}); err == nil {
		t.Fatal("accepted an invalid repository_id")
	}
}

func containsActionKind(actions []NextAction, kind string) bool {
	for _, action := range actions {
		if action.Kind == kind {
			return true
		}
	}
	return false
}

func hasEdge(edges []EdgeView, fromID, toID, relation string) bool {
	for _, edge := range edges {
		if edge.FromID == fromID && edge.ToID == toID && edge.Relation == relation {
			return true
		}
	}
	return false
}

func nodeIDByExperiment(nodes []NodeView, experimentID string) string {
	for _, node := range nodes {
		if node.ExperimentID == experimentID && node.Kind == "run" {
			return node.ID
		}
	}
	return ""
}
