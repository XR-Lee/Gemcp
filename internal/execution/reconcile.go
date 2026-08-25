package execution

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/runner"
)

func (e *Engine) step(ctx context.Context, experimentID int) error {
	if e.selfHosted != nil {
		handled, err := e.selfHosted.Reconcile(ctx, experimentID, e.now().UTC())
		if handled || err != nil {
			return err
		}
	}
	if e.sshCloud != nil {
		handled, err := e.sshCloud.Reconcile(ctx, experimentID, e.now().UTC())
		if handled || err != nil {
			return err
		}
	}
	experimentRecord, err := e.client.Experiment.Get(ctx, experimentID)
	if err != nil {
		return err
	}
	resourceRecord, err := e.client.ProviderResource.Query().Where(
		providerresource.ExperimentIDEQ(experimentID),
	).Order(ent.Desc(providerresource.FieldID)).WithAttempt().WithProviderAccount().First(ctx)
	if err != nil {
		return fmt.Errorf("load owned Provider resource: %w", err)
	}
	now := e.now().UTC()
	claimed, err := e.client.ProviderResource.Update().Where(
		providerresource.IDEQ(resourceRecord.ID),
		providerresource.Or(providerresource.LeaseExpiresAtIsNil(), providerresource.LeaseExpiresAtLT(now), providerresource.LeaseOwnerEQ(e.config.InstanceID)),
	).SetLeaseOwner(e.config.InstanceID).SetLeaseExpiresAt(now.Add(e.config.LeaseDuration)).Save(ctx)
	if err != nil {
		return err
	}
	if claimed != 1 {
		return nil
	}
	defer e.releaseResourceLease(resourceRecord.ID)
	attemptRecord, err := resourceRecord.Edges.AttemptOrErr()
	if err != nil {
		return err
	}
	account, err := resourceRecord.Edges.ProviderAccountOrErr()
	if err != nil {
		return err
	}
	if experimentRecord.DesiredState == "cancelled" && resourceRecord.StopRequestedAt == nil {
		if err := e.requestStop(ctx, experimentRecord, resourceRecord, "cancelled", now); err != nil {
			return err
		}
		resourceRecord, _ = e.client.ProviderResource.Get(ctx, resourceRecord.ID)
	}

	switch string(resourceRecord.State) {
	case "creating":
		if resourceRecord.StopRequestedAt != nil {
			if _, err := resourceRecord.Update().SetState(providerresource.StateDeleted).SetDeletedAt(now).Save(ctx); err != nil {
				return err
			}
			return e.finalize(ctx, experimentRecord.ID, resourceRecord.ID)
		}
		if resourceRecord.CreateAttempts == 0 {
			if !e.config.Enabled {
				return nil
			}
			return e.create(ctx, experimentRecord, attemptRecord, resourceRecord, account, now)
		}
		return e.reconcileCreate(ctx, experimentRecord, attemptRecord, resourceRecord, account, now)
	case "active":
		if (resourceRecord.StopRequestedAt != nil || experimentRecord.State == "cancelling" || experimentRecord.State == "collecting") && cleanupReady(attemptRecord, resourceRecord, now) {
			return e.cleanup(ctx, experimentRecord.ID, resourceRecord.ID)
		}
		return e.observe(ctx, experimentRecord, attemptRecord, resourceRecord, account, now)
	case "stopping", "stopped", "deleting":
		return e.cleanup(ctx, experimentRecord.ID, resourceRecord.ID)
	case "deleted":
		return e.finalize(ctx, experimentRecord.ID, resourceRecord.ID)
	case "error":
		if experimentRecord.State != "queued" && !isTerminalExperiment(experimentRecord.State) {
			return e.finalize(ctx, experimentRecord.ID, resourceRecord.ID)
		}
	}
	return nil
}

func (e *Engine) create(ctx context.Context, experimentRecord *ent.Experiment, attemptRecord *ent.Attempt, resourceRecord *ent.ProviderResource, account *ent.ProviderAccount, now time.Time) error {
	updated, err := e.client.ProviderResource.Update().Where(
		providerresource.IDEQ(resourceRecord.ID), providerresource.StateEQ(providerresource.StateCreating),
		providerresource.CreateAttemptsEQ(0), providerresource.StopRequestedAtIsNil(),
	).AddCreateAttempts(1).SetCreateAttemptedAt(now).Save(ctx)
	if err != nil {
		return err
	}
	if updated != 1 {
		return nil
	}
	updatedResource, err := e.client.ProviderResource.Get(ctx, resourceRecord.ID)
	if err != nil {
		return err
	}
	plaintext, err := e.box.Decrypt(attemptRecord.RunnerTokenCiphertext, runner.TokenAADPrefix+attemptRecord.PublicID.String())
	if err != nil {
		return e.failProvider(ctx, experimentRecord.ID, resourceRecord.ID, "runner_token_decrypt", "could not decrypt the Runner token")
	}
	rawToken := string(plaintext)
	for index := range plaintext {
		plaintext[index] = 0
	}
	spec, err := deploymentSpec(experimentRecord, updatedResource, rawToken, e.config.PublicURL)
	rawToken = ""
	if err != nil {
		return e.failProvider(ctx, experimentRecord.ID, resourceRecord.ID, "invalid_execution_spec", err.Error())
	}
	providerID, requestIDs, err := e.provider.Create(ctx, account, spec)
	if err != nil {
		_ = e.recordResourceError(resourceRecord.ID, requestIDs, err)
		if permanentCreateError(err) {
			return e.failProvider(ctx, experimentRecord.ID, resourceRecord.ID, "provider_create_rejected", safeError(err))
		}
		return nil
	}
	return e.bindCreated(ctx, experimentRecord, attemptRecord, resourceRecord.ID, account, providerID, requestIDs, now)
}

func (e *Engine) reconcileCreate(ctx context.Context, experimentRecord *ent.Experiment, attemptRecord *ent.Attempt, resourceRecord *ent.ProviderResource, account *ent.ProviderAccount, now time.Time) error {
	if resourceRecord.CreateAttemptedAt != nil && now.Before(resourceRecord.CreateAttemptedAt.Add(e.config.ReconcileDelay)) {
		return nil
	}
	observation, err := e.provider.FindByName(ctx, account, resourceRecord.Name)
	if err != nil {
		_ = e.recordResourceError(resourceRecord.ID, observation.RequestIDs, err)
		return err
	}
	if observation.Found {
		return e.bindCreated(ctx, experimentRecord, attemptRecord, resourceRecord.ID, account, observation.Deployment.UUID, observation.RequestIDs, now)
	}
	return e.retryAttempt(ctx, experimentRecord.ID, resourceRecord.ID, "create_result_unconfirmed", "Provider create did not produce a reconcilable deployment", now)
}

func (e *Engine) bindCreated(ctx context.Context, experimentRecord *ent.Experiment, attemptRecord *ent.Attempt, resourceID int, account *ent.ProviderAccount, providerID string, requestIDs map[string]string, now time.Time) error {
	tx, err := e.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	resourceRecord, err := tx.ProviderResource.Get(ctx, resourceID)
	if err != nil {
		return err
	}
	merged := mergeRequestIDs(resourceRecord.RequestIds, requestIDs)
	if _, err := tx.ProviderResource.UpdateOneID(resourceID).SetProviderID(providerID).SetState(providerresource.StateActive).
		SetProviderStatus("created").SetLastSeenAt(now).SetRequestIds(merged).ClearLastError().Save(ctx); err != nil {
		return fmt.Errorf("bind Provider resource ownership: %w", err)
	}
	if _, err := tx.Attempt.UpdateOneID(attemptRecord.ID).SetProviderResourceID(providerID).SetProviderRequestIds(merged).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(experimentRecord.ID).SetProviderResourceID(providerID).SetProviderStatus("created").Save(ctx); err != nil {
		return err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID(e.config.InstanceID).
		SetAction("provider.resource_created").SetTargetType("provider_resource").SetTargetID(resourceRecord.PublicID.String()).
		SetMetadata(map[string]any{"provider_id": providerID, "attempt_id": attemptRecord.PublicID.String(), "provider_account_id": account.PublicID.String()}).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (e *Engine) observe(ctx context.Context, experimentRecord *ent.Experiment, attemptRecord *ent.Attempt, resourceRecord *ent.ProviderResource, account *ent.ProviderAccount, now time.Time) error {
	if resourceRecord.ProviderID == nil {
		return e.failProvider(ctx, experimentRecord.ID, resourceRecord.ID, "missing_provider_id", "owned resource has no Provider ID")
	}
	observation, err := e.provider.Observe(ctx, account, *resourceRecord.ProviderID)
	if err != nil {
		_ = e.recordResourceError(resourceRecord.ID, observation.RequestIDs, err)
		return err
	}
	if !observation.Found {
		if resourceRecord.CreateAttemptedAt != nil && now.Before(resourceRecord.CreateAttemptedAt.Add(e.config.ReconcileDelay)) {
			return nil
		}
		if resourceRecord.StopRequestedAt != nil || attemptRecord.State == "collecting" {
			if _, err := resourceRecord.Update().SetState(providerresource.StateDeleted).SetDeletedAt(now).Save(ctx); err != nil {
				return err
			}
			return e.finalize(ctx, experimentRecord.ID, resourceRecord.ID)
		}
		return e.failProvider(ctx, experimentRecord.ID, resourceRecord.ID, "provider_resource_missing", "Provider deployment disappeared before completion")
	}

	price, startedAt := observationCostData(observation)
	update := resourceRecord.Update().SetProviderStatus(observation.Deployment.Status).SetLastSeenAt(now).
		SetRequestIds(mergeRequestIDs(resourceRecord.RequestIds, observation.RequestIDs)).ClearLastError()
	if price > 0 {
		update.SetPriceMilliPerHour(price)
	}
	if startedAt != nil {
		update.SetProviderStartedAt(*startedAt)
	}
	terminal := terminalDeployment(observation.Deployment)
	if terminal && resourceRecord.TerminalObservedAt == nil {
		update.SetTerminalObservedAt(now)
	}
	resourceRecord, err = update.Save(ctx)
	if err != nil {
		return err
	}
	if _, err := experimentRecord.Update().SetProviderStatus(observation.Deployment.Status).Save(ctx); err != nil {
		return err
	}

	if experimentRecord.DesiredState == "cancelled" {
		if err := e.requestStop(ctx, experimentRecord, resourceRecord, "cancelled", now); err != nil {
			return err
		}
		return e.cleanup(ctx, experimentRecord.ID, resourceRecord.ID)
	}
	if err := e.enforceDeadline(ctx, experimentRecord.ID, resourceRecord.ID, now); err != nil {
		return err
	}
	attemptRecord, err = e.client.Attempt.Get(ctx, attemptRecord.ID)
	if err != nil {
		return err
	}
	experimentRecord, err = e.client.Experiment.Get(ctx, experimentRecord.ID)
	if err != nil {
		return err
	}
	resourceRecord, err = e.client.ProviderResource.Get(ctx, resourceRecord.ID)
	if err != nil {
		return err
	}
	if (resourceRecord.StopRequestedAt != nil || experimentRecord.State == "cancelling" || experimentRecord.State == "collecting") && cleanupReady(attemptRecord, resourceRecord, now) {
		return e.cleanup(ctx, experimentRecord.ID, resourceRecord.ID)
	}
	if terminal {
		if attemptRecord.State == "collecting" {
			if err := e.requestStop(ctx, experimentRecord, resourceRecord, "completed", now); err != nil {
				return err
			}
			return e.cleanup(ctx, experimentRecord.ID, resourceRecord.ID)
		}
		observedAt := resourceRecord.TerminalObservedAt
		if observedAt == nil || now.Before(observedAt.Add(e.config.CallbackGrace)) {
			return nil
		}
		failureCode, failureReason, stopReason := missingCallbackResult(observation)
		if err := e.markMissingCallback(ctx, experimentRecord, attemptRecord, resourceRecord, failureCode, failureReason, stopReason, now); err != nil {
			return err
		}
		return e.cleanup(ctx, experimentRecord.ID, resourceRecord.ID)
	}
	if experimentRecord.State == "provisioning" && resourceRecord.HardDeadlineAt != nil && !now.Before(*resourceRecord.HardDeadlineAt) {
		if err := e.markProvisionTimeout(ctx, experimentRecord, attemptRecord, resourceRecord, now); err != nil {
			return err
		}
		return e.cleanup(ctx, experimentRecord.ID, resourceRecord.ID)
	}
	return nil
}

func cleanupReady(attemptRecord *ent.Attempt, resourceRecord *ent.ProviderResource, now time.Time) bool {
	if resourceRecord.StopReason == nil || *resourceRecord.StopReason != "timeout" || attemptRecord.State == "collecting" {
		return true
	}
	return resourceRecord.HardDeadlineAt == nil || !now.Before(*resourceRecord.HardDeadlineAt)
}

func (e *Engine) cleanup(ctx context.Context, experimentID, resourceID int) error {
	resourceRecord, err := e.client.ProviderResource.Query().Where(providerresource.IDEQ(resourceID)).WithProviderAccount().Only(ctx)
	if err != nil {
		return err
	}
	account, err := resourceRecord.Edges.ProviderAccountOrErr()
	if err != nil {
		return err
	}
	now := e.now().UTC()
	if resourceRecord.ProviderID == nil {
		if _, err := resourceRecord.Update().SetState(providerresource.StateDeleted).SetDeletedAt(now).Save(ctx); err != nil {
			return err
		}
		return e.finalize(ctx, experimentID, resourceID)
	}
	if resourceRecord.State == providerresource.StateDeleting {
		return e.confirmDeletion(ctx, experimentID, resourceRecord, account, now)
	}
	if resourceRecord.State != providerresource.StateStopped {
		resourceRecord, err = resourceRecord.Update().SetState(providerresource.StateStopping).Save(ctx)
		if err != nil {
			return err
		}
		requestIDs, stopErr := e.provider.Stop(ctx, account, *resourceRecord.ProviderID)
		if stopErr != nil {
			_ = e.recordResourceError(resourceID, requestIDs, stopErr)
			return stopErr
		}
		resourceRecord, err = resourceRecord.Update().SetState(providerresource.StateStopped).SetStoppedAt(now).
			SetRequestIds(mergeRequestIDs(resourceRecord.RequestIds, requestIDs)).ClearLastError().Save(ctx)
		if err != nil {
			return err
		}
	}
	resourceRecord, err = resourceRecord.Update().SetState(providerresource.StateDeleting).Save(ctx)
	if err != nil {
		return err
	}
	requestIDs, deleteErr := e.provider.Delete(ctx, account, *resourceRecord.ProviderID)
	if deleteErr != nil {
		_ = e.recordResourceError(resourceID, requestIDs, deleteErr)
		return deleteErr
	}
	resourceRecord, err = resourceRecord.Update().SetDeleteRequestedAt(now).
		SetRequestIds(mergeRequestIDs(resourceRecord.RequestIds, requestIDs)).ClearLastError().Save(ctx)
	if err != nil {
		return err
	}
	return e.confirmDeletion(ctx, experimentID, resourceRecord, account, now)
}

func (e *Engine) confirmDeletion(ctx context.Context, experimentID int, resourceRecord *ent.ProviderResource, account *ent.ProviderAccount, now time.Time) error {
	observation, err := e.provider.Observe(ctx, account, *resourceRecord.ProviderID)
	if err != nil {
		_ = e.recordResourceError(resourceRecord.ID, observation.RequestIDs, err)
		return err
	}
	requestIDs := mergeRequestIDs(resourceRecord.RequestIds, observation.RequestIDs)
	if !observation.Found {
		if _, err := resourceRecord.Update().SetState(providerresource.StateDeleted).SetDeletedAt(now).
			SetRequestIds(requestIDs).ClearLastError().Save(ctx); err != nil {
			return err
		}
		return e.finalize(ctx, experimentID, resourceRecord.ID)
	}
	resourceRecord, err = resourceRecord.Update().SetProviderStatus(observation.Deployment.Status).SetLastSeenAt(now).
		SetRequestIds(requestIDs).ClearLastError().Save(ctx)
	if err != nil {
		return err
	}
	if resourceRecord.DeleteRequestedAt != nil && now.Before(resourceRecord.DeleteRequestedAt.Add(e.config.ReconcileDelay)) {
		return nil
	}
	requestIDs, err = e.provider.Delete(ctx, account, *resourceRecord.ProviderID)
	if err != nil {
		_ = e.recordResourceError(resourceRecord.ID, requestIDs, err)
		return err
	}
	_, err = resourceRecord.Update().SetDeleteRequestedAt(now).
		SetRequestIds(mergeRequestIDs(resourceRecord.RequestIds, requestIDs)).ClearLastError().Save(ctx)
	return err
}

func (e *Engine) requestStop(ctx context.Context, experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource, reason string, now time.Time) error {
	if resourceRecord.StopRequestedAt == nil {
		if _, err := resourceRecord.Update().SetStopRequestedAt(now).SetStopReason(reason).Save(ctx); err != nil {
			return err
		}
	}
	if experimentRecord.State != "collecting" && experimentRecord.State != "cancelling" {
		_, err := experimentRecord.Update().SetState("cancelling").Save(ctx)
		return err
	}
	return nil
}

func (e *Engine) releaseResourceLease(resourceID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = e.client.ProviderResource.Update().Where(
		providerresource.IDEQ(resourceID), providerresource.LeaseOwnerEQ(e.config.InstanceID),
	).ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx)
}

func (e *Engine) recordResourceError(resourceID int, requestIDs map[string]string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	record, err := e.client.ProviderResource.Get(ctx, resourceID)
	if err != nil {
		return err
	}
	_, err = record.Update().SetLastError(safeError(cause)).SetRequestIds(mergeRequestIDs(record.RequestIds, requestIDs)).Save(ctx)
	return err
}

func observationCostData(observation Observation) (int64, *time.Time) {
	var price int64
	var started *time.Time
	for _, container := range observation.Containers {
		if container.PriceMilli > 0 && price <= math.MaxInt64-container.PriceMilli {
			price += container.PriceMilli
		}
		if container.StartedAt != nil && (started == nil || container.StartedAt.Before(*started)) {
			value := *container.StartedAt
			started = &value
		}
	}
	return price, started
}

func terminalDeployment(deployment autodl.Deployment) bool {
	if deployment.FinishedNum > 0 || deployment.FailedNum > 0 {
		return true
	}
	switch strings.ToLower(deployment.Status) {
	case "stopped", "finished", "completed", "failed", "shutdown":
		return true
	default:
		return false
	}
}

func missingCallbackResult(observation Observation) (string, string, string) {
	for _, event := range observation.Events {
		status := strings.ToLower(event.Status)
		if strings.Contains(status, "oom") || strings.Contains(status, "out_of_memory") || strings.Contains(status, "out of memory") {
			return "oom", "Provider reported an out-of-memory termination", "oom"
		}
	}
	if observation.Deployment.FailedNum > 0 || strings.EqualFold(observation.Deployment.Status, "failed") {
		return "provider_job_failed", "Provider reported a failed Job without a Runner completion callback", "provider_error"
	}
	return "runner_callback_missing", "Provider reached a terminal state without a Runner completion callback", "runner_callback_missing"
}

func permanentCreateError(err error) bool {
	var providerErr *autodl.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	if providerErr.HTTPStatus == 401 || providerErr.HTTPStatus == 403 {
		return true
	}
	value := strings.ToLower(providerErr.Code + " " + providerErr.Message)
	capacity := strings.Contains(value, "capacity") || strings.Contains(value, "stock") || strings.Contains(value, "available") || strings.Contains(value, "busy")
	return providerErr.HTTPStatus >= 400 && providerErr.HTTPStatus < 500 && providerErr.HTTPStatus != 408 && providerErr.HTTPStatus != 429 && !capacity
}

func mergeRequestIDs(existing, incoming map[string]string) map[string]string {
	result := make(map[string]string, len(existing)+len(incoming))
	for key, value := range existing {
		if value != "" {
			result[key] = value
		}
	}
	for key, value := range incoming {
		if value != "" {
			result[key] = value
		}
	}
	return result
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512]) + "..."
	}
	return value
}

func isTerminalExperiment(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "timed_out", "budget_stopped", "provider_error":
		return true
	default:
		return false
	}
}
