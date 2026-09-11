package research

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/researchnode"
	"github.com/XR-Lee/Gemcp/ent/study"
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
		SetMetrics(map[string]any{"overall_accuracy": 86.4}).
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
	if !containsActionKind(hypothesis.NextActions, "prepare_experiment") {
		t.Fatalf("hypothesis next actions = %+v", hypothesis.NextActions)
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
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "result", Title: "OBJ-BG smoke accuracy", Summary: "The existing smoke Experiment reached 86.4 overall accuracy.",
			Status: "succeeded", MetricName: "overall_accuracy", MetricValue: &metric,
			FromNodeID: runID, Relation: "produced",
		},
	}); err == nil {
		t.Fatal("accepted a result outside close_run")
	}
	finishedAt := time.Now().UTC()
	experimentRecord, err = experimentRecord.Update().SetState("succeeded").SetFinishedAt(finishedAt).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: experimentRecord.PublicID.String(), Title: "OBJ-BG smoke accuracy",
		Summary: "The existing smoke Experiment reached 86.4 overall accuracy.",
		Status:  "succeeded", MetricName: "overall_accuracy", MetricValue: &metric,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Study == nil || len(updated.Study.Nodes) != 7 || len(updated.Study.Edges) != 8 || nodeIDByExperiment(updated.Study.Nodes, experimentRecord.PublicID.String()) == "" {
		t.Fatalf("updated graph = %+v", updated.Study)
	}
	if !hasHighlightOnHypothesis(updated.Study, hypothesis.Study.Nodes[1].ID, "OBJ-BG smoke accuracy") {
		t.Fatalf("missing highlight observation on hypothesis: %+v", updated.Study)
	}
	if len(updated.Study.Hypotheses) != 1 || len(updated.Study.Hypotheses[0].Experiments) != 1 {
		t.Fatalf("hypothesis records = %+v", updated.Study.Hypotheses)
	}
	if rec := updated.Study.Hypotheses[0].Experiments[0]; rec.Branch != "main" || rec.CommitSHA == "" || rec.HighlightTitle == "" {
		t.Fatalf("hypothesis experiment record = %+v", rec)
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
	if audits, _ := client.AuditEvent.Query().Count(ctx); audits != 9 {
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
		SetMetrics(map[string]any{"overall_accuracy": 86.4, "loss": 0.2}).
		Save(ctx)
	_, _ = client.ExperimentProposal.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
		SetRepositoryID(repository.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).
		SetExperimentID(finished.ID).SetStatus("submitted").SetRequestedRef("autoresearch/objbg-baseline").
		SetCommitSha(finished.CommitSha).SetExecutionMode("argv").SetArgv([]string{"python", "train.py"}).
		SetDisplayCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).
		SetTerminationGraceSeconds(30).SetProjectSnapshot(map[string]any{"expected_metric": "overall_accuracy"}).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetChecks([]map[string]any{}).
		SetReservedCostMilli(0).SetConfirmationDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").
		SetExpiresAt(time.Now().UTC().Add(time.Hour)).Save(ctx)
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
	if _, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "invalid result evidence", ResultCommitSHA: "abc1234",
	}); err == nil {
		t.Fatal("accepted a shortened result_commit_sha")
	}
	metric := 86.4
	wrongMetric := 86.3
	if _, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "mismatched result metric",
		MetricName: "overall_accuracy", MetricValue: &wrongMetric,
	}); err == nil {
		t.Fatal("accepted a metric that did not match the terminal Experiment")
	}
	loss := 0.2
	if _, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "wrong expected metric",
		MetricName: "loss", MetricValue: &loss,
	}); err == nil {
		t.Fatal("accepted a succeeded result without the prepared expected_metric")
	}
	resultCommitSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	closed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "OBJ-BG smoke accuracy",
		Summary: "The smoke Experiment reached 86.4 overall accuracy.", MetricName: "overall_accuracy", MetricValue: &metric,
		ResultCommitSHA: resultCommitSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Study == nil || len(closed.Study.Nodes) != 6 || len(closed.Study.Edges) != 7 {
		t.Fatalf("closed graph = %+v", closed.Study)
	}
	result := nodeByKind(closed.Study.Nodes, "result")
	if result.Kind != "result" || result.CommitSHA != resultCommitSHA {
		t.Fatalf("result commit = %+v", result)
	}
	if result.Branch != "autoresearch/objbg-baseline" {
		t.Fatalf("result branch = %q", result.Branch)
	}
	if result.ExperimentID != finished.PublicID.String() {
		t.Fatalf("result experiment = %q", result.ExperimentID)
	}
	if rec := closed.Study.Hypotheses[0]; rec.Branch != "autoresearch/objbg-baseline" {
		t.Fatalf("hypothesis header branch = %+v", rec)
	}
	if node := nodeByKind(closed.Study.Nodes, "hypothesis"); node.Branch != "autoresearch/objbg-baseline" {
		t.Fatalf("hypothesis node branch = %+v", node)
	}
	for _, node := range closed.Study.Nodes {
		if node.Kind == "run" && node.ExperimentID == finished.PublicID.String() && node.Branch != "autoresearch/objbg-baseline" {
			t.Fatalf("finished run branch = %+v", node)
		}
	}
	if !hasHighlightOnHypothesis(closed.Study, hypothesis.Study.Nodes[1].ID, "OBJ-BG smoke accuracy") {
		t.Fatalf("missing highlight observation: %+v", closed.Study)
	}
	again, err := service.AgentCloseRun(ctx, principal, CloseRunInput{ExperimentID: finished.PublicID.String(), Title: "OBJ-BG smoke accuracy"})
	if err != nil || len(again.Study.Nodes) != 6 {
		t.Fatalf("idempotent close_run = %+v, %v", again.Study, err)
	}
	actions, err := service.AgentNextActions(ctx, principal, WorkspaceInput{})
	if err != nil || len(actions.Actions) == 0 || actions.Actions[0].Kind == "close_run" {
		t.Fatalf("next actions after close = %+v, %v", actions, err)
	}
	failed, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("failed").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	if _, err := service.BindPreparedRun(ctx, principal, hypothesis.Study.Nodes[1].ID, failed.PublicID.String(), "failed smoke"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: failed.PublicID.String(), Title: "false success", Status: "succeeded",
	}); err == nil {
		t.Fatal("recorded a failed Experiment as a succeeded result")
	}
	failedClosed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: failed.PublicID.String(), Title: "failed smoke result", Summary: "The workload failed before reporting its target metric.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := nodeByKind(failedClosed.Study.Nodes, "result"); result.Kind != "result" || result.Status != "failed" {
		t.Fatalf("failed result = %+v", result)
	}
	if !hasHighlightOnHypothesis(failedClosed.Study, hypothesis.Study.Nodes[1].ID, "failed smoke result") {
		t.Fatalf("failed highlight missing: %+v", failedClosed.Study)
	}
}

func TestCloseRunCopiesExpectedMetricWhenOmitted(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-close-run-copy-metric?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("copy-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	finished, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("succeeded").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		SetMetrics(map[string]any{"overall_accuracy": 86.4, "loss": 0.2}).
		Save(ctx)
	_, _ = client.ExperimentProposal.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
		SetRepositoryID(repository.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).
		SetExperimentID(finished.ID).SetStatus("submitted").SetRequestedRef("main").
		SetCommitSha(finished.CommitSha).SetExecutionMode("argv").SetArgv([]string{"python", "train.py"}).
		SetDisplayCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).
		SetTerminationGraceSeconds(30).SetProjectSnapshot(map[string]any{"expected_metric": "overall_accuracy"}).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetChecks([]map[string]any{}).
		SetReservedCostMilli(0).SetConfirmationDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").
		SetExpiresAt(time.Now().UTC().Add(time.Hour)).Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "copy-metric", Question: "Can close_run copy the expected metric from the Experiment?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Copy the recorded scalar", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindPreparedRun(ctx, principal, hypothesis.Study.Nodes[1].ID, finished.PublicID.String(), "finished smoke"); err != nil {
		t.Fatal(err)
	}
	closed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "copied metric result",
		Summary: "The terminal Experiment already recorded overall_accuracy.",
	})
	if err != nil {
		t.Fatal(err)
	}
	result := nodeByKind(closed.Study.Nodes, "result")
	if result.Kind != "result" || result.MetricName != "overall_accuracy" || result.MetricValue == nil || *result.MetricValue != 86.4 {
		t.Fatalf("copied result = %+v", result)
	}
	if !hasHighlightOnHypothesis(closed.Study, hypothesis.Study.Nodes[1].ID, "copied metric result") {
		t.Fatalf("copied highlight missing: %+v", closed.Study)
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

func TestGraphNodeStoresEvidenceTimeFromCommit(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-evidence-time?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("lab-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
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
		Node: &NodeInput{
			Kind: "hypothesis", Title: "Cleaner traversal raises accuracy",
			FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to",
			OccurredAt: "2024-03-12", CommitSHA: "a1b2c3d",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{
			Kind: "observation", Title: "README reports 86.4 on OBJ-BG", Summary: "Paper table lists overall accuracy 86.4.",
			FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to",
			OccurredAt: "2024-11-02T18:04:00Z", CommitSHA: "0123456789abcdef",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	node := observation.Study.Nodes[2]
	if node.OccurredAt == nil || !node.OccurredAt.Equal(time.Date(2024, 11, 2, 18, 4, 0, 0, time.UTC)) {
		t.Fatalf("observation occurred_at = %v", node.OccurredAt)
	}
	if node.CommitSHA != "0123456789abcdef" {
		t.Fatalf("observation commit_sha = %q", node.CommitSHA)
	}
	if node.CreatedAt.Equal(*node.OccurredAt) {
		t.Fatal("occurred_at should not fall back to MCP write time")
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "observation", Title: "Future stamp", FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to", OccurredAt: "2099-01-01"},
	}); err == nil {
		t.Fatal("accepted a future occurred_at")
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "observation", Title: "Bad sha", FromNodeID: hypothesis.Study.Nodes[1].ID, Relation: "leads_to", CommitSHA: "not-a-sha"},
	}); err == nil {
		t.Fatal("accepted a non-hex commit_sha")
	}
}

func TestBindPreparedRunAndPrepareRejectIsolatedOrigin(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-isolated-origin?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("iso-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
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
		Study: &StudyInput{Name: "isolated", Question: "Should an isolated hypothesis be allowed to spend?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	isolated, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Parentless claim"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindPreparedRun(ctx, principal, "", "not-an-experiment", "no-op"); err == nil {
		t.Fatal("BindPreparedRun silently accepted an empty origin")
	}
	isolatedID := findNodeID(isolated.Study.Nodes, "hypothesis", "Parentless claim")
	if isolatedID == "" {
		t.Fatalf("isolated hypothesis missing: %+v", isolated.Study.Nodes)
	}
	if _, err := service.BindPreparedRun(ctx, principal, isolatedID, experimentRecord.PublicID.String(), "isolated"); err == nil || !strings.Contains(err.Error(), "isolated nodes cannot prepare") {
		t.Fatalf("isolated hypothesis bind error = %v", err)
	}
	if err := service.ValidatePreparedBind(ctx, principal, isolatedID); err == nil || !strings.Contains(err.Error(), "isolated nodes cannot prepare") {
		t.Fatalf("isolated hypothesis pre-flight error = %v", err)
	}
	// A plan hanging directly off the question is connected but hypothesis-less:
	// it could pay for a run that close_run can never link to a highlight.
	planned, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "plan", Title: "Plan under question", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	planID := findNodeID(planned.Study.Nodes, "plan", "Plan under question")
	if planID == "" {
		t.Fatalf("plan missing: %+v", planned.Study.Nodes)
	}
	if _, err := service.BindPreparedRun(ctx, principal, planID, experimentRecord.PublicID.String(), "plan run"); err == nil || !strings.Contains(err.Error(), "trace back to a hypothesis") {
		t.Fatalf("hypothesis-less plan bind error = %v", err)
	}
	if err := service.ValidatePreparedBind(ctx, principal, planID); err == nil || !strings.Contains(err.Error(), "trace back to a hypothesis") {
		t.Fatalf("hypothesis-less plan pre-flight error = %v", err)
	}
	// A proposal prepared before the Study existed must be re-prepared once a
	// Study is active instead of submitting off-graph.
	if err := service.ValidatePreparedBind(ctx, principal, ""); err == nil || !strings.Contains(err.Error(), "prepared before the Study existed") {
		t.Fatalf("pre-Study proposal pre-flight error = %v", err)
	}
	// A connected hypothesis passes the same pre-flight.
	connected, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Connected claim", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	connectedID := findNodeID(connected.Study.Nodes, "hypothesis", "Connected claim")
	if err := service.ValidatePreparedBind(ctx, principal, connectedID); err != nil {
		t.Fatalf("connected hypothesis pre-flight = %v", err)
	}
}

func TestCloseRunDegradesForLegacyRunWithoutHypothesis(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-legacy-close?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("legacy-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	finishedAt := time.Now().UTC()
	experimentRecord, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("succeeded").SetFinishedAt(finishedAt).SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "legacy", Question: "Can a pre-contract run still close?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	studyRecord, err := client.Study.Query().Where(study.ProjectIDEQ(project.ID)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a run bound before the hypothesis contract: it hangs directly
	// off the question, which the bind path no longer allows.
	questionRecord, err := client.ResearchNode.Query().Where(
		researchnode.StudyIDEQ(studyRecord.ID), researchnode.KindEQ(researchnode.KindQuestion),
	).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	legacyRun, err := client.ResearchNode.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetStudyID(studyRecord.ID).
		SetKind(researchnode.KindRun).SetTitle("legacy run").SetStatus(researchnode.StatusRunning).
		SetExperimentID(experimentRecord.ID).SetAgentTokenID(token.ID).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResearchEdge.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetStudyID(studyRecord.ID).
		SetFromNodeID(questionRecord.ID).SetToNodeID(legacyRun.ID).SetRelation("leads_to").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	closed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: experimentRecord.PublicID.String(), Title: "legacy result", Status: "succeeded",
	})
	if err != nil {
		t.Fatalf("legacy close_run must degrade, not fail: %v", err)
	}
	if closed.Warning == "" || !strings.Contains(closed.Warning, "no highlight observation") {
		t.Fatalf("legacy close warning = %q", closed.Warning)
	}
	if result := nodeByKind(closed.Study.Nodes, "result"); result.Kind != "result" {
		t.Fatalf("legacy close must still write the result: %+v", closed.Study.Nodes)
	}
	for _, node := range closed.Study.Nodes {
		if node.Kind == "observation" {
			t.Fatalf("legacy close must not invent a highlight: %+v", node)
		}
	}
	_ = created
}

func TestNextActionsFollowHypothesisEvidence(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-next-actions?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("next-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	finished, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("succeeded").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		SetMetrics(map[string]any{"overall_accuracy": 86.4}).
		Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "next", Question: "What should get_next_actions propose after a closed run?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Noise caps accuracy", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsActionKind(hypothesis.NextActions, "prepare_experiment") || hypothesis.NextActions[0].FromNodeID != hypothesis.Study.Nodes[1].ID {
		t.Fatalf("fresh hypothesis should propose an experiment: %+v", hypothesis.NextActions)
	}
	if _, err := service.BindPreparedRun(ctx, principal, hypothesis.Study.Nodes[1].ID, finished.PublicID.String(), "finished smoke"); err != nil {
		t.Fatal(err)
	}
	metric := 86.4
	closed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "smoke accuracy", Highlight: "Background noise still enters kNN",
		MetricName: "overall_accuracy", MetricValue: &metric,
	})
	if err != nil {
		t.Fatal(err)
	}
	decisionIndex, prepareIndex := -1, -1
	for i, action := range closed.NextActions {
		if action.Kind == "record_decision" && decisionIndex == -1 {
			decisionIndex = i
		}
		if action.Kind == "prepare_experiment" && prepareIndex == -1 {
			prepareIndex = i
		}
	}
	if decisionIndex == -1 {
		t.Fatalf("closed run should propose a decision: %+v", closed.NextActions)
	}
	if prepareIndex != -1 && prepareIndex < decisionIndex {
		t.Fatalf("decision should outrank a new proposal: %+v", closed.NextActions)
	}

	// After the decision is recorded and no follow-up hypothesis exists yet,
	// the next Experiment is proposed from that same hypothesis.
	hypothesisID := hypothesis.Study.Nodes[1].ID
	resultID := nodeByKind(closed.Study.Nodes, "result").ID
	decided, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "decision", Title: "Iterate on a cleaner kNN", FromNodeID: resultID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	foundDecidedPrepare := false
	for _, action := range decided.NextActions {
		if action.Kind == "prepare_experiment" && action.FromNodeID == hypothesisID {
			foundDecidedPrepare = true
		}
	}
	if !foundDecidedPrepare {
		t.Fatalf("decided hypothesis should propose the next Experiment from itself: %+v", decided.NextActions)
	}

	// Chained hypotheses: evidence past a follow-up hypothesis belongs to that
	// hypothesis only, in both next actions and the Owner records.
	decisionID := findNodeID(decided.Study.Nodes, "decision", "Iterate on a cleaner kNN")
	followUp, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Cleaner kNN raises accuracy", FromNodeID: decisionID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	followUpID := findNodeID(followUp.Study.Nodes, "hypothesis", "Cleaner kNN raises accuracy")
	queued, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").
		SetCommand("python train.py --clean-knn").SetState("running").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	if _, err := service.BindPreparedRun(ctx, principal, followUpID, queued.PublicID.String(), "follow-up run"); err != nil {
		t.Fatal(err)
	}
	workspace, err := service.AgentWorkspace(ctx, principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	waitActions := 0
	for _, action := range workspace.NextActions {
		if action.Kind == "wait_run" && action.ExperimentID == queued.PublicID.String() {
			waitActions++
		}
	}
	if waitActions != 1 {
		t.Fatalf("follow-up run must be waited on exactly once, got %d: %+v", waitActions, workspace.NextActions)
	}
	for _, record := range workspace.Study.Hypotheses {
		for _, experiment := range record.Experiments {
			if experiment.ExperimentID == queued.PublicID.String() && record.ID != followUpID {
				t.Fatalf("follow-up run attributed to ancestor hypothesis %q: %+v", record.Title, workspace.Study.Hypotheses)
			}
		}
	}
}

func TestCloseRunSucceedsOnFullGraph(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-full-graph?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("full-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	finishedAt := time.Now().UTC()
	experimentRecord, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("succeeded").SetFinishedAt(finishedAt).SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "full", Question: "Can a funded run close on a full Graph?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "hypothesis", Title: "Noise caps accuracy", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesisID := hypothesis.Study.Nodes[1].ID
	if _, err := service.BindPreparedRun(ctx, principal, hypothesisID, experimentRecord.PublicID.String(), "funded run"); err != nil {
		t.Fatal(err)
	}
	// Fill the Study to the node cap with plain observations.
	studyRecord, err := client.Study.Query().Where(study.ProjectIDEQ(project.ID)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		count, countErr := client.ResearchNode.Query().Where(researchnode.StudyIDEQ(studyRecord.ID)).Count(ctx)
		if countErr != nil {
			t.Fatal(countErr)
		}
		if count >= maxNodesPerStudy {
			break
		}
		if _, err := client.ResearchNode.Create().
			SetTenantID(tenant.ID).SetProjectID(project.ID).SetStudyID(studyRecord.ID).
			SetKind(researchnode.KindObservation).SetTitle("filler observation").SetStatus(researchnode.StatusOpen).
			Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{Kind: "observation", Title: "One too many", FromNodeID: hypothesisID, Relation: "leads_to"},
	}); !errors.Is(err, ErrNodeLimit) {
		t.Fatalf("full graph must reject ordinary writes, got %v", err)
	}
	if err := service.ValidatePreparedBind(ctx, principal, hypothesisID); !errors.Is(err, ErrNodeLimit) {
		t.Fatalf("full graph must reject new spending, got %v", err)
	}
	closed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: experimentRecord.PublicID.String(), Title: "full-graph result", Status: "succeeded",
	})
	if err != nil {
		t.Fatalf("close_run must stay possible on a full Graph: %v", err)
	}
	if closed.Warning != "" {
		t.Fatalf("funded close warning = %q", closed.Warning)
	}
	if !hasHighlightOnHypothesis(closed.Study, hypothesisID, "full-graph result") {
		t.Fatalf("full-graph close must still write the highlight: %d nodes", len(closed.Study.Nodes))
	}
}

func findNodeID(nodes []NodeView, kind, title string) string {
	for _, node := range nodes {
		if node.Kind == kind && node.Title == title {
			return node.ID
		}
	}
	return ""
}

func containsActionKind(actions []NextAction, kind string) bool {
	for _, action := range actions {
		if action.Kind == kind {
			return true
		}
	}
	return false
}

func nodeByKind(nodes []NodeView, kind string) NodeView {
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i].Kind == kind {
			return nodes[i]
		}
	}
	return NodeView{}
}

func hasHighlightOnHypothesis(view *StudyView, hypothesisID, title string) bool {
	if view == nil {
		return false
	}
	for _, node := range view.Nodes {
		if node.Kind == "observation" && node.Title == title &&
			hasEdge(view.Edges, hypothesisID, node.ID, "leads_to") &&
			(hasEdge(view.Edges, node.ID, hypothesisID, "supports") || hasEdge(view.Edges, node.ID, hypothesisID, "contradicts")) {
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
