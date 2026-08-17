package research

import (
	"context"
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
	StudyID      string   `json:"study_id,omitempty" jsonschema:"Study ID; omit when the Project has exactly one Study"`
	RunNodeID    string   `json:"run_node_id,omitempty" jsonschema:"Graph run node ID to close"`
	ExperimentID string   `json:"experiment_id,omitempty" jsonschema:"same-Project Experiment ID when run_node_id is omitted"`
	Title        string   `json:"title" jsonschema:"short result title"`
	Summary      string   `json:"summary,omitempty" jsonschema:"bounded scientific claim"`
	Status       string   `json:"status,omitempty" jsonschema:"succeeded or failed"`
	MetricName   string   `json:"metric_name,omitempty" jsonschema:"optional scalar metric name"`
	MetricValue  *float64 `json:"metric_value,omitempty" jsonschema:"optional scalar metric value"`
}

func legalEdge(fromKind, toKind researchnode.Kind, relation researchedge.Relation) bool {
	switch relation {
	case researchedge.RelationLeadsTo:
		switch fromKind {
		case researchnode.KindQuestion:
			return toKind == researchnode.KindHypothesis || toKind == researchnode.KindPlan
		case researchnode.KindHypothesis:
			return toKind == researchnode.KindPlan || toKind == researchnode.KindRun || toKind == researchnode.KindDecision
		case researchnode.KindPlan:
			return toKind == researchnode.KindRun || toKind == researchnode.KindPlan
		case researchnode.KindResult, researchnode.KindObservation:
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
	var question, hypotheses, plans, runs, results []NodeView
	for _, node := range view.Nodes {
		nodesByID[node.ID] = node
		switch node.Kind {
		case "question":
			question = append(question, node)
		case "hypothesis":
			hypotheses = append(hypotheses, node)
		case "plan":
			plans = append(plans, node)
		case "run":
			runs = append(runs, node)
		case "result":
			results = append(results, node)
		}
	}
	actions := make([]NextAction, 0, 6)
	add := func(action NextAction) {
		if len(actions) >= 6 {
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
			Detail: "A paid run must start from a hypothesis or plan node, not from the question alone.",
		})
		return actions
	}
	for _, node := range append(append([]NodeView{}, plans...), hypotheses...) {
		if hasOutgoingKind(outgoing, nodesByID, node.ID, "run") {
			continue
		}
		add(NextAction{
			Kind: "prepare_experiment", Tool: "prepare_experiment", FromNodeID: node.ID,
			Title:  "Prepare a run from " + node.Title,
			Detail: "from_node_id is bound into the confirmation digest. submit_prepared_experiment then writes the run node.",
		})
	}
	for _, node := range runs {
		if hasOutgoingKind(outgoing, nodesByID, node.ID, "result") {
			continue
		}
		if _, terminal := terminalExperimentStates[node.ExperimentState]; node.ExperimentID != "" && terminal {
			add(NextAction{
				Kind: "close_run", Tool: "close_run", FromNodeID: node.ID, ExperimentID: node.ExperimentID,
				Title:  "Close " + node.Title,
				Detail: "close_run is the only way to write a result on this run. Include the metric the Owner approved.",
			})
			continue
		}
		add(NextAction{
			Kind: "wait_run", Tool: "get_experiment", FromNodeID: node.ID, ExperimentID: node.ExperimentID,
			Title:  "Wait for " + node.Title,
			Detail: "Do not invent a result while the Experiment is still running.",
		})
	}
	for _, node := range results {
		if hasOutgoingKind(outgoing, nodesByID, node.ID, "decision") {
			continue
		}
		add(NextAction{
			Kind: "record_decision", Tool: "update_research_workspace", FromNodeID: node.ID,
			Title:  "Record a decision from " + node.Title,
			Detail: "Say whether the result supports the hypothesis before preparing another run.",
		})
	}
	return actions
}

func hasOutgoingKind(outgoing map[string][]EdgeView, nodes map[string]NodeView, fromID, kind string) bool {
	for _, edge := range outgoing[fromID] {
		if nodes[edge.ToID].Kind == kind {
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
		return "", nil
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
		return "", nil
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
		return "", nil
	}
	from, err := tx.ResearchNode.Query().Where(
		researchnode.PublicIDEQ(fromPublicID), researchnode.ProjectIDEQ(current.projectID),
	).WithStudy().Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if from.Kind != researchnode.KindHypothesis && from.Kind != researchnode.KindPlan {
		return "", nil
	}
	selected, err := from.Edges.StudyOrErr()
	if err != nil || selected.Status != study.StatusActive {
		return "", nil
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
		SetExperimentID(experimentRecord.ID)
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
	if err := recordEdge(ctx, tx, current, selected, nodes, record, from.PublicID.String(), string(researchedge.RelationLeadsTo)); err != nil {
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
	title := input.Title
	if strings.TrimSpace(title) == "" {
		title = run.Title + " result"
	}
	if _, err := recordNode(ctx, tx, current, selected, NodeInput{
		Kind: string(researchnode.KindResult), Title: title, Summary: input.Summary, Status: string(status),
		MetricName: input.MetricName, MetricValue: input.MetricValue,
		FromNodeID: run.PublicID.String(), Relation: string(researchedge.RelationProduced),
	}); err != nil {
		return Workspace{}, err
	}
	if _, err := run.Update().SetStatus(status).Save(ctx); err != nil {
		return Workspace{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, err
	}
	return s.workspace(ctx, current, selected.PublicID.String())
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
