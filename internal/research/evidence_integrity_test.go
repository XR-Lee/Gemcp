package research

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
)

type evidenceFixture struct {
	ctx          context.Context
	client       *ent.Client
	service      *Service
	principal    agentauth.Principal
	hypothesisID string
	newRun       func(state string, metrics map[string]any) string
	newBareRun   func(state string, metrics map[string]any) string
}

func newEvidenceFixture(t *testing.T, name string) evidenceFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+name+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_evd").SetTokenHash([]byte(name)).SetScopes([]string{"read", "submit"}).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "evidence", Question: "Does the augmentation beat the 0.80 baseline accuracy?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{Node: &NodeInput{
		Kind: "hypothesis", Title: "Augmentation beats the 0.80 baseline",
		Summary: "Expect overall_accuracy above 0.80 with augmentation on.", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	hypothesisID := hypothesis.Study.Nodes[1].ID
	runs := 0
	newRunWith := func(state string, metrics map[string]any, expectedMetric string) string {
		t.Helper()
		runs++
		create := client.Experiment.Create().
			SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
			SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
			SetCommand("python train.py").SetState(state).SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
			SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
			SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0)
		if metrics != nil {
			create.SetMetrics(metrics)
		}
		record, err := create.Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if expectedMetric != "" {
			if _, err := client.ExperimentProposal.Create().
				SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
				SetRepositoryID(repository.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).
				SetExperimentID(record.ID).SetStatus("submitted").SetRequestedRef("autoresearch/augment").
				SetCommitSha(record.CommitSha).SetExecutionMode("argv").SetArgv([]string{"python", "train.py"}).
				SetDisplayCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).
				SetTerminationGraceSeconds(30).SetProjectSnapshot(map[string]any{"expected_metric": expectedMetric}).
				SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
				SetResourceSnapshot(map[string]any{"name": "default"}).SetChecks([]map[string]any{}).
				SetReservedCostMilli(0).SetConfirmationDigest(fmt.Sprintf("sha256:%064d", record.ID)).
				SetExpiresAt(time.Now().UTC().Add(time.Hour)).Save(ctx); err != nil {
				t.Fatal(err)
			}
		}
		experimentID := record.PublicID.String()
		if _, err := service.BindPreparedRun(ctx, principal, hypothesisID, experimentID, fmt.Sprintf("augmentation run %d", runs)); err != nil {
			t.Fatal(err)
		}
		return experimentID
	}
	newRun := func(state string, metrics map[string]any) string {
		return newRunWith(state, metrics, "overall_accuracy")
	}
	newBareRun := func(state string, metrics map[string]any) string { return newRunWith(state, metrics, "") }
	return evidenceFixture{ctx: ctx, client: client, service: service, principal: principal, hypothesisID: hypothesisID, newRun: newRun, newBareRun: newBareRun}
}

func runRecord(t *testing.T, view *StudyView, experimentID string) HypothesisRunRecord {
	t.Helper()
	for _, hypothesis := range view.Hypotheses {
		for _, record := range hypothesis.Experiments {
			if record.ExperimentID == experimentID {
				return record
			}
		}
	}
	t.Fatalf("no hypothesis record for %s", experimentID)
	return HypothesisRunRecord{}
}

func nodeByTitle(t *testing.T, view *StudyView, kind, title string) NodeView {
	t.Helper()
	for _, node := range view.Nodes {
		if node.Kind == kind && node.Title == title {
			return node
		}
	}
	t.Fatalf("no %s node titled %q", kind, title)
	return NodeView{}
}

func TestCloseRunVerdictIsAJudgmentNotTheExecutionState(t *testing.T) {
	f := newEvidenceFixture(t, "evidence-verdict")

	// A run that finished but missed the baseline is not evidence for the claim.
	weak := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.12})
	closed, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: weak, Title: "weak augmentation accuracy"})
	if err != nil {
		t.Fatal(err)
	}
	if got := verdictEdge(closed.Study, f.hypothesisID, "weak augmentation accuracy"); got != "" {
		t.Fatalf("a finished run without a verdict wrote %q", got)
	}
	if record := runRecord(t, closed.Study, weak); record.Verdict != "inconclusive" || record.MetricSource != "experiment" ||
		record.MetricName != "overall_accuracy" || record.MetricValue == nil || *record.MetricValue != 0.12 {
		t.Fatalf("weak run record = %+v", record)
	}

	// A crash says nothing about the claim, so it cannot carry a verdict.
	crashed := f.newRun("failed", nil)
	if _, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: crashed, Title: "augmentation OOM", Verdict: "contradicts"}); err == nil {
		t.Fatal("a failed Experiment was recorded as contradicting the hypothesis")
	}
	closed, err = f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: crashed, Title: "augmentation OOM"})
	if err != nil {
		t.Fatal(err)
	}
	if got := verdictEdge(closed.Study, f.hypothesisID, "augmentation OOM"); got != "" {
		t.Fatalf("a crashed run wrote verdict %q", got)
	}
	if record := runRecord(t, closed.Study, crashed); record.Verdict != "inconclusive" {
		t.Fatalf("crashed run verdict = %q", record.Verdict)
	}
	var decision NextAction
	for _, action := range closed.NextActions {
		if action.Kind == "record_decision" {
			decision = action
		}
	}
	if !strings.Contains(decision.Detail, "did not produce valid evidence") || strings.Contains(decision.Detail, "Record whether it supports or contradicts") {
		t.Fatalf("crash decision detail = %q", decision.Detail)
	}

	// A verdict needs a number behind it.
	noMetric := f.newRun("succeeded", map[string]any{"loss": 0.4})
	if _, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: noMetric, Title: "no accuracy", Status: "failed", Verdict: "supports"}); err == nil {
		t.Fatal("accepted a verdict on a run closed as failed")
	}

	bare := f.newBareRun("succeeded", map[string]any{"overall_accuracy": 0.9})
	if _, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: bare, Title: "unmeasured claim", Verdict: "supports"}); err == nil ||
		!strings.Contains(err.Error(), "needs a recorded metric") {
		t.Fatalf("accepted a verdict without a metric: %v", err)
	}

	// An explicit verdict on a measured metric is recorded, and a named metric
	// is copied from the Experiment instead of typed by hand.
	strong := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.91})
	if _, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: strong, Title: "bad verdict", Verdict: "maybe"}); err == nil {
		t.Fatal("accepted an unknown verdict")
	}
	closed, err = f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{
		ExperimentID: strong, Title: "augmentation reaches 0.91", MetricName: "overall_accuracy", Verdict: "supports",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := verdictEdge(closed.Study, f.hypothesisID, "augmentation reaches 0.91"); got != "supports" {
		t.Fatalf("explicit verdict edge = %q", got)
	}
	if record := runRecord(t, closed.Study, strong); record.Verdict != "supports" || record.MetricValue == nil || *record.MetricValue != 0.91 {
		t.Fatalf("strong run record = %+v", record)
	}
}

func TestGraphUpdatesCannotRewriteExperimentEvidence(t *testing.T) {
	f := newEvidenceFixture(t, "evidence-lock")
	experimentID := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.12})
	crashedID := f.newRun("failed", nil)
	closed, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: experimentID, Title: "augmentation accuracy"})
	if err != nil {
		t.Fatal(err)
	}
	highlight := nodeByTitle(t, closed.Study, "observation", "augmentation accuracy")
	if !highlight.SystemWritten || highlight.MetricSource != "experiment" {
		t.Fatalf("highlight provenance = %+v", highlight)
	}
	forged := 0.99
	if _, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		ID: highlight.ID, Kind: "observation", Title: "augmentation reaches 99%", MetricName: "overall_accuracy", MetricValue: &forged,
	}}); err == nil || !strings.Contains(err.Error(), "cannot be edited") {
		t.Fatalf("rewrote the close_run highlight: %v", err)
	}
	if _, err := f.service.OwnerUpdate(f.ctx, f.principal.TenantID, "owner", f.principal.ProjectPublicID, UpdateInput{Node: &NodeInput{
		ID: highlight.ID, Kind: "observation", Status: "failed",
	}}); err == nil {
		t.Fatal("Owner Graph update rewrote Experiment evidence")
	}
	runID := nodeIDByExperiment(closed.Study.Nodes, crashedID)
	if _, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		ID: runID, Kind: "run", Status: "succeeded",
	}}); err == nil {
		t.Fatal("relabeled a failed Experiment run as succeeded")
	}
	// Re-sending the stored title to attach an edge is not an edit.
	if _, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		ID: runID, Kind: "run", Title: "augmentation run 2", FromNodeID: f.hypothesisID, Relation: "leads_to",
	}}); err != nil {
		t.Fatalf("edge-only update on a bound run: %v", err)
	}
	after, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	if node := nodeByTitle(t, after.Study, "observation", "augmentation accuracy"); node.MetricValue == nil || *node.MetricValue != 0.12 {
		t.Fatalf("highlight metric changed: %+v", node)
	}

	// A number typed into the Graph stays usable but is labeled asserted.
	claimed := 0.95
	asserted, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		Kind: "observation", Title: "Paper X reports 0.95 with augmentation", MetricName: "overall_accuracy", MetricValue: &claimed,
		FromNodeID: f.hypothesisID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if node := nodeByTitle(t, asserted.Study, "observation", "Paper X reports 0.95 with augmentation"); node.MetricSource != "asserted" || node.SystemWritten {
		t.Fatalf("typed metric provenance = %+v", node)
	}
}

func TestOnlyCloseRunMarksEvidenceAsVerdict(t *testing.T) {
	f := newEvidenceFixture(t, "evidence-verdict-edge")
	crashedID := f.newRun("failed", nil)
	closed, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: crashedID, Title: "crashed augmentation run"})
	if err != nil {
		t.Fatal(err)
	}
	result := nodeByTitle(t, closed.Study, "result", "crashed augmentation run")
	highlight := nodeByTitle(t, closed.Study, "observation", "crashed augmentation run")
	// An edge-only update would skip the succeeded-with-a-metric check that
	// close_run applies before it writes a verdict.
	for _, from := range []string{result.ID, highlight.ID} {
		for _, relation := range []string{"supports", "contradicts"} {
			if _, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
				ID: f.hypothesisID, Kind: "hypothesis", FromNodeID: from, Relation: relation,
			}}); err == nil || !strings.Contains(err.Error(), "only close_run") {
				t.Fatalf("linked Experiment evidence as %s: %v", relation, err)
			}
		}
	}
	if _, err := f.service.OwnerUpdate(f.ctx, f.principal.TenantID, "owner", f.principal.ProjectPublicID, UpdateInput{Node: &NodeInput{
		ID: f.hypothesisID, Kind: "hypothesis", FromNodeID: result.ID, Relation: "supports",
	}}); err == nil {
		t.Fatal("Owner Graph update marked a crashed result as support")
	}
	// The Agent's own reading still links, as its own observation.
	noted, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		Kind: "observation", Title: "The crash came from the data loader", FromNodeID: result.ID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	note := nodeByTitle(t, noted.Study, "observation", "The crash came from the data loader")
	if _, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		ID: f.hypothesisID, Kind: "hypothesis", FromNodeID: note.ID, Relation: "contradicts",
	}}); err != nil {
		t.Fatalf("linking the Agent's own observation: %v", err)
	}
	after, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	if record := runRecord(t, after.Study, crashedID); record.Verdict != "inconclusive" {
		t.Fatalf("crashed run record = %+v", record)
	}
}

func TestGraphUpdatesKeepOmittedFields(t *testing.T) {
	f := newEvidenceFixture(t, "evidence-partial")
	claimed := 0.95
	observation, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		Kind: "observation", Title: "Paper X reports 0.95", Summary: "Table 2, augmentation row.", Status: "succeeded",
		MetricName: "overall_accuracy", MetricValue: &claimed, FromNodeID: f.hypothesisID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	observationID := nodeByTitle(t, observation.Study, "observation", "Paper X reports 0.95").ID
	// Attach a supports edge by id alone: the hypothesis keeps its claim.
	linked, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		ID: f.hypothesisID, Kind: "hypothesis", FromNodeID: observationID, Relation: "supports",
	}})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis := nodeByTitle(t, linked.Study, "hypothesis", "Augmentation beats the 0.80 baseline")
	if hypothesis.Summary != "Expect overall_accuracy above 0.80 with augmentation on." || hypothesis.Status != "open" {
		t.Fatalf("edge-only update erased the hypothesis: %+v", hypothesis)
	}
	if !hasEdge(linked.Study.Edges, observationID, f.hypothesisID, "supports") {
		t.Fatal("supports edge missing")
	}
	retitled, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		ID: observationID, Kind: "observation", Title: "Paper X reports 0.95 (Table 2)",
	}})
	if err != nil {
		t.Fatal(err)
	}
	node := nodeByTitle(t, retitled.Study, "observation", "Paper X reports 0.95 (Table 2)")
	if node.Summary != "Table 2, augmentation row." || node.Status != "succeeded" || node.MetricValue == nil || *node.MetricValue != 0.95 {
		t.Fatalf("title-only update erased fields: %+v", node)
	}
	if _, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		Kind: "observation", FromNodeID: f.hypothesisID, Relation: "leads_to",
	}}); err == nil {
		t.Fatal("created a node without a title")
	}
}

func TestPlanSyncReadsAsHypothesesWithNumbersAndVerdicts(t *testing.T) {
	f := newEvidenceFixture(t, "evidence-plan-sync")
	weak := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.123456})
	strong := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.91})
	crashed := f.newRun("failed", nil)
	if _, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: weak, Title: "weak augmentation accuracy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: strong, Title: "augmentation reaches 0.91", Verdict: "supports"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{ExperimentID: crashed, Title: "augmentation OOM"}); err != nil {
		t.Fatal(err)
	}
	export, err := f.service.AgentExportPlanSync(f.ctx, f.principal, PlanSyncInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"### Augmentation beats the 0.80 baseline",
		"- augmentation run 1: weak augmentation accuracy, overall_accuracy = 0.1235 (measured). Verdict: inconclusive.",
		"- augmentation run 2: augmentation reaches 0.91, overall_accuracy = 0.91 (measured). Verdict: supports.",
		"- augmentation run 3: augmentation OOM, run failed. Verdict: inconclusive.",
		"- augmentation reaches 0.91 on `autoresearch/augment`: overall_accuracy = 0.91 (measured)",
		"- augmentation OOM on `autoresearch/augment`: run failed, no evidence",
	} {
		if !strings.Contains(export.Markdown, want) {
			t.Fatalf("plan sync missing %q:\n%s", want, export.Markdown)
		}
	}
	if testing.Verbose() {
		t.Log("\n" + export.Markdown)
	}
}

func TestAgentReadingStaysBesideTheCloseRunVerdict(t *testing.T) {
	f := newEvidenceFixture(t, "evidence-agent-reading")
	experimentID := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.88})
	closed, err := f.service.AgentCloseRun(f.ctx, f.principal, CloseRunInput{
		ExperimentID: experimentID, Title: "batch 2^7 seed 1", MetricName: "overall_accuracy", Verdict: "supports",
	})
	if err != nil {
		t.Fatal(err)
	}
	result := nodeByTitle(t, closed.Study, "result", "batch 2^7 seed 1")
	reading := 0.8
	noted, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		Kind: "observation", Title: "Agent reading: 0.88 looks like seed noise",
		Summary: "Own reading; not a measurement.", MetricName: "val_accuracy", MetricValue: &reading,
		FromNodeID: result.ID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	observation := nodeByTitle(t, noted.Study, "observation", "Agent reading: 0.88 looks like seed noise")
	updated, err := f.service.AgentUpdate(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		ID: f.hypothesisID, Kind: "hypothesis", FromNodeID: observation.ID, Relation: "contradicts",
	}})
	if err != nil {
		t.Fatal(err)
	}
	record := runRecord(t, updated.Study, experimentID)
	if record.Verdict != "supports" || record.AgentReading != "contradicts" || record.AgentReadingTitle != observation.Title {
		t.Fatalf("run record = %+v", record)
	}
	export, err := f.service.AgentExportPlanSync(f.ctx, f.principal, PlanSyncInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Verdict: supports.",
		"Agent reading: contradicts. (Agent reading: 0.88 looks like seed noise)",
		"- Observation (contradicts): Agent reading: 0.88 looks like seed noise.",
	} {
		if !strings.Contains(export.Markdown, want) {
			t.Fatalf("plan sync missing %q:\n%s", want, export.Markdown)
		}
	}
}
