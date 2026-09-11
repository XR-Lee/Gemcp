package experiment

import (
	"context"
	"regexp"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/researchedge"
	"github.com/XR-Lee/Gemcp/ent/researchnode"
	"github.com/XR-Lee/Gemcp/ent/study"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/google/uuid"
)

var proposalMetricPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,79}$`)

func (s *Service) resolveGraphOrigin(ctx context.Context, principal agentauth.Principal, input PrepareInput) (string, string, error) {
	fromNodeID := strings.TrimSpace(input.FromNodeID)
	expectedMetric := strings.TrimSpace(input.ExpectedMetric)
	if expectedMetric != "" && !proposalMetricPattern.MatchString(expectedMetric) {
		return "", "", &ValidationError{Message: "expected_metric must use letters, numbers, dots, underscores, or hyphens"}
	}
	hasStudy, err := s.client.Study.Query().Where(
		study.ProjectIDEQ(principal.ProjectID), study.StatusEQ(study.StatusActive),
	).Exist(ctx)
	if err != nil {
		return "", "", err
	}
	if !hasStudy {
		if fromNodeID != "" {
			return "", "", &ValidationError{Message: "from_node_id requires an active Study"}
		}
		return "", expectedMetric, nil
	}
	if fromNodeID == "" {
		return "", "", &ValidationError{Message: "from_node_id is required when the Project has an active Study"}
	}
	publicID, err := uuid.Parse(fromNodeID)
	if err != nil {
		return "", "", &ValidationError{Message: "from_node_id must be a Graph node ID"}
	}
	node, err := s.client.ResearchNode.Query().Where(
		researchnode.PublicIDEQ(publicID), researchnode.ProjectIDEQ(principal.ProjectID),
	).WithStudy().Only(ctx)
	if ent.IsNotFound(err) {
		return "", "", &ValidationError{Message: "from_node_id was not found in this Project"}
	}
	if err != nil {
		return "", "", err
	}
	selected, err := node.Edges.StudyOrErr()
	if err != nil || selected.Status != study.StatusActive {
		return "", "", &ValidationError{Message: "from_node_id must belong to an active Study"}
	}
	if node.Kind != researchnode.KindHypothesis && node.Kind != researchnode.KindPlan {
		return "", "", &ValidationError{Message: "from_node_id must be a hypothesis or plan node"}
	}
	incoming, err := s.client.ResearchEdge.Query().Where(researchedge.ToNodeIDEQ(node.ID)).Exist(ctx)
	if err != nil {
		return "", "", err
	}
	if !incoming {
		return "", "", &ValidationError{Message: "from_node_id must hang off a parent Graph node; isolated nodes cannot prepare"}
	}
	if node.Kind == researchnode.KindPlan {
		traceable, err := s.planTracesToHypothesis(ctx, selected.ID, node.ID)
		if err != nil {
			return "", "", err
		}
		if !traceable {
			return "", "", &ValidationError{Message: "from_node_id must trace back to a hypothesis through leads_to parents; record the hypothesis before spending"}
		}
	}
	return node.PublicID.String(), expectedMetric, nil
}

// planTracesToHypothesis mirrors the research-side ancestor walk: close_run
// links the highlight to the nearest hypothesis, so a plan may only spend when
// that hypothesis exists.
func (s *Service) planTracesToHypothesis(ctx context.Context, studyID, startID int) (bool, error) {
	const maxTraversal = 128
	seen := map[int]bool{}
	queue := []int{startID}
	for len(queue) > 0 && len(seen) < maxTraversal {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		edges, err := s.client.ResearchEdge.Query().Where(
			researchedge.StudyIDEQ(studyID), researchedge.ToNodeIDEQ(id), researchedge.RelationEQ(researchedge.RelationLeadsTo),
		).WithFromNode().All(ctx)
		if err != nil {
			return false, err
		}
		for _, edge := range edges {
			from, err := edge.Edges.FromNodeOrErr()
			if err != nil || from == nil {
				continue
			}
			switch from.Kind {
			case researchnode.KindHypothesis:
				return true, nil
			case researchnode.KindPlan, researchnode.KindQuestion, researchnode.KindDecision:
				queue = append(queue, from.ID)
			}
		}
	}
	return false, nil
}

func proposalStoredProjectSnapshot(resolved proposalResolved) map[string]any {
	snapshot := proposalProjectSnapshot(resolved.project)
	if resolved.fromNodeID != "" {
		snapshot["from_node_id"] = resolved.fromNodeID
	}
	if resolved.expectedMetric != "" {
		snapshot["expected_metric"] = resolved.expectedMetric
	}
	if resolved.dataset != "" {
		snapshot["dataset"] = resolved.dataset
	}
	if resolved.workload != "" {
		snapshot["workload"] = resolved.workload
		if len(resolved.parameters) > 0 {
			snapshot["workload_parameters"] = resolved.parameters
		}
	}
	return snapshot
}

func (s *Service) bindPreparedGraph(ctx context.Context, principal agentauth.Principal, snapshot map[string]any, result SubmitPreparedResult) (SubmitPreparedResult, error) {
	if s.graphBinder == nil {
		return result, nil
	}
	fromNodeID := snapshotString(snapshot, "from_node_id")
	if fromNodeID == "" {
		return result, nil
	}
	title := "Prepared run"
	if command := strings.TrimSpace(result.Experiment.Command); command != "" {
		title = command
	}
	runNodeID, err := s.graphBinder.BindPreparedRun(ctx, principal, fromNodeID, result.Experiment.ID, title)
	if err != nil {
		// The Experiment and budget are already committed; a bare error here
		// would push the agent into preparing (and paying) again. Return the
		// submitted Experiment and say exactly what is left to fix.
		result.GraphBindWarning = "Experiment " + result.Experiment.ID + " was submitted, but binding the Graph run failed: " + err.Error() +
			". Do not prepare again; fix the Graph origin and retry submit_prepared_experiment to bind this same Experiment."
		return result, nil
	}
	if strings.TrimSpace(runNodeID) == "" {
		result.GraphBindWarning = "Experiment " + result.Experiment.ID + " was submitted, but no Graph run was bound. Do not prepare again; retry submit_prepared_experiment to bind this same Experiment."
		return result, nil
	}
	result.RunNodeID = runNodeID
	return result, nil
}
