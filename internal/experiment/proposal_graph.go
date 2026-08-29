package experiment

import (
	"context"
	"regexp"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
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
	return node.PublicID.String(), expectedMetric, nil
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
		return result, err
	}
	result.RunNodeID = runNodeID
	return result, nil
}
