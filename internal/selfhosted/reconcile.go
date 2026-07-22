package selfhosted

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodecommand"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
)

func (s *Service) Reconcile(ctx context.Context, experimentID int, now time.Time) (bool, error) {
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return true, err
	}
	defer tx.Rollback()
	assignment, err := tx.NodeAssignment.Query().Where(nodeassignment.ExperimentIDEQ(experimentID)).
		Order(ent.Desc(nodeassignment.FieldCreatedAt), ent.Desc(nodeassignment.FieldID)).
		WithAttempt().WithExperiment().WithNode().First(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	experiment, err := assignment.Edges.ExperimentOrErr()
	if err != nil {
		return true, err
	}
	attempt, err := assignment.Edges.AttemptOrErr()
	if err != nil {
		return true, err
	}
	node, err := assignment.Edges.NodeOrErr()
	if err != nil {
		return true, err
	}
	if assignmentTerminal(assignment.State) || experiment.BudgetFinalizedAt != nil {
		return true, tx.Commit()
	}

	lastSeen := assignment.CreatedAt
	if node.LastSeenAt != nil {
		lastSeen = *node.LastSeenAt
	}
	if !now.Before(lastSeen.Add(s.config.NodeLostAfter)) {
		if node.ObservedState != selfhostednode.ObservedStateLost {
			if _, err := tx.SelfHostedNode.UpdateOneID(node.ID).SetObservedState(selfhostednode.ObservedStateLost).Save(ctx); err != nil {
				return true, err
			}
		}
		reason := "Self-hosted node did not synchronize for 30 minutes"
		if err := s.retryInfrastructure(ctx, tx, assignment, attempt, experiment, nodeassignment.StateLost, "node_lost", reason, now); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	if !now.Before(lastSeen.Add(s.config.NodeStaleAfter)) && node.ObservedState == selfhostednode.ObservedStateOnline {
		if _, err := tx.SelfHostedNode.UpdateOneID(node.ID).SetObservedState(selfhostednode.ObservedStateUnavailable).Save(ctx); err != nil {
			return true, err
		}
	}

	startCommand, err := tx.NodeCommand.Query().Where(
		nodecommand.AssignmentIDEQ(assignment.ID), nodecommand.KindEQ("start_workload"),
	).Order(ent.Desc(nodecommand.FieldID)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return true, err
	}
	if err == nil && startCommand.Status == nodecommand.StatusFailed && assignment.State == nodeassignment.StateStarting {
		reason := startCommand.LastError
		if reason == nil || *reason == "" {
			value := "node rejected the workload start command"
			reason = &value
		}
		if err := s.retryInfrastructure(ctx, tx, assignment, attempt, experiment, nodeassignment.StateFailed, "node_start_failed", *reason, now); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}

	if experiment.DesiredState == "cancelled" {
		reason := "cancelled"
		if experiment.FailureCode != nil && *experiment.FailureCode == "emergency_stop" {
			reason = "emergency"
		}
		if err := s.requestStop(ctx, tx, assignment, experiment, reason, now); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	if assignment.State == nodeassignment.StateStarting && assignment.HardDeadlineAt != nil && !now.Before(*assignment.HardDeadlineAt) {
		if err := s.retryInfrastructure(ctx, tx, assignment, attempt, experiment, nodeassignment.StateFailed, "provision_timeout", "node workload did not start before the provisioning deadline", now); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	if assignment.State == nodeassignment.StateRunning && assignment.DeadlineAt != nil && !now.Before(*assignment.DeadlineAt) {
		if experiment.TimeoutExtendedAt == nil && experiment.TimeoutExtensionSeconds > 0 {
			deadline := assignment.DeadlineAt.Add(time.Duration(experiment.TimeoutExtensionSeconds) * time.Second)
			hardDeadline := deadline.Add(time.Duration(experiment.TerminationGraceSeconds)*time.Second + 30*time.Second)
			if _, err := tx.NodeAssignment.UpdateOneID(assignment.ID).SetDeadlineAt(deadline).SetHardDeadlineAt(hardDeadline).Save(ctx); err != nil {
				return true, err
			}
			if _, err := tx.Experiment.UpdateOneID(experiment.ID).SetDeadlineAt(deadline).SetTimeoutExtendedAt(now).Save(ctx); err != nil {
				return true, err
			}
			if _, err := tx.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
				SetAction("experiment.timeout_extended").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
				SetMetadata(map[string]any{"deadline_at": deadline, "extension_seconds": experiment.TimeoutExtensionSeconds}).Save(ctx); err != nil {
				return true, err
			}
			return true, tx.Commit()
		}
		if err := s.requestStop(ctx, tx, assignment, experiment, "timeout", now); err != nil {
			return true, err
		}
	}
	return true, tx.Commit()
}

func (s *Service) requestStop(ctx context.Context, tx *ent.Tx, assignment *ent.NodeAssignment, experiment *ent.Experiment, reason string, now time.Time) error {
	payload := map[string]any{
		"assignment_id": assignment.PublicID.String(), "reason": reason,
		"termination_grace_seconds": experiment.TerminationGraceSeconds,
	}
	if assignment.WorkloadID != nil {
		payload["workload_id"] = *assignment.WorkloadID
	}
	command, err := enqueueCommand(ctx, tx, experiment.TenantID, assignment.NodeID, assignment.ID, "stop_workload", "stop:"+assignment.PublicID.String()+":"+reason, payload, now)
	if err != nil {
		return err
	}
	assignmentUpdate := assignment.Update().SetState(nodeassignment.StateStopping).SetStopCommandID(command.PublicID.String())
	if assignment.StopRequestedAt == nil {
		assignmentUpdate.SetStopRequestedAt(now).SetStopReason(reason)
	}
	if _, err := assignmentUpdate.Save(ctx); err != nil {
		return err
	}
	if experiment.State != "cancelling" {
		_, err = tx.Experiment.UpdateOneID(experiment.ID).SetState("cancelling").Save(ctx)
	}
	return err
}

func (s *Service) retryInfrastructure(ctx context.Context, tx *ent.Tx, assignment *ent.NodeAssignment, attempt *ent.Attempt, experiment *ent.Experiment, assignmentState nodeassignment.State, code, reason string, now time.Time) error {
	_, err := enqueueCommand(ctx, tx, experiment.TenantID, assignment.NodeID, assignment.ID, "stop_workload", "stop:"+assignment.PublicID.String()+":provider_error", map[string]any{
		"assignment_id": assignment.PublicID.String(), "reason": "provider_error", "termination_grace_seconds": experiment.TerminationGraceSeconds,
	}, now)
	if err != nil {
		return err
	}
	if _, err := tx.NodeAssignment.UpdateOneID(assignment.ID).SetState(assignmentState).SetFinishedAt(now).
		SetFailureCode(code).SetFailureReason(reason).SetLastError(reason).Save(ctx); err != nil {
		return err
	}
	if experiment.DesiredState == "cancelled" {
		if _, err := tx.Attempt.UpdateOneID(attempt.ID).SetState("cancelled").SetFinishedAt(now).SetFailureCode("cancelled").SetFailureReason("experiment was cancelled").Save(ctx); err != nil {
			return err
		}
		if _, err := tx.Experiment.UpdateOneID(experiment.ID).SetState("cancelled").SetFinishedAt(now).SetBudgetFinalizedAt(now).
			SetFailureCode("cancelled").SetFailureReason("experiment was cancelled").ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx); err != nil {
			return err
		}
		return releaseReservation(ctx, tx, experiment)
	}
	if _, err := tx.Attempt.UpdateOneID(attempt.ID).SetState("failed").SetFinishedAt(now).SetRetryReason(code).
		SetFailureCode(code).SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if attempt.Number < s.config.MaxAttempts {
		backoff := time.Duration(attempt.Number*30) * time.Second
		if _, err := tx.Experiment.UpdateOneID(experiment.ID).SetState("queued").SetNextAttemptAt(now.Add(backoff)).
			ClearProviderResourceID().ClearProviderStatus().ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx); err != nil {
			return err
		}
		_, err = tx.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
			SetAction("experiment.retry_scheduled").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
			SetMetadata(map[string]any{"attempt_id": attempt.PublicID.String(), "attempt_number": attempt.Number, "retry_at": now.Add(backoff), "reason": code}).Save(ctx)
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(experiment.ID).SetState("provider_error").SetFinishedAt(now).SetBudgetFinalizedAt(now).
		SetFailureCode(code).SetFailureReason(reason).ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx); err != nil {
		return err
	}
	if err := releaseReservation(ctx, tx, experiment); err != nil {
		return err
	}
	_, err = tx.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
		SetAction("experiment.finalized").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
		SetMetadata(map[string]any{"state": "provider_error", "attempt_id": attempt.PublicID.String(), "failure_code": code}).Save(ctx)
	return err
}

func (s *Service) IsSelfHostedProfile(ctx context.Context, tx *ent.Tx, profileID int) (bool, error) {
	profile, err := tx.ResourceProfile.Query().Where(resourceprofile.IDEQ(profileID)).Only(ctx)
	if err != nil {
		return false, err
	}
	return profile.Backend == resourceprofile.BackendSelfHosted, nil
}

func (s *Service) Enabled() bool { return s != nil && s.config.Enabled }

func (s *Service) String() string {
	return fmt.Sprintf("SelfHosted(enabled=%t)", s.Enabled())
}
