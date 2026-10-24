package research

import (
	"strings"
	"testing"
	"time"
)

func TestPlanSyncListsObservationsOnEachHypothesis(t *testing.T) {
	accuracy := 0.93
	measured := 0.81
	workspace := Workspace{Study: &StudyView{
		Name: "Lineage", Question: "Does augmentation help?",
		Nodes: []NodeView{
			{ID: "h", Kind: "hypothesis", Title: "Augmentation helps"},
			{ID: "follow", Kind: "hypothesis", Title: "Follow-up"},
			{ID: "paper", Kind: "observation", Title: "Paper X", Summary: "Table 2", MetricName: "accuracy", MetricValue: &accuracy, MetricSource: metricSourceAsserted},
			{ID: "note", Kind: "observation", Title: "Lab note", Summary: "Seen in the logs"},
			{ID: "highlight", Kind: "observation", Title: "Closed highlight", MetricName: "accuracy", MetricValue: &measured, MetricSource: metricSourceExperiment, SystemWritten: true},
			{ID: "child", Kind: "observation", Title: "Belongs to the follow-up"},
		},
		Edges: []EdgeView{
			{FromID: "h", ToID: "paper", Relation: "leads_to"},
			{FromID: "paper", ToID: "h", Relation: "supports"},
			{FromID: "note", ToID: "h", Relation: "contradicts"},
			{FromID: "h", ToID: "highlight", Relation: "leads_to"},
			{FromID: "h", ToID: "follow", Relation: "leads_to"},
			{FromID: "follow", ToID: "child", Relation: "leads_to"},
		},
		Hypotheses: []HypothesisView{
			{ID: "h", Title: "Augmentation helps"},
			{ID: "follow", Title: "Follow-up"},
		},
	}}
	markdown := renderPlanSyncMarkdown(workspace, "research-plan", "research-plan/STATUS.md", time.Unix(0, 0).UTC())
	for _, want := range []string{
		"- No experiment yet.",
		"- Observation (supports): Paper X. Table 2. accuracy = 0.93 (asserted)",
		"- Observation (contradicts): Lab note. Seen in the logs",
		"- Observation (leads_to): Closed highlight. accuracy = 0.81 (measured)",
		"- Observation (leads_to): Belongs to the follow-up",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("missing %q\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "- Observation (leads_to): Paper X") {
		t.Fatalf("supports lost to leads_to\n%s", markdown)
	}
	parent := markdown[:strings.Index(markdown, "### Follow-up")]
	if strings.Contains(parent, "Belongs to the follow-up") {
		t.Fatalf("follow-up observation leaked onto the parent\n%s", markdown)
	}
}
