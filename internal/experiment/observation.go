package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/agenttoken"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/cloudsshassignment"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/projectworkload"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/executionmeta"
)

func (s *Service) enrichExecutionObservation(ctx context.Context, record *ent.Experiment, view *View) error {
	if record.AgentTokenID != nil {
		token, err := s.client.AgentToken.Query().Where(agenttoken.IDEQ(*record.AgentTokenID)).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		if err == nil {
			view.ExecutionContext.AgentLabel = token.Label
			view.ExecutionContext.AgentTokenPrefix = token.Prefix
		}
	}
	proposal, err := s.client.ExperimentProposal.Query().Where(experimentproposal.ExperimentIDEQ(record.ID)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if err == nil {
		view.ExecutionContext.ProposalID = proposal.PublicID.String()
		view.ExecutionContext.RequestedRef = proposal.RequestedRef
		view.ExecutionContext.Workload = snapshotString(proposal.ProjectSnapshot, "workload")
		view.ExecutionContext.ExpectedMetric = snapshotString(proposal.ProjectSnapshot, "expected_metric")
		view.SavableWorkload = record.State == "succeeded" &&
			string(record.ExecutionMode) == "argv" &&
			len(record.Argv) > 0 &&
			view.ExecutionContext.Workload == "" &&
			!strings.EqualFold(strings.TrimSpace(proposal.RuntimePreset), "provision")
	}
	if saved, savedErr := s.client.ProjectWorkload.Query().Where(projectworkload.SourceExperimentIDEQ(record.ID)).Only(ctx); savedErr == nil {
		view.SavedWorkload = saved.Name
		view.SavableWorkload = false
	} else if savedErr != nil && !ent.IsNotFound(savedErr) {
		return savedErr
	}

	attempts, err := s.client.Attempt.Query().Where(attempt.ExperimentIDEQ(record.ID)).Order(ent.Asc(attempt.FieldNumber)).All(ctx)
	if err != nil {
		return err
	}
	view.Attempts = make([]AttemptView, 0, len(attempts))
	view.Timeline = append(view.Timeline, TimelineEvent{At: record.CreatedAt, Code: "experiment.created"})
	for _, item := range attempts {
		view.Attempts = append(view.Attempts, makeAttemptView(item))
		view.Timeline = append(view.Timeline, TimelineEvent{At: item.CreatedAt, Code: "attempt.created", Detail: fmt.Sprintf("Attempt %d", item.Number)})
		if item.StartedAt != nil {
			view.Timeline = append(view.Timeline, TimelineEvent{At: *item.StartedAt, Code: "attempt.started", Detail: fmt.Sprintf("Attempt %d", item.Number)})
		}
		if item.FinishedAt != nil {
			detail := fmt.Sprintf("Attempt %d", item.Number)
			if item.FailureCode != nil {
				detail += ": " + *item.FailureCode
			}
			view.Timeline = append(view.Timeline, TimelineEvent{At: *item.FinishedAt, Code: "attempt.finished", Detail: detail})
		}
	}
	events, err := s.client.AuditEvent.Query().Where(
		auditevent.TenantIDEQ(record.TenantID), auditevent.TargetTypeEQ("experiment"), auditevent.TargetIDEQ(record.PublicID.String()),
	).Order(ent.Asc(auditevent.FieldCreatedAt), ent.Asc(auditevent.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return err
	}
	for _, event := range events {
		view.Timeline = append(view.Timeline, TimelineEvent{At: event.CreatedAt, Code: event.Action, Detail: timelineDetail(event.Metadata)})
		if runtimeInfo, ok := decodeRuntimeInfo(event.Metadata["runtime_info"], record.OutputPath); ok {
			view.ExecutionContext.RuntimeInfo = runtimeInfo
		}
	}
	if record.FinishedAt != nil {
		view.Timeline = append(view.Timeline, TimelineEvent{At: *record.FinishedAt, Code: "experiment.terminal", Detail: record.State})
	}
	sort.SliceStable(view.Timeline, func(left, right int) bool { return view.Timeline[left].At.Before(view.Timeline[right].At) })
	return s.enrichBackendObservation(ctx, record, view)
}

func (s *Service) enrichBackendObservation(ctx context.Context, record *ent.Experiment, view *View) error {
	if view.ExecutionContext.Backend == "ssh_cloud" {
		assignment, err := s.client.CloudSSHAssignment.Query().Where(cloudsshassignment.ExperimentIDEQ(record.ID)).WithNode().
			Order(ent.Desc(cloudsshassignment.FieldID)).First(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		node, _ := assignment.Edges.NodeOrErr()
		view.BackendObservation = &BackendObservationView{
			Kind: "ssh_cloud", ID: assignment.PublicID.String(), State: string(assignment.State), StopReason: pointerString(assignment.StopReason),
			LastError: pointerString(assignment.LastError), UpdatedAt: assignment.UpdatedAt, FinishedAt: assignment.FinishedAt,
		}
		if node != nil {
			view.BackendObservation.NodeLabel = node.Label
		}
		return nil
	}
	if view.ExecutionContext.Backend == "self_hosted" {
		assignment, err := s.client.NodeAssignment.Query().Where(nodeassignment.ExperimentIDEQ(record.ID)).WithNode().
			Order(ent.Desc(nodeassignment.FieldID)).First(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		cleanupComplete := false
		cleanupEvents, err := s.client.AuditEvent.Query().Where(
			auditevent.TenantIDEQ(record.TenantID), auditevent.ActionEQ("experiment.cleanup_complete"),
			auditevent.TargetTypeEQ("experiment"), auditevent.TargetIDEQ(record.PublicID.String()),
		).Order(ent.Desc(auditevent.FieldCreatedAt), ent.Desc(auditevent.FieldID)).Limit(20).All(ctx)
		if err != nil {
			return err
		}
		for _, event := range cleanupEvents {
			if metadataString(event.Metadata, "assignment_id") == assignment.PublicID.String() {
				cleanupComplete = true
				break
			}
		}
		node, _ := assignment.Edges.NodeOrErr()
		view.BackendObservation = &BackendObservationView{
			Kind: "self_hosted", ID: assignment.PublicID.String(), State: string(assignment.State), StopReason: pointerString(assignment.StopReason),
			LastError: pointerString(assignment.LastError), CleanupComplete: cleanupComplete, UpdatedAt: assignment.UpdatedAt,
			FinishedAt: assignment.FinishedAt,
		}
		if node != nil {
			view.BackendObservation.NodeLabel = node.Label
		}
		return nil
	}
	resource, err := s.client.ProviderResource.Query().Where(providerresource.ExperimentIDEQ(record.ID)).
		Order(ent.Desc(providerresource.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cleanup := resource.State == providerresource.StateDeleted || (resource.State == providerresource.StateError && resource.ProviderID == nil)
	view.BackendObservation = &BackendObservationView{
		Kind: view.ExecutionContext.Backend, ID: resource.PublicID.String(), ProviderID: pointerString(resource.ProviderID), State: string(resource.State),
		Status: pointerString(resource.ProviderStatus), StopReason: pointerString(resource.StopReason), LastError: pointerString(resource.LastError),
		CleanupComplete: cleanup, UpdatedAt: resource.UpdatedAt, FinishedAt: resource.DeletedAt,
	}
	return nil
}

func makeAttemptView(record *ent.Attempt) AttemptView {
	return AttemptView{
		ID: record.PublicID.String(), Number: record.Number, State: record.State,
		ProviderResourceID: record.ProviderResourceID, RetryReason: record.RetryReason,
		FailureCode: record.FailureCode, FailureReason: record.FailureReason,
		StartedAt: record.StartedAt, FinishedAt: record.FinishedAt,
		EstimatedCostMilli: record.EstimatedCostMilli, ExitCode: record.ExitCode,
		SourceDownloads: record.SourceDownloads, LogTail: record.LogTail, Metrics: record.Metrics,
		LastHeartbeatAt: record.LastHeartbeatAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func decodeRuntimeInfo(value any, outputPath string) (*executionmeta.RuntimeInfo, bool) {
	if value == nil {
		return nil, false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var runtimeInfo executionmeta.RuntimeInfo
	if err := json.Unmarshal(encoded, &runtimeInfo); err != nil {
		return nil, false
	}
	expected := outputPath
	if runtimeInfo.Source == "node_binding" {
		expected = "/outputs"
	}
	validated, err := executionmeta.Validate(runtimeInfo, expected)
	if err != nil {
		return nil, false
	}
	return &validated, true
}

func timelineDetail(metadata map[string]any) string {
	for _, key := range []string{"stage", "reason", "state"} {
		if value, ok := metadata[key].(string); ok && value != "" {
			detail := value
			if key == "stage" {
				if errorType, ok := metadata["error_type"].(string); ok && errorType != "" {
					detail += " (" + errorType + ")"
				}
			}
			return detail
		}
	}
	return ""
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
