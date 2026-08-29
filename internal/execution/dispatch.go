package execution

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/provideraccount"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/notification"
	"github.com/XR-Lee/Gemcp/internal/runner"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
)

func (e *Engine) dispatchAvailable(ctx context.Context, now time.Time) ([]int, error) {
	active, err := e.client.Experiment.Query().Where(entexperiment.StateIn(activeExperimentStates...)).Count(ctx)
	if err != nil {
		return nil, err
	}
	slots := e.config.GlobalConcurrency - active
	if slots <= 0 {
		return nil, nil
	}
	candidates, err := e.client.Experiment.Query().Where(
		entexperiment.StateEQ("queued"), entexperiment.DesiredStateEQ("running"), entexperiment.NextAttemptAtLTE(now),
		entexperiment.HasProjectWith(project.StatusEQ("active")),
	).Order(ent.Asc(entexperiment.FieldCreatedAt), ent.Asc(entexperiment.FieldID)).Limit(slots * 4).IDs(ctx)
	if err != nil {
		return nil, err
	}
	dispatched := make([]int, 0, slots)
	for _, candidateID := range candidates {
		if len(dispatched) >= slots {
			break
		}
		ok, err := e.dispatchOne(ctx, candidateID, now)
		if err != nil {
			return dispatched, err
		}
		if ok {
			dispatched = append(dispatched, candidateID)
		}
	}
	return dispatched, nil
}

func (e *Engine) dispatchOne(ctx context.Context, experimentID int, now time.Time) (bool, error) {
	tx, err := e.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	record, err := tx.Experiment.Query().Where(
		entexperiment.IDEQ(experimentID), entexperiment.StateEQ("queued"),
		entexperiment.DesiredStateEQ("running"), entexperiment.NextAttemptAtLTE(now),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	projectRecord, err := tx.Project.Query().Where(project.IDEQ(record.ProjectID), project.StatusEQ("active")).Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	globalActive, err := tx.Experiment.Query().Where(entexperiment.StateIn(activeExperimentStates...)).Count(ctx)
	if err != nil {
		return false, err
	}
	projectActive, err := tx.Experiment.Query().Where(
		entexperiment.ProjectIDEQ(record.ProjectID), entexperiment.StateIn(activeExperimentStates...),
	).Count(ctx)
	if err != nil {
		return false, err
	}
	if globalActive >= e.config.GlobalConcurrency || projectActive >= projectRecord.MaxConcurrency {
		return false, nil
	}
	profileRecord, err := tx.ResourceProfile.Query().Where(resourceprofile.IDEQ(record.ResourceProfileID)).Only(ctx)
	if err != nil {
		return false, err
	}
	if profileRecord.Backend == resourceprofile.BackendSelfHosted {
		if e.selfHosted == nil || !e.selfHosted.Enabled() {
			return false, nil
		}
		dispatched, err := e.selfHosted.Dispatch(ctx, tx, record, now)
		if err != nil || !dispatched {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	providerBackend := provideraccount.BackendPrivate
	if profileRecord.Backend == resourceprofile.BackendAutodlElastic {
		providerBackend = provideraccount.BackendElastic
	} else if profileRecord.Backend != resourceprofile.BackendAutodlPrivate {
		return false, fmt.Errorf("unsupported AutoDL resource backend %q", profileRecord.Backend)
	}
	providerRecord, err := tx.ProviderAccount.Query().Where(
		provideraccount.TenantIDEQ(record.TenantID), provideraccount.BackendEQ(providerBackend),
		provideraccount.StatusEQ(provideraccount.StatusActive),
	).Order(ent.Asc(provideraccount.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	reservation, err := tx.BudgetEntry.Query().Where(
		budgetentry.ExperimentIDEQ(record.ID), budgetentry.KindEQ("reservation"),
	).Order(ent.Desc(budgetentry.FieldID)).First(ctx)
	if err != nil {
		return false, fmt.Errorf("load experiment reservation: %w", err)
	}
	requiredReservation, reservationErr := executionReservation(record)
	if reservationErr != nil {
		if err := terminalQueuedProviderError(ctx, tx, record, reservation, now, "invalid_reservation_policy", safeError(reservationErr)); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if requiredReservation > record.ReservedCostMilli {
		if err := terminalQueuedProviderError(ctx, tx, record, reservation, now, "reservation_policy_outdated", "queued experiment does not reserve its timeout extension and termination grace"); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if len(record.SecretNames) > 0 {
		if err := terminalQueuedProviderError(ctx, tx, record, reservation, now, "secret_injection_unavailable", "project Secret injection is not available"); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	currentPeriod, err := projectBudgetPeriod(now, projectRecord.Timezone)
	if err != nil {
		return false, err
	}
	if reservation.Period != currentPeriod {
		currentCommitted, err := executionLedgerTotal(ctx, tx, record.ProjectID, currentPeriod)
		if err != nil {
			return false, err
		}
		if record.ReservedCostMilli > projectRecord.MonthlyBudgetMilli || currentCommitted > projectRecord.MonthlyBudgetMilli-record.ReservedCostMilli {
			if err := stopQueuedForBudget(ctx, tx, record, reservation, now); err != nil {
				return false, err
			}
			return false, tx.Commit()
		}
		if _, err := tx.BudgetEntry.Create().SetTenantID(record.TenantID).SetProjectID(record.ProjectID).SetExperimentID(record.ID).
			SetPeriod(reservation.Period).SetKind("release").SetAmountMilli(-record.ReservedCostMilli).
			SetDescription("queued experiment reservation rollover release").Save(ctx); err != nil {
			return false, err
		}
		reservation, err = tx.BudgetEntry.Create().SetTenantID(record.TenantID).SetProjectID(record.ProjectID).SetExperimentID(record.ID).
			SetPeriod(currentPeriod).SetKind("reservation").SetAmountMilli(record.ReservedCostMilli).
			SetDescription("queued experiment reservation rollover").Save(ctx)
		if err != nil {
			return false, err
		}
	}
	committed, err := executionLedgerTotal(ctx, tx, record.ProjectID, reservation.Period)
	if err != nil {
		return false, err
	}
	if committed > projectRecord.MonthlyBudgetMilli {
		if err := stopQueuedForBudget(ctx, tx, record, reservation, now); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	attemptNumber, err := tx.Attempt.Query().Where(attempt.ExperimentIDEQ(record.ID)).Count(ctx)
	if err != nil {
		return false, err
	}
	if attemptNumber >= e.config.MaxAttempts {
		if err := terminalQueuedProviderError(ctx, tx, record, reservation, now, "attempt_limit", "maximum infrastructure attempts exhausted"); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	attemptNumber++
	attemptPublicID := uuid.New()
	rawToken, _, err := secrets.RandomToken("gmr", 32)
	if err != nil {
		return false, err
	}
	plaintext := []byte(rawToken)
	ciphertext, err := e.box.Encrypt(plaintext, runner.TokenAADPrefix+attemptPublicID.String())
	for index := range plaintext {
		plaintext[index] = 0
	}
	if err != nil {
		return false, fmt.Errorf("encrypt Runner token: %w", err)
	}
	tokenTTL, err := runnerTokenTTL(record, e.config)
	if err != nil {
		return false, err
	}
	attemptRecord, err := tx.Attempt.Create().SetPublicID(attemptPublicID).
		SetTenantID(record.TenantID).SetProjectID(record.ProjectID).SetExperimentID(record.ID).SetNumber(attemptNumber).
		SetRunnerTokenHash(e.box.Digest(runner.TokenDigestDomain, rawToken)).SetRunnerTokenCiphertext(ciphertext).
		SetRunnerTokenExpiresAt(now.Add(tokenTTL)).Save(ctx)
	if err != nil {
		return false, err
	}
	resourceRecord, err := tx.ProviderResource.Create().SetTenantID(record.TenantID).SetProjectID(record.ProjectID).
		SetExperimentID(record.ID).SetAttemptID(attemptRecord.ID).SetProviderAccountID(providerRecord.ID).
		SetName("gemcp-" + attemptPublicID.String()).SetHardDeadlineAt(now.Add(e.config.ProvisionTimeout)).Save(ctx)
	if err != nil {
		return false, err
	}
	updated, err := tx.Experiment.Update().Where(entexperiment.IDEQ(record.ID), entexperiment.StateEQ("queued")).
		SetState("provisioning").SetLeaseOwner(e.config.InstanceID).SetLeaseExpiresAt(now.Add(e.config.LeaseDuration)).
		ClearProviderResourceID().ClearProviderStatus().Save(ctx)
	if err != nil {
		return false, err
	}
	if updated != 1 {
		return false, nil
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(record.TenantID).SetActorType("system").SetActorID(e.config.InstanceID).
		SetAction("experiment.dispatched").SetTargetType("experiment").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"attempt_id": attemptPublicID.String(), "attempt_number": attemptNumber, "resource_id": resourceRecord.PublicID.String()}).Save(ctx); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func runnerTokenTTL(record *ent.Experiment, config Config) (time.Duration, error) {
	seconds := int64(record.MaxRuntimeSeconds) + int64(record.TimeoutExtensionSeconds) + int64(record.TerminationGraceSeconds)
	if seconds <= 0 || seconds > int64((30*24*time.Hour)/time.Second) {
		return 0, fmt.Errorf("experiment runtime policy exceeds the 30-day Runner limit")
	}
	limit := 32 * 24 * time.Hour
	runtime := time.Duration(seconds) * time.Second
	if config.ProvisionTimeout > limit || runtime > limit-config.ProvisionTimeout || config.RunnerTokenExtraTTL > limit-config.ProvisionTimeout-runtime {
		return 0, fmt.Errorf("Runner token TTL exceeds 32 days")
	}
	return config.ProvisionTimeout + runtime + config.RunnerTokenExtraTTL, nil
}

const (
	provisionReserveSeconds           = int64(600)
	shutdownObservationReserveSeconds = int64(30)
)

func executionReservation(record *ent.Experiment) (int64, error) {
	var snapshot resourceSnapshot
	if err := decodeSnapshot(record.ResourceSnapshot, &snapshot); err != nil {
		return 0, fmt.Errorf("decode resource snapshot: %w", err)
	}
	seconds := provisionReserveSeconds + shutdownObservationReserveSeconds
	for _, value := range []int{record.MaxRuntimeSeconds, record.TimeoutExtensionSeconds, record.TerminationGraceSeconds} {
		if value < 0 || seconds > math.MaxInt64-int64(value) {
			return 0, fmt.Errorf("execution reservation duration overflow")
		}
		seconds += int64(value)
	}
	if record.MaxRuntimeSeconds <= 0 || snapshot.PriceToMilli <= 0 || snapshot.GPUNum <= 0 {
		return 0, fmt.Errorf("invalid runtime or resource price for reservation")
	}
	if snapshot.PriceToMilli > math.MaxInt64/int64(snapshot.GPUNum) || snapshot.PriceToMilli*int64(snapshot.GPUNum) > math.MaxInt64/seconds {
		return 0, fmt.Errorf("execution reservation overflow")
	}
	numerator := snapshot.PriceToMilli * int64(snapshot.GPUNum) * seconds
	result := numerator / 3600
	if numerator%3600 != 0 {
		result++
	}
	return result, nil
}

func projectBudgetPeriod(now time.Time, timezone string) (string, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", fmt.Errorf("load project timezone: %w", err)
	}
	return now.In(location).Format("2006-01"), nil
}

func executionLedgerTotal(ctx context.Context, tx *ent.Tx, projectID int, period string) (int64, error) {
	entries, err := tx.BudgetEntry.Query().Where(budgetentry.ProjectIDEQ(projectID), budgetentry.PeriodEQ(period)).All(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if entry.AmountMilli > 0 && total > math.MaxInt64-entry.AmountMilli {
			return 0, fmt.Errorf("budget ledger overflow")
		}
		if entry.AmountMilli < 0 && total < math.MinInt64-entry.AmountMilli {
			return 0, fmt.Errorf("budget ledger underflow")
		}
		total += entry.AmountMilli
	}
	return total, nil
}

func stopQueuedForBudget(ctx context.Context, tx *ent.Tx, record *ent.Experiment, reservation *ent.BudgetEntry, now time.Time) error {
	if _, err := tx.BudgetEntry.Create().SetTenantID(record.TenantID).SetProjectID(record.ProjectID).SetExperimentID(record.ID).
		SetPeriod(reservation.Period).SetKind("release").SetAmountMilli(-record.ReservedCostMilli).
		SetDescription("dispatch budget recheck release").Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(record.ID).SetState("budget_stopped").SetFinishedAt(now).SetBudgetFinalizedAt(now).
		SetFailureCode("budget_recheck_failed").SetFailureReason("project budget changed before dispatch").Save(ctx); err != nil {
		return err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(record.TenantID).SetActorType("system").SetActorID("scheduler").
		SetAction("experiment.budget_stopped").SetTargetType("experiment").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"stage": "dispatch"}).Save(ctx); err != nil {
		return err
	}
	return notification.Enqueue(ctx, tx.Notification, notification.EnqueueInput{
		TenantID: record.TenantID, DedupKey: "budget-stopped:" + record.PublicID.String(), Kind: "budget_stopped", Severity: "critical",
		Subject: "[Gemcp] Experiment stopped by budget recheck", Body: fmt.Sprintf("Experiment %s was not dispatched because the project budget no longer had capacity.", record.PublicID.String()),
	})
}

func terminalQueuedProviderError(ctx context.Context, tx *ent.Tx, record *ent.Experiment, reservation *ent.BudgetEntry, now time.Time, code, reason string) error {
	if _, err := tx.BudgetEntry.Create().SetTenantID(record.TenantID).SetProjectID(record.ProjectID).SetExperimentID(record.ID).
		SetPeriod(reservation.Period).SetKind("release").SetAmountMilli(-record.ReservedCostMilli).
		SetDescription("provider error reservation release").Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(record.ID).SetState("provider_error").SetFinishedAt(now).SetBudgetFinalizedAt(now).
		SetFailureCode(code).SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(record.TenantID).SetActorType("system").SetActorID("scheduler").
		SetAction("experiment.provider_error").SetTargetType("experiment").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"failure_code": code}).Save(ctx); err != nil {
		return err
	}
	return notification.Enqueue(ctx, tx.Notification, notification.EnqueueInput{
		TenantID: record.TenantID, DedupKey: "provider-error:" + record.PublicID.String(), Kind: "provider_error", Severity: "critical",
		Subject: "[Gemcp] Experiment could not be dispatched", Body: fmt.Sprintf("Experiment %s failed before dispatch.\nFailure code: %s\nReason: %s", record.PublicID.String(), code, reason),
	})
}
