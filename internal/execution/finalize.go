package execution

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/notification"
)

const shutdownObservationMargin = 30 * time.Second

func (e *Engine) retryAttempt(ctx context.Context, experimentID, resourceID int, code, reason string, now time.Time) error {
	tx, err := e.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	experimentRecord, err := tx.Experiment.Get(ctx, experimentID)
	if err != nil {
		return err
	}
	resourceRecord, err := tx.ProviderResource.Query().Where(providerresource.IDEQ(resourceID)).WithAttempt().Only(ctx)
	if err != nil {
		return err
	}
	attemptRecord, err := resourceRecord.Edges.AttemptOrErr()
	if err != nil {
		return err
	}
	if experimentRecord.DesiredState == "cancelled" {
		if _, err := tx.Attempt.UpdateOneID(attemptRecord.ID).SetState("cancelled").SetFinishedAt(now).
			SetRunnerTokenExpiresAt(now).ClearRunnerTokenHash().ClearRunnerTokenCiphertext().Save(ctx); err != nil {
			return err
		}
		if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetState(providerresource.StateDeleted).SetDeletedAt(now).
			SetStopRequestedAt(now).SetStopReason("cancelled").Save(ctx); err != nil {
			return err
		}
		if _, err := tx.Experiment.UpdateOneID(experimentID).SetState("cancelling").Save(ctx); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return e.finalize(ctx, experimentID, resourceID)
	}
	if _, err := tx.Attempt.UpdateOneID(attemptRecord.ID).SetState("failed").SetFinishedAt(now).SetRunnerTokenExpiresAt(now).
		ClearRunnerTokenHash().ClearRunnerTokenCiphertext().SetFailureCode(code).SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetState(providerresource.StateError).SetLastError(reason).Save(ctx); err != nil {
		return err
	}
	if attemptRecord.Number < e.config.MaxAttempts {
		backoff := time.Duration(attemptRecord.Number*30) * time.Second
		if _, err := tx.Experiment.UpdateOneID(experimentID).SetState("queued").SetNextAttemptAt(now.Add(backoff)).
			ClearLeaseOwner().ClearLeaseExpiresAt().ClearProviderResourceID().ClearProviderStatus().Save(ctx); err != nil {
			return err
		}
		if _, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID(e.config.InstanceID).
			SetAction("experiment.retry_scheduled").SetTargetType("experiment").SetTargetID(experimentRecord.PublicID.String()).
			SetMetadata(map[string]any{"attempt_id": attemptRecord.PublicID.String(), "attempt_number": attemptRecord.Number, "retry_at": now.Add(backoff), "reason": code}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetStopRequestedAt(now).SetStopReason("provider_error").Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(experimentID).SetState("collecting").SetFailureCode(code).SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return e.finalize(ctx, experimentID, resourceID)
}

func (e *Engine) failProvider(ctx context.Context, experimentID, resourceID int, code, reason string) error {
	reason = safeError(fmt.Errorf("%s", reason))
	now := e.now().UTC()
	tx, err := e.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	resourceRecord, err := tx.ProviderResource.Query().Where(providerresource.IDEQ(resourceID)).WithAttempt().Only(ctx)
	if err != nil {
		return err
	}
	attemptRecord, err := resourceRecord.Edges.AttemptOrErr()
	if err != nil {
		return err
	}
	if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetState(providerresource.StateError).SetStopRequestedAt(now).
		SetStopReason("provider_error").SetLastError(reason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Attempt.UpdateOneID(attemptRecord.ID).SetState("failed").SetFinishedAt(now).SetRunnerTokenExpiresAt(now).
		ClearRunnerTokenHash().ClearRunnerTokenCiphertext().SetFailureCode(code).SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(experimentID).SetState("collecting").SetFailureCode(code).SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return e.finalize(ctx, experimentID, resourceID)
}

func (e *Engine) markMissingCallback(ctx context.Context, experimentRecord *ent.Experiment, attemptRecord *ent.Attempt, resourceRecord *ent.ProviderResource, failureCode, failureReason, stopReason string, now time.Time) error {
	tx, err := e.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Attempt.UpdateOneID(attemptRecord.ID).SetState("failed").SetFinishedAt(now).SetRunnerTokenExpiresAt(now).
		ClearRunnerTokenHash().ClearRunnerTokenCiphertext().SetFailureCode(failureCode).SetFailureReason(failureReason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(experimentRecord.ID).SetState("collecting").SetFailureCode(failureCode).SetFailureReason(failureReason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.ProviderResource.UpdateOneID(resourceRecord.ID).SetStopRequestedAt(now).SetStopReason(stopReason).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (e *Engine) markProvisionTimeout(ctx context.Context, experimentRecord *ent.Experiment, attemptRecord *ent.Attempt, resourceRecord *ent.ProviderResource, now time.Time) error {
	tx, err := e.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	reason := "Runner did not start before the provisioning deadline; inspect gemcp-launch.log in the durable output path if present"
	if _, err := tx.Attempt.UpdateOneID(attemptRecord.ID).SetState("failed").SetFinishedAt(now).SetRunnerTokenExpiresAt(now).
		ClearRunnerTokenHash().ClearRunnerTokenCiphertext().SetFailureCode("provision_timeout").SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(experimentRecord.ID).SetState("cancelling").SetFailureCode("provision_timeout").SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.ProviderResource.UpdateOneID(resourceRecord.ID).SetStopRequestedAt(now).SetStopReason("provision_timeout").Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (e *Engine) enforceDeadline(ctx context.Context, experimentID, resourceID int, now time.Time) error {
	tx, err := e.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	experimentRecord, err := tx.Experiment.Get(ctx, experimentID)
	if err != nil {
		return err
	}
	resourceRecord, err := tx.ProviderResource.Get(ctx, resourceID)
	if err != nil {
		return err
	}
	if experimentRecord.DesiredState == "cancelled" {
		if resourceRecord.StopRequestedAt == nil {
			if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetStopRequestedAt(now).SetStopReason("cancelled").Save(ctx); err != nil {
				return err
			}
		}
		if experimentRecord.State != "collecting" {
			if _, err := tx.Experiment.UpdateOneID(experimentID).SetState("cancelling").Save(ctx); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	if experimentRecord.DeadlineAt == nil || now.Before(*experimentRecord.DeadlineAt) {
		return tx.Commit()
	}
	if experimentRecord.TimeoutExtendedAt == nil && experimentRecord.TimeoutExtensionSeconds > 0 {
		deadline := experimentRecord.DeadlineAt.Add(time.Duration(experimentRecord.TimeoutExtensionSeconds) * time.Second)
		if _, err := tx.Experiment.UpdateOneID(experimentID).SetDeadlineAt(deadline).SetTimeoutExtendedAt(now).Save(ctx); err != nil {
			return err
		}
		if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetHardDeadlineAt(deadline.Add(time.Duration(experimentRecord.TerminationGraceSeconds)*time.Second + shutdownObservationMargin)).Save(ctx); err != nil {
			return err
		}
		if _, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID(e.config.InstanceID).
			SetAction("experiment.timeout_extended").SetTargetType("experiment").SetTargetID(experimentRecord.PublicID.String()).
			SetMetadata(map[string]any{"deadline_at": deadline, "extension_seconds": experimentRecord.TimeoutExtensionSeconds}).Save(ctx); err != nil {
			return err
		}
		if err := notification.Enqueue(ctx, tx.Notification, notification.EnqueueInput{
			TenantID: experimentRecord.TenantID, DedupKey: "timeout-extended:" + experimentRecord.PublicID.String(),
			Kind: "timeout_extended", Severity: "warning", Subject: "[Gemcp] Experiment runtime extended",
			Body: fmt.Sprintf("Experiment %s reached its initial deadline. The extended execution deadline is now %s.", experimentRecord.PublicID.String(), deadline.Format(time.RFC3339)),
		}); err != nil {
			return err
		}
		if now.Before(deadline) {
			return tx.Commit()
		}
	}
	if _, err := tx.Experiment.UpdateOneID(experimentID).SetState("cancelling").Save(ctx); err != nil {
		return err
	}
	if resourceRecord.StopRequestedAt == nil {
		if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetStopRequestedAt(now).SetStopReason("timeout").Save(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (e *Engine) finalize(ctx context.Context, experimentID, resourceID int) error {
	tx, err := e.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	experimentRecord, err := tx.Experiment.Get(ctx, experimentID)
	if err != nil {
		return err
	}
	if experimentRecord.BudgetFinalizedAt != nil {
		return tx.Commit()
	}
	resourceRecord, err := tx.ProviderResource.Query().Where(providerresource.IDEQ(resourceID)).WithAttempt().Only(ctx)
	if err != nil {
		return err
	}
	attemptRecord, err := resourceRecord.Edges.AttemptOrErr()
	if err != nil {
		return err
	}
	now := e.now().UTC()
	finalState, attemptState, failureCode, failureReason := terminalResult(experimentRecord, attemptRecord, resourceRecord)
	estimated := estimateResourceCost(experimentRecord, resourceRecord, now)
	if estimated > experimentRecord.ReservedCostMilli {
		estimated = experimentRecord.ReservedCostMilli
	}
	reservation, err := tx.BudgetEntry.Query().Where(
		budgetentry.ExperimentIDEQ(experimentID), budgetentry.KindEQ("reservation"),
	).Order(ent.Desc(budgetentry.FieldID)).First(ctx)
	if err != nil {
		return fmt.Errorf("load reservation for finalization: %w", err)
	}
	released, err := tx.BudgetEntry.Query().Where(
		budgetentry.ExperimentIDEQ(experimentID), budgetentry.KindEQ("release"), budgetentry.PeriodEQ(reservation.Period),
	).Exist(ctx)
	if err != nil {
		return err
	}
	if !released {
		if _, err := tx.BudgetEntry.Create().SetTenantID(experimentRecord.TenantID).SetProjectID(experimentRecord.ProjectID).
			SetExperimentID(experimentID).SetPeriod(reservation.Period).SetKind("release").SetAmountMilli(-experimentRecord.ReservedCostMilli).
			SetDescription("experiment completion reservation release").Save(ctx); err != nil {
			return err
		}
	}
	charged, err := tx.BudgetEntry.Query().Where(budgetentry.ExperimentIDEQ(experimentID), budgetentry.KindEQ("charge")).Exist(ctx)
	if err != nil {
		return err
	}
	if estimated > 0 && !charged {
		if _, err := tx.BudgetEntry.Create().SetTenantID(experimentRecord.TenantID).SetProjectID(experimentRecord.ProjectID).
			SetExperimentID(experimentID).SetPeriod(reservation.Period).SetKind("charge").SetAmountMilli(estimated).
			SetDescription("Provider runtime cost estimate").Save(ctx); err != nil {
			return err
		}
	}
	attemptUpdate := tx.Attempt.UpdateOneID(attemptRecord.ID).SetState(attemptState).SetFinishedAt(now).
		SetEstimatedCostMilli(estimated).SetRunnerTokenExpiresAt(now).ClearRunnerTokenHash().ClearRunnerTokenCiphertext()
	if failureCode != "" {
		attemptUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := attemptUpdate.Save(ctx); err != nil {
		return err
	}
	experimentUpdate := tx.Experiment.UpdateOneID(experimentID).SetState(finalState).SetFinishedAt(now).
		SetEstimatedCostMilli(estimated).SetBudgetFinalizedAt(now).ClearLeaseOwner().ClearLeaseExpiresAt()
	if failureCode != "" {
		experimentUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := experimentUpdate.Save(ctx); err != nil {
		return err
	}
	if err := enqueueFinalNotification(ctx, tx.Notification, experimentRecord, finalState, failureCode, failureReason, estimated); err != nil {
		return err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID(e.config.InstanceID).
		SetAction("experiment.finalized").SetTargetType("experiment").SetTargetID(experimentRecord.PublicID.String()).
		SetMetadata(map[string]any{"state": finalState, "attempt_id": attemptRecord.PublicID.String(), "estimated_cost_milli": estimated}).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func enqueueFinalNotification(ctx context.Context, client *ent.NotificationClient, experimentRecord *ent.Experiment, state, failureCode, failureReason string, estimated int64) error {
	severity := ""
	switch state {
	case "provider_error", "timed_out", "failed":
		severity = "critical"
	case "cancelled":
		if failureCode == "emergency_stop" {
			severity = "critical"
		}
	}
	if severity == "" {
		return nil
	}
	shortID := experimentRecord.PublicID.String()
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	body := fmt.Sprintf("Experiment: %s\nState: %s\nEstimated cost: %d milli-CNY", experimentRecord.PublicID.String(), state, estimated)
	if failureCode != "" {
		body += "\nFailure code: " + failureCode
	}
	if failureReason != "" {
		body += "\nReason: " + failureReason
	}
	return notification.Enqueue(ctx, client, notification.EnqueueInput{
		TenantID: experimentRecord.TenantID, DedupKey: "experiment-final:" + experimentRecord.PublicID.String() + ":" + state,
		Kind: "experiment_final", Severity: severity, Subject: "[Gemcp] Experiment " + shortID + " " + state, Body: body,
	})
}

func terminalResult(experimentRecord *ent.Experiment, attemptRecord *ent.Attempt, resourceRecord *ent.ProviderResource) (string, string, string, string) {
	reason := ""
	if resourceRecord.StopReason != nil {
		reason = *resourceRecord.StopReason
	}
	switch reason {
	case "cancelled", "owner_stop":
		return "cancelled", "cancelled", "cancelled", "experiment was cancelled by an authorized request"
	case "timeout":
		return "timed_out", "failed", "runtime_timeout", "experiment exceeded its extended runtime deadline"
	case "emergency":
		return "cancelled", "cancelled", "emergency_stop", "experiment was stopped by the Owner emergency action"
	case "oom":
		return "failed", "failed", "oom", valueOr(experimentRecord.FailureReason, "Provider reported an out-of-memory termination")
	case "runner_error":
		return "failed", "failed", "runner_error", "the Runner failed while managing the experiment process"
	case "provider_error", "provision_timeout", "runner_callback_missing":
		code := valueOr(experimentRecord.FailureCode, reason)
		return "provider_error", "failed", code, valueOr(experimentRecord.FailureReason, "Provider execution failed")
	}
	if experimentRecord.FailureCode != nil && attemptRecord.ExitCode == nil {
		return "provider_error", "failed", *experimentRecord.FailureCode, valueOr(experimentRecord.FailureReason, "Provider execution failed")
	}
	if attemptRecord.ExitCode == nil {
		return "provider_error", "failed", "runner_result_missing", "Runner did not report an exit code"
	}
	if *attemptRecord.ExitCode == 0 {
		return "succeeded", "succeeded", "", ""
	}
	return "failed", "failed", "command_failed", fmt.Sprintf("experiment command exited with code %d", *attemptRecord.ExitCode)
}

func estimateResourceCost(experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource, now time.Time) int64 {
	start := resourceRecord.ProviderStartedAt
	if start == nil {
		start = experimentRecord.StartedAt
	}
	if start == nil {
		return 0
	}
	end := now
	if resourceRecord.StoppedAt != nil {
		end = *resourceRecord.StoppedAt
	}
	seconds := int64(math.Ceil(end.Sub(*start).Seconds()))
	if seconds <= 0 {
		seconds = 1
	}
	price := resourceRecord.PriceMilliPerHour
	if price <= 0 {
		var snapshot resourceSnapshot
		if decodeSnapshot(experimentRecord.ResourceSnapshot, &snapshot) == nil && snapshot.PriceToMilli > 0 && snapshot.GPUNum > 0 && snapshot.PriceToMilli <= math.MaxInt64/int64(snapshot.GPUNum) {
			price = snapshot.PriceToMilli * int64(snapshot.GPUNum)
		}
	}
	if price <= 0 || price > math.MaxInt64/seconds {
		return experimentRecord.ReservedCostMilli
	}
	numerator := price * seconds
	result := numerator / 3600
	if numerator%3600 != 0 {
		result++
	}
	return result
}

func valueOr(value *string, fallback string) string {
	if value != nil && *value != "" {
		return *value
	}
	return fallback
}
