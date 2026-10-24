package research

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/ent/researchedge"
	"github.com/XR-Lee/Gemcp/ent/researchnode"
	"github.com/google/uuid"
)

func TestGraphWritesReturnWhatTheyRecorded(t *testing.T) {
	f := newEvidenceFixture(t, "graph-write-receipt")
	before, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}

	// An observation comes back alone, with the edge this call wrote and the
	// hypothesis it hangs off.
	claimed := 0.95
	noted, err := f.service.AgentUpdateView(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		Kind: "observation", Title: "Paper X reports 0.95", MetricName: "overall_accuracy", MetricValue: &claimed,
		FromNodeID: f.hypothesisID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if noted.View != ViewCompact || noted.StudyID != before.Study.ID || noted.Study != nil || noted.Studies != nil {
		t.Fatalf("compact write = %+v", noted)
	}
	if len(noted.Nodes) != 1 || noted.Nodes[0].Kind != "observation" || noted.Nodes[0].MetricSource != "asserted" {
		t.Fatalf("observation write nodes = %+v", noted.Nodes)
	}
	observationID := noted.Nodes[0].ID
	if len(noted.Edges) != 1 || !hasEdge(noted.Edges, f.hypothesisID, observationID, "leads_to") {
		t.Fatalf("observation write edges = %+v", noted.Edges)
	}
	if hypothesis := noted.Hypothesis; hypothesis == nil || hypothesis.ID != f.hypothesisID ||
		hypothesis.Summary != "Expect overall_accuracy above 0.80 with augmentation on." || hypothesis.Run != nil {
		t.Fatalf("observation write hypothesis = %+v", noted.Hypothesis)
	}
	if !containsActionKind(noted.NextActions, "record_decision") {
		t.Fatalf("observation write next actions = %+v", noted.NextActions)
	}

	// An edge-only update by id returns the node and the edge it added, not
	// the edges the node already had. A retry reports the same edge.
	for range 2 {
		linked, err := f.service.AgentUpdateView(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
			ID: f.hypothesisID, Kind: "hypothesis", FromNodeID: observationID, Relation: "supports",
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(linked.Nodes) != 1 || linked.Nodes[0].ID != f.hypothesisID || linked.Hypothesis == nil || linked.Hypothesis.ID != f.hypothesisID {
			t.Fatalf("edge-only write = %+v", linked)
		}
		if len(linked.Edges) != 1 || !hasEdge(linked.Edges, observationID, f.hypothesisID, "supports") {
			t.Fatalf("edge-only write edges = %+v", linked.Edges)
		}
	}

	// close_run returns its result and highlight, the edges that tie them to
	// the run and the hypothesis, and the run's record under a tally of all
	// the hypothesis's runs.
	strong := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.91})
	graph, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	runID := nodeIDByExperiment(graph.Study.Nodes, strong)
	closed, err := f.service.AgentCloseRunView(f.ctx, f.principal, CloseRunInput{
		ExperimentID: strong, Title: "augmentation reaches 0.91", Verdict: "supports",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(closed.Nodes) != 2 || closed.Nodes[0].Kind != "result" || closed.Nodes[1].Kind != "observation" || closed.Warning != "" {
		t.Fatalf("close_run nodes = %+v (warning %q)", closed.Nodes, closed.Warning)
	}
	result, highlight := closed.Nodes[0], closed.Nodes[1]
	if result.ExperimentID != strong || result.MetricSource != "experiment" || !highlight.SystemWritten {
		t.Fatalf("close_run result %+v, highlight %+v", result, highlight)
	}
	if len(closed.Edges) != 4 || !hasEdge(closed.Edges, runID, result.ID, "produced") ||
		!hasEdge(closed.Edges, f.hypothesisID, highlight.ID, "leads_to") || !hasEdge(closed.Edges, result.ID, highlight.ID, "leads_to") ||
		!hasEdge(closed.Edges, highlight.ID, f.hypothesisID, "supports") {
		t.Fatalf("close_run edges = %+v", closed.Edges)
	}
	if run := closed.Hypothesis.Run; run == nil || run.RunNodeID != runID || run.Verdict != "supports" || run.MetricSource != "experiment" ||
		run.MetricValue == nil || *run.MetricValue != 0.91 {
		t.Fatalf("close_run run record = %+v", closed.Hypothesis.Run)
	}
	if closed.Hypothesis.Tally != (HypothesisTally{Runs: 1, Supports: 1}) {
		t.Fatalf("close_run tally = %+v", closed.Hypothesis.Tally)
	}
	retried, err := f.service.AgentCloseRunView(f.ctx, f.principal, CloseRunInput{
		ExperimentID: strong, Title: "augmentation reaches 0.91", Verdict: "supports",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(retried.Nodes) != 2 || retried.Nodes[0].ID != result.ID || retried.Nodes[1].ID != highlight.ID || len(retried.Edges) != 4 {
		t.Fatalf("close_run retry = %+v", retried)
	}

	// A crash closes without a verdict edge and counts as inconclusive; a run
	// still going counts as open.
	crashed := f.newRun("failed", nil)
	f.newRun("running", nil)
	crashClosed, err := f.service.AgentCloseRunView(f.ctx, f.principal, CloseRunInput{ExperimentID: crashed, Title: "augmentation OOM"})
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range crashClosed.Edges {
		if edge.Relation == "supports" || edge.Relation == "contradicts" {
			t.Fatalf("crashed run carried a verdict edge: %+v", crashClosed.Edges)
		}
	}
	if len(crashClosed.Edges) != 3 || crashClosed.Hypothesis.Run == nil || crashClosed.Hypothesis.Run.ExperimentID != crashed ||
		crashClosed.Hypothesis.Run.Verdict != "inconclusive" {
		t.Fatalf("crashed close_run = %+v", crashClosed)
	}
	if crashClosed.Hypothesis.Tally != (HypothesisTally{Runs: 3, Open: 1, Supports: 1, Inconclusive: 1}) {
		t.Fatalf("tally after a crash = %+v", crashClosed.Hypothesis.Tally)
	}

	// A decision on a result belongs to the hypothesis behind that result.
	decided, err := f.service.AgentUpdateView(f.ctx, f.principal, UpdateInput{Node: &NodeInput{
		Kind: "decision", Title: "Keep augmentation", FromNodeID: result.ID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(decided.Nodes) != 1 || decided.Nodes[0].Kind != "decision" || decided.Hypothesis == nil ||
		decided.Hypothesis.ID != f.hypothesisID || decided.Hypothesis.Run != nil {
		t.Fatalf("decision write = %+v", decided)
	}
}

func TestGraphWriteViews(t *testing.T) {
	f := newEvidenceFixture(t, "graph-write-views")

	// A new Study comes back with its question node and the plan it set.
	created, err := f.service.AgentUpdateView(f.ctx, f.principal, UpdateInput{
		Study: &StudyInput{Name: "views", Question: "Does a compact write still show the Owner's plan?"},
		Plan:  &PlanInput{Goal: "Check the write views.", NextAction: "Record a hypothesis."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Nodes) != 1 || created.Nodes[0].Kind != "question" || len(created.Edges) != 0 || created.Hypothesis != nil ||
		created.Plan == nil || created.Plan.NextAction != "Record a hypothesis." || created.StudyID == "" {
		t.Fatalf("created Study write = %+v", created)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	if _, ok := shape["study"]; ok {
		t.Fatalf("compact write sent the whole Study: %s", encoded)
	}
	if _, ok := shape["studies"]; ok {
		t.Fatalf("compact write sent every Study: %s", encoded)
	}
	if edges, ok := shape["edges"].([]any); !ok || len(edges) != 0 {
		t.Fatalf("compact write edges = %#v", shape["edges"])
	}

	// view full adds the whole workspace to the same record of the write.
	full, err := f.service.AgentUpdateView(f.ctx, f.principal, UpdateInput{View: "full", Node: &NodeInput{
		StudyID: created.StudyID, Kind: "hypothesis", Title: "Compact writes keep the plan",
		FromNodeID: created.Nodes[0].ID, Relation: "leads_to",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if full.View != ViewFull || full.Study == nil || full.Study.ID != created.StudyID || len(full.Study.Nodes) != 2 ||
		full.Study.Plan == nil || len(full.Studies) != 2 {
		t.Fatalf("full write workspace = %+v", full)
	}
	if len(full.Nodes) != 1 || full.Nodes[0].Kind != "hypothesis" || len(full.Edges) != 1 || full.Hypothesis == nil ||
		full.Hypothesis.Tally != (HypothesisTally{}) {
		t.Fatalf("full write record = %+v", full)
	}

	// A view that does not exist is refused before anything is written.
	if _, err := f.service.AgentUpdateView(f.ctx, f.principal, UpdateInput{View: "everything", Node: &NodeInput{
		StudyID: created.StudyID, Kind: "observation", Title: "Never written", FromNodeID: full.Nodes[0].ID, Relation: "leads_to",
	}}); err == nil || !strings.Contains(err.Error(), "view must be compact or full") {
		t.Fatalf("unknown view error = %v", err)
	}
	if _, err := f.service.AgentCloseRunView(f.ctx, f.principal, CloseRunInput{
		ExperimentID: f.newRun("succeeded", map[string]any{"overall_accuracy": 0.9}), Title: "never closed", View: "everything",
	}); err == nil || !strings.Contains(err.Error(), "view must be compact or full") {
		t.Fatalf("unknown close_run view error = %v", err)
	}
	after, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{StudyID: created.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Study.Nodes) != 2 {
		t.Fatalf("an unknown view still wrote: %+v", after.Study.Nodes)
	}
	evidence, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result := nodeByKind(evidence.Study.Nodes, "result"); result.Kind != "" {
		t.Fatalf("an unknown view still closed the run: %+v", result)
	}

	readOnly := f.principal
	readOnly.Scopes = []string{"read"}
	if _, err := f.service.AgentUpdateView(f.ctx, readOnly, UpdateInput{Study: &StudyInput{Name: "denied", Question: "Should this be rejected?"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only update error = %v", err)
	}
	if _, err := f.service.AgentCloseRunView(f.ctx, readOnly, CloseRunInput{ExperimentID: uuid.NewString()}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only close_run error = %v", err)
	}
}

// The compact response is the point: it must not grow with the Graph.
func TestCompactGraphWritesStayFlatAsTheGraphGrows(t *testing.T) {
	f := newEvidenceFixture(t, "graph-write-flat")
	var compact, full []int
	for range 8 {
		experimentID := f.newRun("succeeded", map[string]any{"overall_accuracy": 0.5})
		closed, err := f.service.AgentCloseRunView(f.ctx, f.principal, CloseRunInput{ExperimentID: experimentID, Title: "augmentation accuracy"})
		if err != nil {
			t.Fatal(err)
		}
		workspace, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{})
		if err != nil {
			t.Fatal(err)
		}
		compact = append(compact, encodedSize(t, closed))
		full = append(full, encodedSize(t, workspace))
	}
	last := len(compact) - 1
	// Timestamps vary in length; nothing else should.
	if compact[last] > compact[0]+128 {
		t.Fatalf("compact close_run grew with the Graph: %v", compact)
	}
	if full[last] < 2*full[0] {
		t.Fatalf("the Graph barely grew, so this test proves nothing: %v", full)
	}
}

func TestCompactCloseRunKeepsTheLegacyWarning(t *testing.T) {
	f := newEvidenceFixture(t, "graph-write-legacy")
	experimentID := f.newBareRun("succeeded", nil)
	workspace, err := f.service.AgentWorkspace(f.ctx, f.principal, WorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.client.ResearchNode.Query().Where(researchnode.PublicIDEQ(uuid.MustParse(nodeIDByExperiment(workspace.Study.Nodes, experimentID)))).Only(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	question, err := f.client.ResearchNode.Query().Where(researchnode.StudyIDEQ(run.StudyID), researchnode.KindEQ(researchnode.KindQuestion)).Only(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Hang the run off the question, the way runs bound before the
	// hypothesis contract were.
	if _, err := f.client.ResearchEdge.Delete().Where(researchedge.ToNodeIDEQ(run.ID)).Exec(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.ResearchEdge.Create().SetTenantID(run.TenantID).SetProjectID(run.ProjectID).SetStudyID(run.StudyID).
		SetFromNodeID(question.ID).SetToNodeID(run.ID).SetRelation(researchedge.RelationLeadsTo).Save(f.ctx); err != nil {
		t.Fatal(err)
	}
	closed, err := f.service.AgentCloseRunView(f.ctx, f.principal, CloseRunInput{ExperimentID: experimentID, Title: "legacy result", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(closed.Warning, "no highlight observation") || len(closed.Nodes) != 1 || closed.Nodes[0].Kind != "result" ||
		len(closed.Edges) != 1 || closed.Edges[0].Relation != "produced" || closed.Hypothesis != nil {
		t.Fatalf("legacy compact close_run = %+v", closed)
	}
}

func encodedSize(t *testing.T, value any) int {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return len(encoded)
}
