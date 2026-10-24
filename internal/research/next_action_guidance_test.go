package research

import (
	"strings"
	"testing"
)

func TestPausedAndArchivedStudiesDoNotSuggestPrepare(t *testing.T) {
	paused := guidanceStudy("paused")
	paused.Nodes = append(paused.Nodes, NodeView{
		ID: "run", Kind: "run", Title: "Smoke", ExperimentID: "exp-1", ExperimentState: "running",
	})
	paused.Edges = append(paused.Edges, EdgeView{FromID: "h", ToID: "run", Relation: "leads_to"})
	actions := deriveNextActions(paused)
	assertNoKind(t, actions, "prepare_experiment")
	if actions[0].Kind != "wait_run" || actions[0].AllowedRefPattern != "autoresearch/*" {
		t.Fatalf("in-flight run = %+v", actions[0])
	}
	review := actionByKind(t, actions, "review_study")
	if review.Tool != "get_research_workspace" || review.AllowedRefPattern != "" || review.ProtocolBranch != "" || !strings.Contains(review.Detail, "Do not call prepare_experiment") {
		t.Fatalf("review = %+v", review)
	}
	resume := actionByKind(t, actions, "resume_study")
	if resume.Tool != "update_research_workspace" || resume.AllowedRefPattern != "" || !strings.Contains(resume.Detail, "human sets status back to active") {
		t.Fatalf("resume = %+v", resume)
	}
	if actionByKind(t, actions, "export_plan_sync").Tool != "export_research_plan_sync" {
		t.Fatalf("export = %+v", actions)
	}

	archived := guidanceStudy("archived")
	archivedActions := deriveNextActions(archived)
	assertNoKind(t, archivedActions, "prepare_experiment")
	assertNoKind(t, archivedActions, "resume_study")
	archivedReview := actionByKind(t, archivedActions, "review_study")
	if !strings.Contains(archivedReview.Detail, "retrospective and evidence-only") {
		t.Fatalf("archived review = %+v", archivedReview)
	}
}

func TestAssertedObservationsDoNotRankDecisionFirst(t *testing.T) {
	view := guidanceStudy("active")
	view.Nodes = append(view.Nodes,
		NodeView{ID: "o1", Kind: "observation", Title: "Paper A", MetricSource: metricSourceAsserted},
		NodeView{ID: "o2", Kind: "observation", Title: "Paper B", MetricSource: metricSourceAsserted},
	)
	view.Edges = append(view.Edges,
		EdgeView{FromID: "h", ToID: "o1", Relation: "leads_to"},
		EdgeView{FromID: "o2", ToID: "h", Relation: "supports"},
	)
	actions := deriveNextActions(view)
	if actions[0].Kind != "prepare_experiment" {
		t.Fatalf("first action = %+v", actions)
	}
	if !strings.Contains(actions[0].Detail, "2 outside claims recorded; test them") {
		t.Fatalf("detail = %q", actions[0].Detail)
	}
	assertNoKind(t, actions, "record_decision")

	one := guidanceStudy("active")
	one.Nodes = append(one.Nodes, NodeView{ID: "o", Kind: "observation", Title: "Paper A", MetricSource: metricSourceAsserted})
	one.Edges = append(one.Edges, EdgeView{FromID: "h", ToID: "o", Relation: "leads_to"})
	oneActions := deriveNextActions(one)
	if !strings.Contains(oneActions[0].Detail, "1 outside claim recorded; test them") {
		t.Fatalf("singular detail = %q", oneActions[0].Detail)
	}
}

func TestExperimentEvidenceStillRanksDecisionFirst(t *testing.T) {
	withResult := guidanceStudy("active")
	withResult.Nodes = append(withResult.Nodes, NodeView{ID: "result", Kind: "result", Title: "Smoke accuracy"})
	withResult.Edges = append(withResult.Edges, EdgeView{FromID: "h", ToID: "result", Relation: "leads_to"})
	if actions := deriveNextActions(withResult); actions[0].Kind != "record_decision" {
		t.Fatalf("result actions = %+v", actions)
	}

	measured := guidanceStudy("active")
	measured.Nodes = append(measured.Nodes, NodeView{
		ID: "o", Kind: "observation", Title: "Highlight", SystemWritten: true, MetricSource: metricSourceExperiment,
	})
	measured.Edges = append(measured.Edges, EdgeView{FromID: "h", ToID: "o", Relation: "leads_to"})
	if actions := deriveNextActions(measured); actions[0].Kind != "record_decision" || !strings.Contains(actions[0].Detail, "Highlight") {
		t.Fatalf("measured observation actions = %+v", actions)
	}
}

func guidanceStudy(status string) *StudyView {
	return &StudyView{
		ID: "study-1", Name: "Lineage", Status: status,
		Route: &RouteBinding{ProtocolBranch: "research-plan", ProtocolDocPath: "research-plan/STATUS.md", CodeRefPattern: "autoresearch/*"},
		Nodes: []NodeView{
			{ID: "q", Kind: "question", Title: "Question"},
			{ID: "h", Kind: "hypothesis", Title: "Augmentation helps"},
		},
		Edges: []EdgeView{{FromID: "q", ToID: "h", Relation: "leads_to"}},
	}
}

func actionByKind(t *testing.T, actions []NextAction, kind string) NextAction {
	t.Helper()
	for _, action := range actions {
		if action.Kind == kind {
			return action
		}
	}
	t.Fatalf("missing %s in %+v", kind, actions)
	return NextAction{}
}

func assertNoKind(t *testing.T, actions []NextAction, kind string) {
	t.Helper()
	for _, action := range actions {
		if action.Kind == kind {
			t.Fatalf("unexpected %s: %+v", kind, actions)
		}
	}
}
