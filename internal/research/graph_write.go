package research

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/agentauth"
)

// Graph writes answer with what they recorded instead of the whole Study, so
// an Agent's context does not grow with the Graph. get_research_workspace is
// the full read; view full asks a write for the same workspace.
const (
	ViewCompact = "compact"
	ViewFull    = "full"
)

// GraphWrite is what update_research_workspace and close_run return: the
// nodes and edges the call recorded, the hypothesis record it touched, and the
// next actions. Studies and Study are the whole workspace, sent only for view
// full.
type GraphWrite struct {
	ProjectID   string             `json:"project_id"`
	View        string             `json:"view"`
	StudyID     string             `json:"study_id"`
	Nodes       []NodeView         `json:"nodes"`
	Edges       []EdgeView         `json:"edges"`
	Hypothesis  *HypothesisSummary `json:"hypothesis,omitempty"`
	Plan        *PlanView          `json:"plan,omitempty"`
	NextActions []NextAction       `json:"next_actions"`
	Warning     string             `json:"warning,omitempty"`
	GeneratedAt time.Time          `json:"generated_at"`
	Studies     []StudySummary     `json:"studies,omitempty"`
	Study       *StudyView         `json:"study,omitempty"`
}

// HypothesisSummary is the hypothesis record a Graph write touched. In place
// of every run record it carries a tally of the hypothesis's runs and, after
// close_run, the record of the run just closed.
type HypothesisSummary struct {
	ID      string               `json:"id"`
	Title   string               `json:"title"`
	Summary string               `json:"summary,omitempty"`
	Status  string               `json:"status"`
	Branch  string               `json:"branch,omitempty"`
	Tally   HypothesisTally      `json:"tally"`
	Run     *HypothesisRunRecord `json:"run,omitempty"`
}

// HypothesisTally counts a hypothesis's runs. Open runs have no result yet;
// closed runs count under the verdict close_run recorded.
type HypothesisTally struct {
	Runs         int `json:"runs"`
	Open         int `json:"open"`
	Supports     int `json:"supports"`
	Contradicts  int `json:"contradicts"`
	Inconclusive int `json:"inconclusive"`
}

// written names what one Graph write recorded, by public ID.
type written struct {
	studyID      string
	nodeIDs      []string
	edges        []edgeKey
	plan         bool
	hypothesisID string
	runID        string
}

type edgeKey struct{ from, to, relation string }

func normalizeView(value string) (string, error) {
	switch view := strings.ToLower(strings.TrimSpace(value)); view {
	case "", ViewCompact:
		return ViewCompact, nil
	case ViewFull:
		return ViewFull, nil
	default:
		return "", invalid("view must be compact or full")
	}
}

// AgentUpdateView is update_research_workspace: it applies an Agent's update
// and returns it in the requested view. A bad view is refused before anything
// is written.
func (s *Service) AgentUpdateView(ctx context.Context, principal agentauth.Principal, input UpdateInput) (GraphWrite, error) {
	if !principal.HasScope("submit") {
		return GraphWrite{}, ErrForbidden
	}
	view, err := normalizeView(input.View)
	if err != nil {
		return GraphWrite{}, err
	}
	workspace, record, err := s.update(ctx, agentWriter(principal), input)
	if err != nil {
		return GraphWrite{}, err
	}
	return makeGraphWrite(workspace, record, view), nil
}

// AgentCloseRunView is close_run: it closes a run for an Agent and returns it
// in the requested view. A bad view is refused before anything is written.
func (s *Service) AgentCloseRunView(ctx context.Context, principal agentauth.Principal, input CloseRunInput) (GraphWrite, error) {
	if !principal.HasScope("submit") {
		return GraphWrite{}, ErrForbidden
	}
	view, err := normalizeView(input.View)
	if err != nil {
		return GraphWrite{}, err
	}
	workspace, record, err := s.closeRun(ctx, agentWriter(principal), input)
	if err != nil {
		return GraphWrite{}, err
	}
	return makeGraphWrite(workspace, record, view), nil
}

// makeGraphWrite cuts the workspace after a write down to what the write
// recorded, and keeps the whole workspace for view full.
func makeGraphWrite(workspace Workspace, record written, view string) GraphWrite {
	result := GraphWrite{
		ProjectID: workspace.ProjectID, View: view, StudyID: record.studyID,
		Nodes: []NodeView{}, Edges: []EdgeView{},
		NextActions: workspace.NextActions, Warning: workspace.Warning, GeneratedAt: workspace.GeneratedAt,
	}
	if view == ViewFull {
		result.Studies, result.Study = workspace.Studies, workspace.Study
	}
	study := workspace.Study
	if study == nil {
		return result
	}
	for _, node := range study.Nodes {
		if slices.Contains(record.nodeIDs, node.ID) {
			result.Nodes = append(result.Nodes, node)
		}
	}
	for _, edge := range study.Edges {
		if slices.Contains(record.edges, edgeKey{from: edge.FromID, to: edge.ToID, relation: edge.Relation}) {
			result.Edges = append(result.Edges, edge)
		}
	}
	if record.plan {
		result.Plan = study.Plan
	}
	for _, hypothesis := range study.Hypotheses {
		if hypothesis.ID == record.hypothesisID {
			result.Hypothesis = summarizeHypothesis(hypothesis, record.runID)
		}
	}
	return result
}

// summarizeHypothesis keeps a hypothesis record's claim, tallies its runs, and
// keeps the record of runID.
func summarizeHypothesis(record HypothesisView, runID string) *HypothesisSummary {
	summary := &HypothesisSummary{
		ID: record.ID, Title: record.Title, Summary: record.Summary, Status: record.Status, Branch: record.Branch,
	}
	for _, run := range record.Experiments {
		summary.Tally.Runs++
		// Every result has a title, so a run without one is still open.
		switch {
		case run.ResultTitle == "":
			summary.Tally.Open++
		case run.Verdict == verdictSupports:
			summary.Tally.Supports++
		case run.Verdict == verdictContradicts:
			summary.Tally.Contradicts++
		default:
			summary.Tally.Inconclusive++
		}
		if run.RunNodeID == runID {
			closed := run
			summary.Run = &closed
		}
	}
	return summary
}

// findHypothesis names the hypothesis whose record the recorded nodes belong
// to, and the run among them, for writes other than close_run.
func (w *written) findHypothesis(view *StudyView) {
	if view == nil {
		return
	}
	for _, node := range view.Nodes {
		if !slices.Contains(w.nodeIDs, node.ID) {
			continue
		}
		if w.hypothesisID == "" {
			w.hypothesisID = owningHypothesis(view, node.ID)
		}
		if w.runID == "" && node.Kind == "run" {
			w.runID = node.ID
		}
	}
}

// owningHypothesis is the node itself when it is a hypothesis, or else the
// nearest hypothesis upstream of it: the hypothesis whose record holds the
// node's evidence. A node that hangs off no hypothesis, such as the question,
// has none.
func owningHypothesis(view *StudyView, nodeID string) string {
	kinds := make(map[string]string, len(view.Nodes))
	for _, node := range view.Nodes {
		kinds[node.ID] = node.Kind
	}
	incoming := map[string][]string{}
	for _, edge := range view.Edges {
		incoming[edge.ToID] = append(incoming[edge.ToID], edge.FromID)
	}
	seen := map[string]bool{nodeID: true}
	for queue := []string{nodeID}; len(queue) > 0; queue = queue[1:] {
		id := queue[0]
		if kinds[id] == "hypothesis" {
			return id
		}
		for _, from := range incoming[id] {
			if !seen[from] {
				seen[from] = true
				queue = append(queue, from)
			}
		}
	}
	return ""
}

// addClosedRun names what close_run recorded for w.runID: its result, the
// highlight observation, and the edges that tie them to the run and the
// hypothesis.
func (w *written) addClosedRun(view *StudyView) {
	if view == nil {
		return
	}
	resultID := ""
	for _, edge := range view.Edges {
		if edge.FromID == w.runID && edge.Relation == "produced" {
			resultID = edge.ToID
			break
		}
	}
	if resultID == "" {
		return
	}
	w.nodeIDs = append(w.nodeIDs, resultID)
	w.edges = append(w.edges, edgeKey{from: w.runID, to: resultID, relation: "produced"})
	// A run without a hypothesis ancestor closes without a highlight.
	highlightID := ""
	if w.hypothesisID != "" {
		highlightID = highlightOf(view, resultID)
	}
	if highlightID == "" {
		return
	}
	w.nodeIDs = append(w.nodeIDs, highlightID)
	// Of the two verdict edges, at most the one close_run recorded exists.
	w.edges = append(w.edges,
		edgeKey{from: w.hypothesisID, to: highlightID, relation: "leads_to"},
		edgeKey{from: resultID, to: highlightID, relation: "leads_to"},
		edgeKey{from: highlightID, to: w.hypothesisID, relation: verdictSupports},
		edgeKey{from: highlightID, to: w.hypothesisID, relation: verdictContradicts},
	)
}

// highlightOf is the observation close_run hung off a result: the one marked
// as written from Experiment evidence or, on a Graph from before that mark,
// the first. An Agent may link its own observations to the result later.
func highlightOf(view *StudyView, resultID string) string {
	nodes := make(map[string]NodeView, len(view.Nodes))
	for _, node := range view.Nodes {
		nodes[node.ID] = node
	}
	first := ""
	for _, edge := range view.Edges {
		node, ok := nodes[edge.ToID]
		if edge.FromID != resultID || edge.Relation != "leads_to" || !ok || node.Kind != "observation" {
			continue
		}
		if node.SystemWritten {
			return node.ID
		}
		if first == "" {
			first = node.ID
		}
	}
	return first
}
