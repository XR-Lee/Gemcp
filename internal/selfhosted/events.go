package selfhosted

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/internal/executionmeta"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
)

const (
	maxLogTailBytes = 64 << 10
	maxMetricsBytes = 64 << 10
)

func (s *Service) ProjectNodeEvent(ctx context.Context, tx *ent.Tx, node *ent.SelfHostedNode, event nodeprotocol.Event, now time.Time) error {
	switch event.Kind {
	case "workload_started", "workload_heartbeat", "workload_finished", "workload_cleanup_complete":
	default:
		return nil
	}
	assignmentID, err := uuid.Parse(payloadString(event.Payload, "assignment_id"))
	if err != nil {
		return ErrInvalidEvent
	}
	assignment, err := tx.NodeAssignment.Query().Where(
		nodeassignment.PublicIDEQ(assignmentID), nodeassignment.NodeIDEQ(node.ID),
	).WithAttempt().WithExperiment().Only(ctx)
	if ent.IsNotFound(err) {
		return ErrInvalidEvent
	}
	if err != nil {
		return err
	}
	attempt, err := assignment.Edges.AttemptOrErr()
	if err != nil {
		return err
	}
	experiment, err := assignment.Edges.ExperimentOrErr()
	if err != nil {
		return err
	}
	switch event.Kind {
	case "workload_started":
		return s.projectStarted(ctx, tx, assignment, attempt, experiment, event, now)
	case "workload_heartbeat":
		return s.projectHeartbeat(ctx, tx, assignment, attempt, experiment, event, now)
	case "workload_finished":
		return s.projectFinished(ctx, tx, assignment, attempt, experiment, event, now)
	case "workload_cleanup_complete":
		return s.projectCleanup(ctx, tx, assignment, experiment, event, now)
	default:
		return nil
	}
}

func (s *Service) projectCleanup(ctx context.Context, tx *ent.Tx, assignment *ent.NodeAssignment, experiment *ent.Experiment, event nodeprotocol.Event, now time.Time) error {
	workloadID := strings.TrimSpace(payloadString(event.Payload, "workload_id"))
	if !assignmentTerminal(assignment.State) || assignment.WorkloadID == nil || workloadID == "" || len(workloadID) > 255 || *assignment.WorkloadID != workloadID {
		return ErrInvalidEvent
	}
	_, err := tx.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("self_hosted_node").SetActorID(fmt.Sprintf("node:%d", assignment.NodeID)).
		SetAction("experiment.cleanup_complete").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
		SetMetadata(map[string]any{"assignment_id": assignment.PublicID.String(), "workload_id": workloadID, "recorded_at": now}).Save(ctx)
	return err
}

func (s *Service) projectStarted(ctx context.Context, tx *ent.Tx, assignment *ent.NodeAssignment, attempt *ent.Attempt, experiment *ent.Experiment, event nodeprotocol.Event, now time.Time) error {
	if assignmentTerminal(assignment.State) {
		return nil
	}
	workloadID := strings.TrimSpace(payloadString(event.Payload, "workload_id"))
	if workloadID == "" || len(workloadID) > 255 {
		return ErrInvalidEvent
	}
	if assignment.State == nodeassignment.StateRunning {
		if assignment.WorkloadID != nil && *assignment.WorkloadID != workloadID {
			return ErrInvalidEvent
		}
		return s.projectHeartbeat(ctx, tx, assignment, attempt, experiment, event, now)
	}
	runtimeInfo, err := runtimeInfoFromPayload(event.Payload)
	if err != nil {
		return ErrInvalidEvent
	}
	deadline := now.Add(time.Duration(experiment.MaxRuntimeSeconds) * time.Second)
	hardDeadline := deadline.Add(time.Duration(experiment.TimeoutExtensionSeconds+experiment.TerminationGraceSeconds)*time.Second + 30*time.Second)
	if _, err := assignment.Update().SetState(nodeassignment.StateRunning).SetWorkloadID(workloadID).
		SetStartedAt(now).SetLastHeartbeatAt(now).SetDeadlineAt(deadline).SetHardDeadlineAt(hardDeadline).ClearLastError().Save(ctx); err != nil {
		return err
	}
	attemptUpdate := tx.Attempt.UpdateOneID(attempt.ID).SetState("running").SetLastHeartbeatAt(now)
	if attempt.StartedAt == nil {
		attemptUpdate.SetStartedAt(now)
	}
	if _, err := attemptUpdate.Save(ctx); err != nil {
		return err
	}
	if experiment.DesiredState == "running" && experiment.State == "provisioning" {
		if _, err := tx.Experiment.UpdateOneID(experiment.ID).SetState("running").SetStartedAt(now).SetDeadlineAt(deadline).
			SetProviderStatus("running").Save(ctx); err != nil {
			return err
		}
	}
	metadata := map[string]any{"attempt_id": attempt.PublicID.String(), "assignment_id": assignment.PublicID.String(), "deadline_at": deadline}
	if runtimeInfo != nil {
		metadata["runtime_info"] = runtimeInfo
	}
	_, err = tx.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("self_hosted_node").SetActorID(fmt.Sprintf("node:%d", assignment.NodeID)).
		SetAction("experiment.started").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
		SetMetadata(metadata).Save(ctx)
	return err
}

func (s *Service) projectHeartbeat(ctx context.Context, tx *ent.Tx, assignment *ent.NodeAssignment, attempt *ent.Attempt, experiment *ent.Experiment, event nodeprotocol.Event, now time.Time) error {
	if assignmentTerminal(assignment.State) {
		return nil
	}
	logTail, metrics, err := liveOutputFromPayload(event.Payload)
	if err != nil {
		return ErrInvalidEvent
	}
	assignmentUpdate := assignment.Update().SetLastHeartbeatAt(now)
	attemptUpdate := tx.Attempt.UpdateOneID(attempt.ID).SetLastHeartbeatAt(now)
	experimentUpdate := tx.Experiment.UpdateOneID(experiment.ID)
	if logTail != "" {
		assignmentUpdate.SetLogTail(logTail)
		attemptUpdate.SetLogTail(logTail)
		experimentUpdate.SetLogTail(logTail)
	}
	if len(metrics) > 0 {
		assignmentUpdate.SetMetrics(metrics)
		attemptUpdate.SetMetrics(metrics)
		experimentUpdate.SetMetrics(metrics)
	}
	if _, err := assignmentUpdate.Save(ctx); err != nil {
		return err
	}
	if _, err := attemptUpdate.Save(ctx); err != nil {
		return err
	}
	if logTail != "" || len(metrics) > 0 {
		if _, err := experimentUpdate.Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) projectFinished(ctx context.Context, tx *ent.Tx, assignment *ent.NodeAssignment, attempt *ent.Attempt, experiment *ent.Experiment, event nodeprotocol.Event, now time.Time) error {
	if assignmentTerminal(assignment.State) {
		return nil
	}
	exitCode, ok := payloadInteger(event.Payload, "exit_code")
	reason := strings.TrimSpace(payloadString(event.Payload, "reason"))
	logTail := payloadString(event.Payload, "log_tail")
	metrics, _ := event.Payload["metrics"].(map[string]any)
	if metrics == nil {
		metrics = map[string]any{}
	}
	encodedMetrics, metricsErr := json.Marshal(metrics)
	if !ok || exitCode < 0 || exitCode > 255 || !validFinishReason(reason) || !utf8.ValidString(logTail) || len([]byte(logTail)) > maxLogTailBytes || metricsErr != nil || len(encodedMetrics) > maxMetricsBytes {
		return ErrInvalidEvent
	}
	finalState, attemptState, failureCode, failureReason := selfHostedResult(experiment, exitCode, reason)
	assignmentState := nodeassignment.State(finalState)
	if finalState == "timed_out" {
		assignmentState = nodeassignment.StateTimedOut
	}
	assignmentUpdate := assignment.Update().SetState(assignmentState).SetFinishedAt(now).SetLastHeartbeatAt(now).
		SetExitCode(exitCode).SetLogTail(logTail).SetMetrics(metrics).ClearLastError()
	if failureCode != "" {
		assignmentUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := assignmentUpdate.Save(ctx); err != nil {
		return err
	}
	attemptUpdate := tx.Attempt.UpdateOneID(attempt.ID).SetState(attemptState).SetFinishedAt(now).SetLastHeartbeatAt(now).
		SetExitCode(exitCode).SetLogTail(logTail).SetMetrics(metrics).SetEstimatedCostMilli(0)
	if failureCode != "" {
		attemptUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := attemptUpdate.Save(ctx); err != nil {
		return err
	}
	experimentUpdate := tx.Experiment.UpdateOneID(experiment.ID).SetState(finalState).SetFinishedAt(now).SetExitCode(exitCode).
		SetLogTail(logTail).SetMetrics(metrics).SetEstimatedCostMilli(0).SetBudgetFinalizedAt(now).SetProviderStatus("finished").
		ClearLeaseOwner().ClearLeaseExpiresAt()
	if failureCode != "" {
		experimentUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := experimentUpdate.Save(ctx); err != nil {
		return err
	}
	if err := releaseReservation(ctx, tx, experiment); err != nil {
		return err
	}
	_, err := tx.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("self_hosted_node").SetActorID(fmt.Sprintf("node:%d", assignment.NodeID)).
		SetAction("experiment.finalized").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
		SetMetadata(map[string]any{"state": finalState, "attempt_id": attempt.PublicID.String(), "assignment_id": assignment.PublicID.String(), "exit_code": exitCode}).Save(ctx)
	return err
}

func releaseReservation(ctx context.Context, tx *ent.Tx, experiment *ent.Experiment) error {
	reservation, err := tx.BudgetEntry.Query().Where(
		budgetentry.ExperimentIDEQ(experiment.ID), budgetentry.KindEQ("reservation"),
	).Order(ent.Desc(budgetentry.FieldID)).First(ctx)
	if err != nil {
		return err
	}
	released, err := tx.BudgetEntry.Query().Where(
		budgetentry.ExperimentIDEQ(experiment.ID), budgetentry.KindEQ("release"), budgetentry.PeriodEQ(reservation.Period),
	).Exist(ctx)
	if err != nil || released {
		return err
	}
	_, err = tx.BudgetEntry.Create().SetTenantID(experiment.TenantID).SetProjectID(experiment.ProjectID).
		SetExperimentID(experiment.ID).SetPeriod(reservation.Period).SetKind("release").SetAmountMilli(-experiment.ReservedCostMilli).
		SetDescription("unmetered Self-hosted completion release").Save(ctx)
	return err
}

func runtimeInfoFromPayload(payload map[string]any) (*executionmeta.RuntimeInfo, error) {
	raw, exists := payload["runtime_info"]
	if !exists || raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var runtimeInfo executionmeta.RuntimeInfo
	if err := json.Unmarshal(encoded, &runtimeInfo); err != nil {
		return nil, err
	}
	validated, err := executionmeta.Validate(runtimeInfo, "/outputs")
	if err != nil {
		return nil, err
	}
	return &validated, nil
}

func liveOutputFromPayload(payload map[string]any) (string, map[string]any, error) {
	logTail := ""
	if raw, exists := payload["log_tail"]; exists {
		value, ok := raw.(string)
		if !ok || !utf8.ValidString(value) || len([]byte(value)) > maxLogTailBytes {
			return "", nil, ErrInvalidEvent
		}
		logTail = value
	}
	metrics := map[string]any{}
	if raw, exists := payload["metrics"]; exists {
		value, ok := raw.(map[string]any)
		if !ok {
			return "", nil, ErrInvalidEvent
		}
		encoded, err := json.Marshal(value)
		if err != nil || len(encoded) > maxMetricsBytes {
			return "", nil, ErrInvalidEvent
		}
		metrics = value
	}
	return logTail, metrics, nil
}

func assignmentTerminal(state nodeassignment.State) bool {
	switch state {
	case nodeassignment.StateSucceeded, nodeassignment.StateFailed, nodeassignment.StateCancelled, nodeassignment.StateTimedOut, nodeassignment.StateLost:
		return true
	default:
		return false
	}
}

func payloadInteger(payload map[string]any, key string) (int, bool) {
	switch value := payload[key].(type) {
	case int:
		return value, true
	case float64:
		if value < 0 || value > 255 || value != float64(int(value)) {
			return 0, false
		}
		return int(value), true
	default:
		return 0, false
	}
}

func validFinishReason(reason string) bool {
	switch reason {
	case "completed", "cancelled", "timeout", "emergency", "runner_error", "oom":
		return true
	default:
		return false
	}
}

func selfHostedResult(experiment *ent.Experiment, exitCode int, reason string) (string, string, string, string) {
	if experiment.DesiredState == "cancelled" || reason == "cancelled" || reason == "emergency" {
		code := "cancelled"
		message := "experiment was cancelled by an authorized request"
		if reason == "emergency" || (experiment.FailureCode != nil && *experiment.FailureCode == "emergency_stop") {
			code, message = "emergency_stop", "experiment was stopped by the Owner emergency action"
		}
		return "cancelled", "cancelled", code, message
	}
	switch reason {
	case "timeout":
		return "timed_out", "failed", "runtime_timeout", "experiment exceeded its extended runtime deadline"
	case "runner_error":
		return "failed", "failed", "node_runner_error", "the node failed while supervising the workload"
	case "oom":
		return "failed", "failed", "oom", "the workload was terminated after exhausting GPU or host memory"
	}
	if exitCode == 0 {
		return "succeeded", "succeeded", "", ""
	}
	return "failed", "failed", "command_failed", fmt.Sprintf("experiment command exited with code %d", exitCode)
}
