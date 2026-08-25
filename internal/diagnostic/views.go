package diagnostic

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/diagnosticrun"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	entproject "github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/google/uuid"
)

func (s *Service) List(ctx context.Context, tenantID int, projectID string, limit int) (ListResult, error) {
	var result ListResult
	projectRecord, err := s.project(ctx, tenantID, projectID)
	if err != nil {
		return result, err
	}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > maxDiagnosticRuns {
		return result, invalid("limit must be between 1 and 100")
	}
	records, err := s.client.DiagnosticRun.Query().Where(
		diagnosticrun.ProjectIDEQ(projectRecord.ID),
	).WithExperiment().Order(ent.Desc(diagnosticrun.FieldCreatedAt)).Limit(limit).All(ctx)
	if err != nil {
		return result, err
	}
	result.Runs = make([]RunSummary, 0, len(records))
	for _, record := range records {
		view, err := s.runSummary(ctx, tenantID, projectRecord.PublicID.String(), record)
		if err != nil {
			return ListResult{}, err
		}
		result.Runs = append(result.Runs, view)
	}
	return result, nil
}

func (s *Service) runSummary(ctx context.Context, tenantID int, projectID string, record *ent.DiagnosticRun) (RunSummary, error) {
	var result RunSummary
	if record.TenantID != tenantID {
		return result, ErrNotFound
	}
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return result, err
	}
	experimentView := experiment.View{
		ID: experimentRecord.PublicID.String(), State: experimentRecord.State, DesiredState: experimentRecord.DesiredState,
		FailureCode: experimentRecord.FailureCode, FailureReason: experimentRecord.FailureReason, Metrics: experimentRecord.Metrics,
	}
	_, cleanupComplete, err := s.backendObservation(ctx, record.Backend, experimentRecord.ID, terminalExperiment(experimentRecord.State))
	if err != nil {
		return result, err
	}
	assessment := assess(experimentView, nil, cleanupComplete)
	assessment.Recommendations = nil
	return RunSummary{
		ID: record.PublicID.String(), ProjectID: projectID, Backend: string(record.Backend), Suite: string(record.Suite),
		RequestedBy: record.RequestedBy, Experiment: ExperimentSummary{ID: experimentRecord.PublicID.String(), State: experimentRecord.State},
		Assessment: assessment, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

func (s *Service) Get(ctx context.Context, tenantID int, projectID, runID string) (RunView, error) {
	var result RunView
	projectPublicID, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return result, ErrNotFound
	}
	runPublicID, err := uuid.Parse(strings.TrimSpace(runID))
	if err != nil {
		return result, ErrNotFound
	}
	record, err := s.client.DiagnosticRun.Query().Where(
		diagnosticrun.PublicIDEQ(runPublicID),
		diagnosticrun.HasProjectWith(entproject.PublicIDEQ(projectPublicID), entproject.TenantIDEQ(tenantID)),
	).WithProject().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	return s.runView(ctx, tenantID, projectPublicID.String(), record)
}

func (s *Service) runView(ctx context.Context, tenantID int, projectID string, input *ent.DiagnosticRun) (RunView, error) {
	var result RunView
	record, err := s.client.DiagnosticRun.Query().Where(
		diagnosticrun.IDEQ(input.ID), diagnosticrun.TenantIDEQ(tenantID),
	).WithExperiment().WithProject().Only(ctx)
	if err != nil {
		return result, err
	}
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return result, err
	}
	if s.experiments == nil {
		return result, fmt.Errorf("experiment query service is unavailable")
	}
	experimentView, err := s.experiments.OwnerGet(ctx, tenantID, projectID, experimentRecord.PublicID.String())
	if err != nil {
		return result, err
	}
	preflight, err := decodePreflight(record.Preflight)
	if err != nil {
		return result, err
	}
	attemptRecords, err := s.client.Attempt.Query().Where(
		attempt.ExperimentIDEQ(experimentRecord.ID),
	).Order(ent.Asc(attempt.FieldNumber)).All(ctx)
	if err != nil {
		return result, err
	}
	attempts := make([]AttemptObservation, 0, len(attemptRecords))
	for _, item := range attemptRecords {
		attempts = append(attempts, AttemptObservation{
			ID: item.PublicID.String(), Number: item.Number, State: item.State, SourceDownloads: item.SourceDownloads,
			ExitCode: item.ExitCode, LogTail: item.LogTail, Metrics: item.Metrics, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt,
			FailureCode: item.FailureCode, FailureReason: item.FailureReason,
		})
	}
	backendState, cleanupComplete, err := s.backendObservation(ctx, record.Backend, experimentRecord.ID, terminalExperiment(experimentRecord.State))
	if err != nil {
		return result, err
	}
	timeline, err := s.timeline(ctx, record, experimentRecord, attemptRecords)
	if err != nil {
		return result, err
	}
	assessment := assess(experimentView, attempts, cleanupComplete)
	return RunView{
		ID: record.PublicID.String(), ProjectID: projectID, Backend: string(record.Backend), Suite: string(record.Suite),
		RequestedBy: record.RequestedBy, Preflight: preflight, Experiment: experimentView, Assessment: assessment,
		Attempts: attempts, BackendState: backendState, Timeline: timeline, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

func decodePreflight(value map[string]any) (Preflight, error) {
	var result Preflight
	encoded, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) backendObservation(ctx context.Context, backend diagnosticrun.Backend, experimentID int, terminal bool) (*BackendObservation, bool, error) {
	if backend != diagnosticrun.BackendSelfHosted {
		record, err := s.client.ProviderResource.Query().Where(
			providerresource.ExperimentIDEQ(experimentID),
		).Order(ent.Desc(providerresource.FieldID)).First(ctx)
		if ent.IsNotFound(err) {
			return nil, terminal, nil
		}
		if err != nil {
			return nil, false, err
		}
		cleanup := record.State == providerresource.StateDeleted || (record.State == providerresource.StateError && record.ProviderID == nil)
		return &BackendObservation{
			Kind: string(backend), ID: record.PublicID.String(), State: string(record.State), Status: record.ProviderStatus,
			StopReason: record.StopReason, LastError: record.LastError, StopRequestedAt: record.StopRequestedAt, FinishedAt: record.DeletedAt,
		}, cleanup, nil
	}
	record, err := s.client.NodeAssignment.Query().Where(
		nodeassignment.ExperimentIDEQ(experimentID),
	).Order(ent.Desc(nodeassignment.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, terminal, nil
	}
	if err != nil {
		return nil, false, err
	}
	cleanup := terminalNodeAssignment(string(record.State))
	return &BackendObservation{
		Kind: BackendSelfHosted, ID: record.PublicID.String(), State: string(record.State), StopReason: record.StopReason,
		LastError: record.LastError, StopRequestedAt: record.StopRequestedAt, FinishedAt: record.FinishedAt,
	}, cleanup, nil
}

func (s *Service) timeline(ctx context.Context, run *ent.DiagnosticRun, experimentRecord *ent.Experiment, attempts []*ent.Attempt) ([]TimelineEvent, error) {
	result := []TimelineEvent{{At: run.CreatedAt, Code: "diagnostic_submitted"}, {At: experimentRecord.CreatedAt, Code: "experiment_created"}}
	for _, item := range attempts {
		result = append(result, TimelineEvent{At: item.CreatedAt, Code: "attempt_created", Detail: fmt.Sprintf("Attempt %d", item.Number)})
		if item.StartedAt != nil {
			result = append(result, TimelineEvent{At: *item.StartedAt, Code: "attempt_started", Detail: fmt.Sprintf("Attempt %d", item.Number)})
		}
		if item.FinishedAt != nil {
			detail := fmt.Sprintf("Attempt %d", item.Number)
			if item.FailureCode != nil {
				detail += ": " + *item.FailureCode
			}
			result = append(result, TimelineEvent{At: *item.FinishedAt, Code: "attempt_finished", Detail: detail})
		}
	}
	events, err := s.client.AuditEvent.Query().Where(
		auditevent.TenantIDEQ(experimentRecord.TenantID), auditevent.TargetTypeEQ("experiment"),
		auditevent.TargetIDEQ(experimentRecord.PublicID.String()),
	).Order(ent.Asc(auditevent.FieldCreatedAt), ent.Asc(auditevent.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		detail := ""
		if stage, ok := event.Metadata["stage"].(string); ok {
			detail = stage
			if errorType, ok := event.Metadata["error_type"].(string); ok && errorType != "" {
				detail += " (" + errorType + ")"
			}
		}
		result = append(result, TimelineEvent{At: event.CreatedAt, Code: event.Action, Detail: detail})
	}
	if experimentRecord.FinishedAt != nil {
		result = append(result, TimelineEvent{At: *experimentRecord.FinishedAt, Code: "experiment_terminal", Detail: experimentRecord.State})
	}
	sort.SliceStable(result, func(left, right int) bool { return result[left].At.Before(result[right].At) })
	return result, nil
}

func assess(view experiment.View, attempts []AttemptObservation, cleanupComplete bool) Assessment {
	result := Assessment{CleanupComplete: cleanupComplete}
	if !terminalExperiment(view.State) {
		result.Status = "running"
		switch view.State {
		case "queued":
			result.Classification = "waiting_for_scheduler"
			result.Summary = "Diagnostic is queued for an execution slot."
		case "provisioning":
			result.Classification = "backend_provisioning"
			result.Summary = "Backend provisioning is in progress."
			if view.RunnerStage != nil {
				result.Classification = "runner_bootstrap"
				result.Summary = "Runner bootstrap reached " + *view.RunnerStage + "."
			}
		case "running":
			result.Classification = "suite_running"
			result.Summary = "The built-in diagnostic suite is running on the GPU."
		default:
			result.Classification = "cleanup"
			result.Summary = "Diagnostic execution finished and managed cleanup is in progress."
		}
		return result
	}
	if view.State == "succeeded" && !cleanupComplete {
		result.Status = "running"
		result.Classification = "cleanup_pending"
		result.Summary = "Diagnostic execution is terminal, but managed backend cleanup is not complete."
		result.Recommendations = []string{"Do not retry until managed backend cleanup completes."}
		return result
	}
	if view.State == "succeeded" {
		if passed, _ := view.Metrics["diagnostic_passed"].(bool); passed {
			result.Status = "passed"
			result.Classification = "all_checks_passed"
			result.Summary = "The end-to-end backend diagnostic passed."
			return result
		}
		result.Status = "failed"
		result.Classification = "invalid_diagnostic_result"
		result.Summary = "Workload exited successfully but did not return a passing diagnostic result."
		result.Recommendations = []string{"Inspect metrics.json and the latest Attempt log tail."}
		return result
	}
	result.Status = "failed"
	result.Classification = value(view.FailureCode, "diagnostic_failed")
	result.Summary = value(view.FailureReason, "The backend diagnostic failed.")
	errorType, _ := view.Metrics["error_type"].(string)
	stage := value(view.RunnerStage, "")
	switch {
	case strings.Contains(strings.ToLower(errorType), "torch") || errorType == "ModuleNotFoundError" || errorType == "ImportError":
		result.Recommendations = []string{"Use an image that contains a CUDA-compatible PyTorch build.", "Run gpu_connectivity first to separate image framework errors from GPU injection errors."}
	case errorType == "unexpected_gpu_count":
		result.Recommendations = []string{"Verify the backend injected exactly the GPU count requested by the Resource Profile.", "Inspect nvidia-smi output and the latest Attempt log tail."}
	case strings.HasPrefix(errorType, "nvidia_smi") || errorType == "no_visible_gpu":
		result.Recommendations = []string{"Verify NVIDIA driver visibility and container GPU injection for the selected backend.", "For Self-hosted Nodes, check Docker and NVIDIA Container Toolkit before retrying."}
	case strings.Contains(strings.ToLower(errorType), "cuda") || strings.Contains(strings.ToLower(result.Summary), "cuda"):
		result.Recommendations = []string{"Verify image CUDA compatibility with the selected GPU driver.", "Inspect nvidia-smi output and the latest Attempt log tail."}
	case strings.Contains(stage, "source"):
		result.Recommendations = []string{"Inspect source archive and transfer checks in the preflight and timeline.", "Check the public Runner route for interrupted response bodies."}
	case strings.Contains(stage, "started_callback"):
		result.Recommendations = []string{"Check the public Runner event route, Cloudflare edge logs, and callback reachability."}
	case result.Classification == "node_lost" || result.Classification == "node_unavailable":
		result.Recommendations = []string{"Check gemcp-node service health, outbound HTTPS, Docker, and NVIDIA Container Toolkit on the selected Node."}
	case result.Classification == "provision_timeout":
		result.Recommendations = []string{"Use the Runner stage timeline to identify the last completed bootstrap phase.", "Inspect gemcp-launch.log when shared storage is available."}
	default:
		result.Recommendations = []string{"Inspect the latest Attempt log tail, backend observation, and timeline before retrying."}
	}
	if !cleanupComplete {
		result.Recommendations = append(result.Recommendations, "Do not retry until managed backend cleanup completes.")
	}
	if len(attempts) == 0 {
		result.Recommendations = append(result.Recommendations, "No Attempt was created; inspect scheduler, budget, and backend availability checks.")
	}
	return result
}

func terminalExperiment(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "timed_out", "budget_stopped", "provider_error":
		return true
	default:
		return false
	}
}

func terminalNodeAssignment(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "lost":
		return true
	default:
		return false
	}
}

func value(pointer *string, fallback string) string {
	if pointer == nil || strings.TrimSpace(*pointer) == "" {
		return fallback
	}
	return *pointer
}
