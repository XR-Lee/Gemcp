package research

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/researchedge"
	"github.com/XR-Lee/Gemcp/ent/researchnode"
	"github.com/XR-Lee/Gemcp/ent/study"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/google/uuid"
)

var terminalExperimentStates = map[string]struct{}{
	"succeeded": {}, "failed": {}, "cancelled": {}, "timed_out": {}, "budget_stopped": {}, "provider_error": {},
}

type NextAction struct {
	Kind         string `json:"kind"`
	Tool         string `json:"tool"`
	StudyID      string `json:"study_id,omitempty"`
	FromNodeID   string `json:"from_node_id,omitempty"`
	ExperimentID string `json:"experiment_id,omitempty"`
	Title        string `json:"title"`
	Detail       string `json:"detail"`
}

type NextActionsView struct {
	StudyID string       `json:"study_id,omitempty"`
	Actions []NextAction `json:"actions"`
}

type CloseRunInput struct {
	StudyID         string   `json:"study_id,omitempty" jsonschema:"Study ID; omit when the Project has exactly one Study"`
	RunNodeID       string   `json:"run_node_id,omitempty" jsonschema:"Graph run node ID to close"`
	ExperimentID    string   `json:"experiment_id,omitempty" jsonschema:"same-Project Experiment ID when run_node_id is omitted"`
	Title           string   `json:"title" jsonschema:"short result title"`
	Summary         string   `json:"summary,omitempty" jsonschema:"bounded scientific claim"`
	Highlight       string   `json:"highlight,omitempty" jsonschema:"short highlight observation linked to the hypothesis; omit to use the result title"`
	Status          string   `json:"status,omitempty" jsonschema:"succeeded or failed"`
	MetricName      string   `json:"metric_name,omitempty" jsonschema:"optional scalar metric name; omit to copy the prepared expected_metric from the terminal Experiment"`
	MetricValue     *float64 `json:"metric_value,omitempty" jsonschema:"optional scalar metric value; omit to copy the matching Experiment metric"`
	ResultCommitSHA string   `json:"result_commit_sha,omitempty" jsonschema:"optional full 40- or 64-character Git commit containing the durable result manifest"`
}

const (
	isolatedSpendOriginMessage   = "from_node_id must hang off a parent Graph node; isolated nodes cannot prepare"
	hypothesislessSpendMessage   = "from_node_id must trace back to a hypothesis through leads_to parents; record the hypothesis before spending"
	legacyCloseHighlightWarning  = "run has no hypothesis ancestor, so no highlight observation was written; link the run's origin to a hypothesis to restore the contract"
	preStudyProposalStaleMessage = "this proposal was prepared before the Study existed; prepare_experiment again with from_node_id on the Graph"
)

func legalEdge(fromKind, toKind researchnode.Kind, relation researchedge.Relation) bool {
	switch relation {
	case researchedge.RelationLeadsTo:
		switch fromKind {
		case researchnode.KindQuestion:
			return toKind == researchnode.KindHypothesis || toKind == researchnode.KindPlan
		case researchnode.KindHypothesis:
			return toKind == researchnode.KindPlan || toKind == researchnode.KindRun || toKind == researchnode.KindDecision || toKind == researchnode.KindObservation
		case researchnode.KindPlan:
			return toKind == researchnode.KindRun || toKind == researchnode.KindPlan
		case researchnode.KindResult:
			return toKind == researchnode.KindDecision || toKind == researchnode.KindHypothesis || toKind == researchnode.KindObservation
		case researchnode.KindObservation:
			return toKind == researchnode.KindDecision || toKind == researchnode.KindHypothesis
		case researchnode.KindDecision:
			return toKind == researchnode.KindHypothesis || toKind == researchnode.KindPlan
		}
	case researchedge.RelationCompares:
		return fromKind == researchnode.KindHypothesis && toKind == researchnode.KindHypothesis
	case researchedge.RelationSupersedes:
		return fromKind == toKind && (fromKind == researchnode.KindHypothesis || fromKind == researchnode.KindPlan || fromKind == researchnode.KindDecision)
	case researchedge.RelationSupports, researchedge.RelationContradicts:
		return (fromKind == researchnode.KindResult || fromKind == researchnode.KindObservation) && toKind == researchnode.KindHypothesis
	case researchedge.RelationProduced:
		return fromKind == researchnode.KindRun && toKind == researchnode.KindResult
	}
	return false
}

func deriveNextActions(view *StudyView) []NextAction {
	if view == nil {
		return []NextAction{{
			Kind: "create_study", Tool: "update_research_workspace",
			Title:  "Create a Study",
			Detail: "Open a research question before preparing a paid Experiment.",
		}}
	}
	outgoing := map[string][]EdgeView{}
	for _, edge := range view.Edges {
		outgoing[edge.FromID] = append(outgoing[edge.FromID], edge)
	}
	nodesByID := map[string]NodeView{}
	var question, hypotheses []NodeView
	for _, node := range view.Nodes {
		nodesByID[node.ID] = node
		switch node.Kind {
		case "question":
			question = append(question, node)
		case "hypothesis":
			hypotheses = append(hypotheses, node)
		}
	}
	actions := make([]NextAction, 0, 8)
	add := func(action NextAction) {
		if len(actions) >= 8 {
			return
		}
		action.StudyID = view.ID
		actions = append(actions, action)
	}
	if len(hypotheses) == 0 {
		fromID := ""
		if len(question) > 0 {
			fromID = question[0].ID
		}
		add(NextAction{
			Kind: "record_hypothesis", Tool: "update_research_workspace", FromNodeID: fromID,
			Title:  "Record a hypothesis",
			Detail: "A paid run must start from a hypothesis or a plan under that hypothesis, not from the question alone.",
		})
		return actions
	}
	addedPrepare := false
	for i := len(hypotheses) - 1; i >= 0; i-- {
		hypothesis := hypotheses[i]
		kids := descendantsOf(outgoing, nodesByID, hypothesis.ID)
		var terminalUnclosed, openRuns, results, observations, decisions []NodeView
		hasRun := false
		for _, node := range kids {
			switch node.Kind {
			case "run":
				hasRun = true
				if hasOutgoingKind(outgoing, nodesByID, node.ID, "result") {
					continue
				}
				if _, terminal := terminalExperimentStates[node.ExperimentState]; node.ExperimentID != "" && terminal {
					terminalUnclosed = append(terminalUnclosed, node)
					continue
				}
				openRuns = append(openRuns, node)
			case "result":
				results = append(results, node)
			case "observation":
				observations = append(observations, node)
			case "decision":
				decisions = append(decisions, node)
			}
		}
		for _, node := range terminalUnclosed {
			add(NextAction{
				Kind: "close_run", Tool: "close_run", FromNodeID: node.ID, ExperimentID: node.ExperimentID,
				Title:  "Close " + node.Title,
				Detail: "Write the result and a highlight observation on hypothesis \"" + hypothesis.Title + "\". close_run is the only writer.",
			})
		}
		for _, node := range openRuns {
			add(NextAction{
				Kind: "wait_run", Tool: "get_experiment", FromNodeID: node.ID, ExperimentID: node.ExperimentID,
				Title:  "Wait for " + node.Title,
				Detail: "Poll get_experiment for state, log_tail, and metrics on hypothesis \"" + hypothesis.Title + "\". Do not SSH or infer metrics from logs.",
			})
		}
		if len(decisions) == 0 && (len(results) > 0 || len(observations) > 0) && len(terminalUnclosed) == 0 && len(openRuns) == 0 {
			fromID := hypothesis.ID
			detail := "Decide whether the evidence supports \"" + hypothesis.Title + "\" before preparing another Experiment."
			if len(results) > 0 {
				fromID = results[len(results)-1].ID
				detail = "Result \"" + results[len(results)-1].Title + "\" is on hypothesis \"" + hypothesis.Title + "\". Record whether it supports or contradicts that claim."
			} else if len(observations) > 0 {
				fromID = observations[len(observations)-1].ID
				detail = "Observation \"" + observations[len(observations)-1].Title + "\" is on hypothesis \"" + hypothesis.Title + "\". Record a decision before proposing the next Experiment."
			}
			add(NextAction{
				Kind: "record_decision", Tool: "update_research_workspace", FromNodeID: fromID,
				Title:  "Record a decision on " + hypothesis.Title,
				Detail: detail,
			})
			continue
		}
		if len(decisions) > 0 && len(terminalUnclosed) == 0 && len(openRuns) == 0 && !decisionSpawnedHypothesis(outgoing, nodesByID, decisions) {
			add(NextAction{
				Kind: "prepare_experiment", Tool: "prepare_experiment", FromNodeID: hypothesis.ID,
				Title:  "Prepare the next Experiment for " + hypothesis.Title,
				Detail: "A decision is recorded on this hypothesis. Propose the next Experiment from it, or record a follow-up hypothesis first.",
			})
			addedPrepare = true
			continue
		}
		if !hasRun {
			add(NextAction{
				Kind: "prepare_experiment", Tool: "prepare_experiment", FromNodeID: hypothesis.ID,
				Title:  "Prepare an Experiment for " + hypothesis.Title,
				Detail: "from_node_id must be this hypothesis or a plan under it. The confirmation digest binds that origin; submit_prepared_experiment writes the run.",
			})
			addedPrepare = true
		}
	}
	if !addedPrepare && len(actions) < 8 {
		newest := hypotheses[len(hypotheses)-1]
		if !hasOpenRun(outgoing, nodesByID, newest.ID) {
			add(NextAction{
				Kind: "prepare_experiment", Tool: "prepare_experiment", FromNodeID: newest.ID,
				Title:  "Prepare the next Experiment for " + newest.Title,
				Detail: "Existing runs and observations are on the Graph. Propose the next Experiment from this hypothesis.",
			})
		}
	}
	return actions
}

func descendantsOf(outgoing map[string][]EdgeView, nodes map[string]NodeView, startID string) []NodeView {
	seen := map[string]bool{startID: true}
	queue := []string{startID}
	var out []NodeView
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, edge := range outgoing[id] {
			if seen[edge.ToID] {
				continue
			}
			seen[edge.ToID] = true
			node, ok := nodes[edge.ToID]
			if !ok {
				continue
			}
			// Stop at hypothesis boundaries: evidence past a follow-up
			// hypothesis belongs to that hypothesis, not to every ancestor.
			if node.Kind == "hypothesis" {
				continue
			}
			out = append(out, node)
			queue = append(queue, node.ID)
		}
	}
	return out
}

func hasOpenRun(outgoing map[string][]EdgeView, nodes map[string]NodeView, hypothesisID string) bool {
	for _, node := range descendantsOf(outgoing, nodes, hypothesisID) {
		if node.Kind != "run" {
			continue
		}
		if !hasOutgoingKind(outgoing, nodes, node.ID, "result") {
			return true
		}
	}
	return false
}

func hasOutgoingKind(outgoing map[string][]EdgeView, nodes map[string]NodeView, fromID, kind string) bool {
	for _, edge := range outgoing[fromID] {
		if nodes[edge.ToID].Kind == kind {
			return true
		}
	}
	return false
}

func decisionSpawnedHypothesis(outgoing map[string][]EdgeView, nodes map[string]NodeView, decisions []NodeView) bool {
	for _, decision := range decisions {
		if hasOutgoingKind(outgoing, nodes, decision.ID, "hypothesis") {
			return true
		}
	}
	return false
}

func (s *Service) AgentNextActions(ctx context.Context, principal agentauth.Principal, input WorkspaceInput) (NextActionsView, error) {
	workspace, err := s.AgentWorkspace(ctx, principal, input)
	if err != nil {
		return NextActionsView{}, err
	}
	view := NextActionsView{Actions: workspace.NextActions}
	if workspace.Study != nil {
		view.StudyID = workspace.Study.ID
	}
	return view, nil
}

func (s *Service) BindPreparedRun(ctx context.Context, principal agentauth.Principal, fromNodeID, experimentID, title string) (string, error) {
	fromNodeID = strings.TrimSpace(fromNodeID)
	if fromNodeID == "" || strings.TrimSpace(experimentID) == "" {
		return "", invalid("from_node_id and experiment_id are required to bind a Graph run")
	}
	tokenID := principal.TokenID
	current := actor{
		tenantID: principal.TenantID, projectID: principal.ProjectID, projectPublic: principal.ProjectPublicID,
		tokenID: &tokenID, actorType: auditevent.ActorTypeAgentToken, actorID: principal.TokenPublicID,
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	experimentRecord, err := findProjectExperiment(ctx, tx, current.projectID, experimentID)
	if err != nil {
		return "", err
	}
	existing, err := tx.ResearchNode.Query().Where(
		researchnode.ProjectIDEQ(current.projectID), researchnode.ExperimentIDEQ(experimentRecord.ID),
	).Only(ctx)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return existing.PublicID.String(), nil
	}
	if !ent.IsNotFound(err) {
		return "", err
	}
	fromPublicID, err := uuid.Parse(fromNodeID)
	if err != nil {
		return "", invalid("from_node_id must be a Graph node ID")
	}
	from, err := tx.ResearchNode.Query().Where(
		researchnode.PublicIDEQ(fromPublicID), researchnode.ProjectIDEQ(current.projectID),
	).WithStudy().Only(ctx)
	if ent.IsNotFound(err) {
		return "", invalid("from_node_id was not found in this Project")
	}
	if err != nil {
		return "", err
	}
	if from.Kind != researchnode.KindHypothesis && from.Kind != researchnode.KindPlan {
		return "", invalid("from_node_id must be a hypothesis or plan node")
	}
	selected, err := from.Edges.StudyOrErr()
	if err != nil || selected.Status != study.StatusActive {
		return "", invalid("from_node_id must belong to an active Study")
	}
	incoming, err := tx.ResearchEdge.Query().Where(researchedge.ToNodeIDEQ(from.ID)).Exist(ctx)
	if err != nil {
		return "", err
	}
	if !incoming {
		return "", invalid(isolatedSpendOriginMessage)
	}
	if from.Kind == researchnode.KindPlan {
		hypothesis, ancestorErr := hypothesisAncestor(ctx, tx.ResearchEdge, selected.ID, from.ID)
		if ancestorErr != nil {
			return "", ancestorErr
		}
		if hypothesis == nil {
			return "", invalid(hypothesislessSpendMessage)
		}
	}
	count, err := tx.ResearchNode.Query().Where(researchnode.StudyIDEQ(selected.ID)).Count(ctx)
	if err != nil {
		return "", err
	}
	if count >= maxNodesPerStudy {
		return "", ErrNodeLimit
	}
	runTitle := strings.TrimSpace(title)
	if runTitle == "" {
		runTitle = "Prepared run"
	}
	if normalized, normErr := normalizeText("title", runTitle, 2, maxTitleLength); normErr == nil {
		runTitle = normalized
	} else {
		runTitle = "Prepared run"
	}
	create := tx.ResearchNode.Create().
		SetTenantID(current.tenantID).SetProjectID(current.projectID).SetStudyID(selected.ID).
		SetKind(researchnode.KindRun).SetTitle(runTitle).SetStatus(researchnode.StatusRunning).
		SetExperimentID(experimentRecord.ID).
		SetOccurredAt(experimentEvidenceTime(experimentRecord)).
		SetCommitSha(experimentRecord.CommitSha)
	if current.tokenID != nil {
		create.SetAgentTokenID(*current.tokenID)
	}
	record, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			bound, lookupErr := tx.ResearchNode.Query().Where(
				researchnode.ProjectIDEQ(current.projectID), researchnode.ExperimentIDEQ(experimentRecord.ID),
			).Only(ctx)
			if lookupErr != nil {
				return "", err
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return "", commitErr
			}
			return bound.PublicID.String(), nil
		}
		return "", err
	}
	nodes, err := tx.ResearchNode.Query().Where(researchnode.StudyIDEQ(selected.ID)).All(ctx)
	if err != nil {
		return "", err
	}
	if err := recordEdge(ctx, tx, current, selected, nodes, record, from.PublicID.String(), string(researchedge.RelationLeadsTo), false); err != nil {
		return "", err
	}
	if _, err := selected.Update().SetUpdatedAt(time.Now().UTC()).Save(ctx); err != nil {
		return "", err
	}
	if err := writeAudit(ctx, tx, current, "research.run_bound", "research_node", record.PublicID.String(), map[string]any{
		"study_id": selected.PublicID.String(), "experiment_id": experimentRecord.PublicID.String(), "from_node_id": from.PublicID.String(),
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return record.PublicID.String(), nil
}

func (s *Service) AgentCloseRun(ctx context.Context, principal agentauth.Principal, input CloseRunInput) (Workspace, error) {
	if !principal.HasScope("submit") {
		return Workspace{}, ErrForbidden
	}
	resultCommitSHA, err := normalizeResultCommit(input.ResultCommitSHA)
	if err != nil {
		return Workspace{}, err
	}
	tokenID := principal.TokenID
	current := actor{
		tenantID: principal.TenantID, projectID: principal.ProjectID, projectPublic: principal.ProjectPublicID,
		tokenID: &tokenID, actorType: auditevent.ActorTypeAgentToken, actorID: principal.TokenPublicID,
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Workspace{}, err
	}
	defer func() { _ = tx.Rollback() }()
	studies, err := tx.Study.Query().Where(study.ProjectIDEQ(current.projectID)).Order(ent.Desc(study.FieldUpdatedAt)).All(ctx)
	if err != nil {
		return Workspace{}, err
	}
	selected, err := selectStudy(studies, input.StudyID)
	if err != nil {
		return Workspace{}, err
	}
	if selected == nil {
		if len(studies) > 1 {
			return Workspace{}, ErrChoice
		}
		return Workspace{}, invalid("create a Study before closing a run")
	}
	run, err := findCloseRunNode(ctx, tx, current.projectID, selected.ID, input.RunNodeID, input.ExperimentID)
	if err != nil {
		return Workspace{}, err
	}
	produced, err := tx.ResearchEdge.Query().Where(
		researchedge.StudyIDEQ(selected.ID), researchedge.FromNodeIDEQ(run.ID), researchedge.RelationEQ(researchedge.RelationProduced),
	).Exist(ctx)
	if err != nil {
		return Workspace{}, err
	}
	if produced {
		if err := tx.Commit(); err != nil {
			return Workspace{}, err
		}
		return s.workspace(ctx, current, selected.PublicID.String())
	}
	experimentRecord, err := run.Edges.ExperimentOrErr()
	if err != nil || experimentRecord == nil {
		return Workspace{}, invalid("close_run requires a Graph run that already links a Project Experiment")
	}
	if _, terminal := terminalExperimentStates[experimentRecord.State]; !terminal {
		return Workspace{}, invalid("close_run requires a terminal Experiment; wait or cancel first")
	}
	status := researchnode.StatusSucceeded
	if strings.TrimSpace(input.Status) != "" {
		if input.Status != string(researchnode.StatusSucceeded) && input.Status != string(researchnode.StatusFailed) {
			return Workspace{}, invalid("status must be succeeded or failed")
		}
		status = researchnode.Status(input.Status)
	} else if experimentRecord.State != "succeeded" {
		status = researchnode.StatusFailed
	}
	if status == researchnode.StatusSucceeded && experimentRecord.State != "succeeded" {
		return Workspace{}, invalid("a non-succeeded Experiment cannot be recorded as a succeeded result")
	}
	if err := fillCloseRunMetric(ctx, experimentRecord, &input); err != nil {
		return Workspace{}, err
	}
	if err := validateCloseRunMetric(ctx, experimentRecord, status, input.MetricName, input.MetricValue); err != nil {
		return Workspace{}, err
	}
	title := input.Title
	if strings.TrimSpace(title) == "" {
		title = run.Title + " result"
	}
	hypothesis, err := hypothesisAncestor(ctx, tx.ResearchEdge, selected.ID, run.ID)
	if err != nil {
		return Workspace{}, err
	}
	// close_run is the only writer for a funded run, so its terminal writes are
	// exempt from the study caps: a full Graph must never leave paid work
	// permanently uncloseable.
	resultNode, err := recordNode(ctx, tx, current, selected, NodeInput{
		Kind: string(researchnode.KindResult), Title: title, Summary: input.Summary, Status: string(status),
		MetricName: input.MetricName, MetricValue: input.MetricValue,
		OccurredAt: experimentEvidenceTime(experimentRecord).Format(time.RFC3339),
		CommitSHA:  resultCommitSHA,
		FromNodeID: run.PublicID.String(), Relation: string(researchedge.RelationProduced),

		exemptStudyCaps: true,
	})
	if err != nil {
		return Workspace{}, err
	}
	warning := ""
	if hypothesis == nil {
		// Legacy runs bound before the hypothesis contract cannot fail forever:
		// write the result, skip the highlight, and tell the agent why.
		warning = legacyCloseHighlightWarning
	} else if err := writeHighlightObservation(ctx, tx, current, selected, hypothesis, resultNode, experimentRecord, input, title, status, resultCommitSHA); err != nil {
		return Workspace{}, err
	}
	if _, err := run.Update().SetStatus(status).Save(ctx); err != nil {
		return Workspace{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, err
	}
	workspace, err := s.workspace(ctx, current, selected.PublicID.String())
	if err != nil {
		return Workspace{}, err
	}
	workspace.Warning = warning
	return workspace, nil
}

func writeHighlightObservation(ctx context.Context, tx *ent.Tx, current actor, selected *ent.Study, hypothesis, resultNode *ent.ResearchNode, experimentRecord *ent.Experiment, input CloseRunInput, resultTitle string, status researchnode.Status, resultCommitSHA string) error {
	highlightTitle := strings.TrimSpace(input.Highlight)
	if highlightTitle == "" {
		highlightTitle = resultTitle
	}
	commitSHA := resultCommitSHA
	if commitSHA == "" {
		commitSHA = experimentRecord.CommitSha
	}
	observation, err := recordNode(ctx, tx, current, selected, NodeInput{
		Kind: string(researchnode.KindObservation), Title: highlightTitle, Summary: input.Summary, Status: string(status),
		MetricName: input.MetricName, MetricValue: input.MetricValue,
		OccurredAt: experimentEvidenceTime(experimentRecord).Format(time.RFC3339),
		CommitSHA:  commitSHA,
		FromNodeID: hypothesis.PublicID.String(), Relation: string(researchedge.RelationLeadsTo),

		exemptStudyCaps: true,
	})
	if err != nil {
		return err
	}
	nodes, err := tx.ResearchNode.Query().Where(researchnode.StudyIDEQ(selected.ID)).All(ctx)
	if err != nil {
		return err
	}
	// Tie the highlight to its concrete result so the Owner view shows the
	// right highlight when one hypothesis accumulates several runs.
	if resultNode != nil {
		if err := recordEdge(ctx, tx, current, selected, nodes, observation, resultNode.PublicID.String(), string(researchedge.RelationLeadsTo), true); err != nil {
			return err
		}
	}
	relation := researchedge.RelationSupports
	if status != researchnode.StatusSucceeded {
		relation = researchedge.RelationContradicts
	}
	return recordEdge(ctx, tx, current, selected, nodes, hypothesis, observation.PublicID.String(), string(relation), true)
}

// hypothesisAncestor walks leads_to parents through plan, question, and
// decision nodes and returns the nearest hypothesis, or nil when the start
// node does not trace back to one.
func hypothesisAncestor(ctx context.Context, edgeClient *ent.ResearchEdgeClient, studyID, startID int) (*ent.ResearchNode, error) {
	seen := map[int]bool{}
	queue := []int{startID}
	for len(queue) > 0 && len(seen) < maxNodesPerStudy {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		edges, err := edgeClient.Query().Where(
			researchedge.StudyIDEQ(studyID), researchedge.ToNodeIDEQ(id), researchedge.RelationEQ(researchedge.RelationLeadsTo),
		).WithFromNode().All(ctx)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			from, err := edge.Edges.FromNodeOrErr()
			if err != nil || from == nil {
				continue
			}
			if from.Kind == researchnode.KindHypothesis {
				return from, nil
			}
			if from.Kind == researchnode.KindPlan || from.Kind == researchnode.KindQuestion || from.Kind == researchnode.KindDecision {
				queue = append(queue, from.ID)
			}
		}
	}
	return nil, nil
}

// ValidatePreparedBind reports whether submit_prepared_experiment will be able
// to bind a Graph run for this proposal, so the check runs before any budget
// is committed. An empty fromNodeID is only valid while the Project still has
// no active Study.
func (s *Service) ValidatePreparedBind(ctx context.Context, principal agentauth.Principal, fromNodeID string) error {
	fromNodeID = strings.TrimSpace(fromNodeID)
	if fromNodeID == "" {
		hasStudy, err := s.client.Study.Query().Where(
			study.ProjectIDEQ(principal.ProjectID), study.StatusEQ(study.StatusActive),
		).Exist(ctx)
		if err != nil {
			return err
		}
		if hasStudy {
			return invalid(preStudyProposalStaleMessage)
		}
		return nil
	}
	publicID, err := uuid.Parse(fromNodeID)
	if err != nil {
		return invalid("from_node_id must be a Graph node ID")
	}
	node, err := s.client.ResearchNode.Query().Where(
		researchnode.PublicIDEQ(publicID), researchnode.ProjectIDEQ(principal.ProjectID),
	).WithStudy().Only(ctx)
	if ent.IsNotFound(err) {
		return invalid("from_node_id was not found in this Project")
	}
	if err != nil {
		return err
	}
	selected, err := node.Edges.StudyOrErr()
	if err != nil || selected.Status != study.StatusActive {
		return invalid("from_node_id must belong to an active Study")
	}
	if node.Kind != researchnode.KindHypothesis && node.Kind != researchnode.KindPlan {
		return invalid("from_node_id must be a hypothesis or plan node")
	}
	incoming, err := s.client.ResearchEdge.Query().Where(researchedge.ToNodeIDEQ(node.ID)).Exist(ctx)
	if err != nil {
		return err
	}
	if !incoming {
		return invalid(isolatedSpendOriginMessage)
	}
	if node.Kind == researchnode.KindPlan {
		hypothesis, err := hypothesisAncestor(ctx, s.client.ResearchEdge, selected.ID, node.ID)
		if err != nil {
			return err
		}
		if hypothesis == nil {
			return invalid(hypothesislessSpendMessage)
		}
	}
	count, err := s.client.ResearchNode.Query().Where(researchnode.StudyIDEQ(selected.ID)).Count(ctx)
	if err != nil {
		return err
	}
	if count >= maxNodesPerStudy {
		return ErrNodeLimit
	}
	return nil
}

func normalizeResultCommit(value string) (string, error) {
	sha := strings.ToLower(strings.TrimSpace(value))
	if sha == "" {
		return "", nil
	}
	if len(sha) != 40 && len(sha) != 64 {
		return "", invalid("result_commit_sha must be a full 40- or 64-character hexadecimal Git commit SHA")
	}
	if !evidenceCommitPattern.MatchString(sha) {
		return "", invalid("result_commit_sha must be a full 40- or 64-character hexadecimal Git commit SHA")
	}
	return sha, nil
}

func fillCloseRunMetric(ctx context.Context, experimentRecord *ent.Experiment, input *CloseRunInput) error {
	if strings.TrimSpace(input.MetricName) != "" || input.MetricValue != nil {
		return nil
	}
	expected, err := proposalExpectedMetric(ctx, experimentRecord)
	if err != nil {
		return err
	}
	if expected == "" {
		return nil
	}
	observed, ok := scalarMetric(experimentRecord.Metrics[expected])
	if !ok {
		return nil
	}
	value := observed
	input.MetricName = expected
	input.MetricValue = &value
	return nil
}

func validateCloseRunMetric(ctx context.Context, experimentRecord *ent.Experiment, status researchnode.Status, metricName string, metricValue *float64) error {
	metricName = strings.TrimSpace(metricName)
	if metricName == "" && metricValue != nil {
		return invalid("metric_value requires metric_name")
	}
	if metricName != "" {
		if !metricPattern.MatchString(metricName) {
			return invalid("metric_name must use letters, numbers, dots, underscores, or hyphens")
		}
		if metricValue == nil || math.IsNaN(*metricValue) || math.IsInf(*metricValue, 0) {
			return invalid("metric_value must be a finite scalar when metric_name is provided")
		}
		observed, ok := scalarMetric(experimentRecord.Metrics[metricName])
		if !ok {
			return invalid("metric_name was not reported by the terminal Experiment")
		}
		if !sameMetric(observed, *metricValue) {
			return invalid("metric_value does not match the terminal Experiment metric")
		}
	}
	expectedMetric, err := proposalExpectedMetric(ctx, experimentRecord)
	if err != nil {
		return err
	}
	if status == researchnode.StatusSucceeded && expectedMetric != "" && metricName != expectedMetric {
		return invalid("a succeeded result must record the expected_metric from the prepared proposal")
	}
	return nil
}

func proposalExpectedMetric(ctx context.Context, experimentRecord *ent.Experiment) (string, error) {
	proposal, err := experimentRecord.QueryProposal().Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	value, _ := proposal.ProjectSnapshot["expected_metric"].(string)
	return strings.TrimSpace(value), nil
}

func scalarMetric(value any) (float64, bool) {
	var result float64
	switch value := value.(type) {
	case float64:
		result = value
	case float32:
		result = float64(value)
	case int:
		result = float64(value)
	case int8:
		result = float64(value)
	case int16:
		result = float64(value)
	case int32:
		result = float64(value)
	case int64:
		result = float64(value)
	case uint:
		result = float64(value)
	case uint8:
		result = float64(value)
	case uint16:
		result = float64(value)
	case uint32:
		result = float64(value)
	case uint64:
		result = float64(value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		result = parsed
	default:
		return 0, false
	}
	return result, !math.IsNaN(result) && !math.IsInf(result, 0)
}

func sameMetric(observed, reported float64) bool {
	scale := math.Max(1, math.Max(math.Abs(observed), math.Abs(reported)))
	return math.Abs(observed-reported) <= scale*1e-12
}

func findCloseRunNode(ctx context.Context, tx *ent.Tx, projectID, studyID int, runNodeID, experimentID string) (*ent.ResearchNode, error) {
	if strings.TrimSpace(runNodeID) != "" {
		publicID, err := uuid.Parse(strings.TrimSpace(runNodeID))
		if err != nil {
			return nil, invalid("run_node_id must be a Graph run node ID")
		}
		record, err := tx.ResearchNode.Query().Where(
			researchnode.PublicIDEQ(publicID), researchnode.ProjectIDEQ(projectID), researchnode.StudyIDEQ(studyID),
			researchnode.KindEQ(researchnode.KindRun),
		).WithExperiment().Only(ctx)
		if ent.IsNotFound(err) {
			return nil, invalid("run_node_id was not found in this Study")
		}
		return record, err
	}
	if strings.TrimSpace(experimentID) == "" {
		return nil, invalid("run_node_id or experiment_id is required")
	}
	experimentRecord, err := findProjectExperiment(ctx, tx, projectID, experimentID)
	if err != nil {
		return nil, err
	}
	record, err := tx.ResearchNode.Query().Where(
		researchnode.ProjectIDEQ(projectID), researchnode.StudyIDEQ(studyID),
		researchnode.KindEQ(researchnode.KindRun), researchnode.ExperimentIDEQ(experimentRecord.ID),
	).WithExperiment().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, invalid("no Graph run is bound to this Experiment")
	}
	return record, err
}
