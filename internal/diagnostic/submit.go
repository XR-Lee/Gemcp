package diagnostic

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/diagnosticrun"
	"github.com/XR-Lee/Gemcp/ent/environment"
	entproject "github.com/XR-Lee/Gemcp/ent/project"
	entrepository "github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/google/uuid"
)

func (s *Service) Submit(ctx context.Context, tenantID int, actorID, projectID string, input SubmitInput) (SubmitResult, error) {
	var result SubmitResult
	if s == nil || s.client == nil || s.box == nil {
		return result, fmt.Errorf("diagnostic service is unavailable")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if !idempotencyPattern.MatchString(key) {
		return result, invalid("idempotency_key must contain 8 to 128 safe characters")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || len(actorID) > 120 {
		return result, invalid("authenticated Owner identity is required")
	}
	resolved, err := s.resolve(ctx, tenantID, projectID, input.PreflightInput)
	if err != nil {
		return result, err
	}
	fingerprint := diagnosticFingerprint(resolved.normalized)
	keyHash := s.box.Digest("diagnostic-idempotency", key)
	if record, found, err := s.existing(ctx, resolved.project.ID, keyHash, fingerprint); err != nil {
		return result, err
	} else if found {
		view, viewErr := s.runView(ctx, tenantID, resolved.project.PublicID.String(), record)
		return SubmitResult{Run: view, Idempotent: true}, viewErr
	}
	preflight, err := s.Preflight(ctx, tenantID, projectID, input.PreflightInput)
	if err != nil {
		return result, err
	}
	if !preflight.Eligible {
		return result, ErrPreflightFailed
	}
	if !input.Confirmed {
		return result, ErrConfirmationRequired
	}
	if !hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(input.ConfirmationDigest))), []byte(preflight.ConfirmationDigest)) {
		return result, ErrProposalChanged
	}
	for attempt := 0; attempt < 3; attempt++ {
		record, idempotent, err := s.createRun(ctx, tenantID, actorID, resolved, preflight, keyHash, fingerprint)
		if err == nil {
			view, viewErr := s.runView(ctx, tenantID, resolved.project.PublicID.String(), record)
			return SubmitResult{Run: view, Idempotent: idempotent}, viewErr
		}
		if ent.IsConstraintError(err) {
			if existing, found, lookupErr := s.existing(ctx, resolved.project.ID, keyHash, fingerprint); lookupErr != nil {
				return result, lookupErr
			} else if found {
				view, viewErr := s.runView(ctx, tenantID, resolved.project.PublicID.String(), existing)
				return SubmitResult{Run: view, Idempotent: true}, viewErr
			}
		}
		if !retryableTransaction(err) {
			return result, err
		}
	}
	return result, fmt.Errorf("diagnostic submission transaction did not converge")
}

func (s *Service) createRun(ctx context.Context, tenantID int, actorID string, resolved resolvedInput, preflight Preflight, keyHash, fingerprint []byte) (*ent.DiagnosticRun, bool, error) {
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if record, found, err := existingQuery(ctx, tx.DiagnosticRun.Query(), resolved.project.ID, keyHash, fingerprint); err != nil {
		return nil, false, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return record, true, nil
	}
	projectRecord, err := tx.Project.Query().Where(
		entproject.IDEQ(resolved.project.ID), entproject.TenantIDEQ(tenantID), entproject.StatusEQ(entproject.StatusActive),
	).Only(ctx)
	if err != nil {
		return nil, false, ErrPreflightFailed
	}
	repositoryRecord, err := tx.Repository.Query().Where(
		entrepository.IDEQ(resolved.repository.ID), entrepository.ProjectIDEQ(projectRecord.ID), entrepository.StatusEQ(entrepository.StatusActive),
	).Only(ctx)
	if err != nil {
		return nil, false, ErrPreflightFailed
	}
	environmentRecord, err := tx.Environment.Query().Where(
		environment.IDEQ(resolved.environment.ID), environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ(environment.StatusApproved),
	).Only(ctx)
	if err != nil {
		return nil, false, ErrPreflightFailed
	}
	profileRecord, err := tx.ResourceProfile.Query().Where(
		resourceprofile.IDEQ(resolved.profile.ID), resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Only(ctx)
	if err != nil || string(environmentRecord.Backend) != resolved.normalized.Backend || string(profileRecord.Backend) != resolved.normalized.Backend {
		return nil, false, ErrPreflightFailed
	}
	current := resolved
	current.project = projectRecord
	current.repository = repositoryRecord
	current.environment = environmentRecord
	current.profile = profileRecord
	current.grace = projectRecord.TerminationGraceSeconds
	if current.grace > 30 {
		current.grace = 30
	}
	if current.runtime > projectRecord.MaxRuntimeSeconds {
		return nil, false, ErrPreflightFailed
	}
	current.reservation, err = diagnosticReservation(profileRecord, current.runtime, current.grace)
	if err != nil {
		return nil, false, ErrPreflightFailed
	}
	currentProposal := proposalFor(current)
	if !hmac.Equal([]byte(proposalDigest(current, currentProposal)), []byte(preflight.ConfirmationDigest)) {
		return nil, false, ErrProposalChanged
	}
	reservation := current.reservation
	if reservation > projectRecord.MaxExperimentMilli {
		return nil, false, ErrExperimentCap
	}
	period, err := currentPeriod(s.now().UTC(), projectRecord.Timezone)
	if err != nil {
		return nil, false, err
	}
	if reservation > 0 {
		entries, err := tx.BudgetEntry.Query().Where(
			budgetentry.ProjectIDEQ(projectRecord.ID), budgetentry.PeriodEQ(period),
		).All(ctx)
		if err != nil {
			return nil, false, err
		}
		committed := int64(0)
		for _, entry := range entries {
			if (entry.AmountMilli > 0 && committed > math.MaxInt64-entry.AmountMilli) || (entry.AmountMilli < 0 && committed < math.MinInt64-entry.AmountMilli) {
				return nil, false, fmt.Errorf("budget ledger overflow")
			}
			committed += entry.AmountMilli
		}
		if reservation > projectRecord.MonthlyBudgetMilli || committed > projectRecord.MonthlyBudgetMilli-reservation {
			return nil, false, ErrBudgetExceeded
		}
	}
	experimentID := uuid.New()
	outputPath := "/root/autodl-fs/projects/" + projectRecord.PublicID.String() + "/experiments/" + experimentID.String() + "/"
	selfHosted := current.normalized.Backend == BackendSelfHosted
	if selfHosted {
		outputPath = "managed://experiments/" + experimentID.String() + "/outputs"
	}
	experimentRecord, err := tx.Experiment.Create().
		SetPublicID(experimentID).
		SetTenantID(tenantID).
		SetProjectID(projectRecord.ID).
		SetRepositoryID(repositoryRecord.ID).
		SetEnvironmentID(environmentRecord.ID).
		SetResourceProfileID(profileRecord.ID).
		SetCommitSha(current.normalized.CommitSHA).
		SetCommand(current.command).
		SetMaxRuntimeSeconds(current.runtime).
		SetTimeoutExtensionSeconds(0).
		SetTerminationGraceSeconds(current.grace).
		SetRepositorySnapshot(diagnosticRepositorySnapshot(repositoryRecord, projectRecord.PublicID.String())).
		SetEnvironmentSnapshot(diagnosticEnvironmentSnapshot(environmentRecord)).
		SetResourceSnapshot(diagnosticResourceSnapshot(profileRecord)).
		SetSecretNames([]string{}).
		SetOutputPath(outputPath).
		SetReservedCostMilli(reservation).
		Save(ctx)
	if err != nil {
		return nil, false, err
	}
	description := "diagnostic budget reservation"
	if selfHosted {
		description = "unmetered Self-hosted diagnostic reservation"
	}
	if _, err := tx.BudgetEntry.Create().
		SetTenantID(tenantID).SetProjectID(projectRecord.ID).SetExperimentID(experimentRecord.ID).
		SetPeriod(period).SetKind("reservation").SetAmountMilli(reservation).SetDescription(description).Save(ctx); err != nil {
		return nil, false, err
	}
	preflightValue, err := preflightMap(preflight)
	if err != nil {
		return nil, false, err
	}
	run, err := tx.DiagnosticRun.Create().
		SetTenantID(tenantID).
		SetProjectID(projectRecord.ID).
		SetExperimentID(experimentRecord.ID).
		SetBackend(diagnosticrun.Backend(current.normalized.Backend)).
		SetSuite(diagnosticrun.Suite(current.normalized.Suite)).
		SetRequestedBy(actorID).
		SetIdempotencyKeyHash(keyHash).
		SetRequestFingerprint(fingerprint).
		SetPreflight(preflightValue).
		Save(ctx)
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType("user").SetActorID(actorID).
		SetAction("diagnostic.submitted").SetTargetType("diagnostic_run").SetTargetID(run.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectRecord.PublicID.String(), "experiment_id": experimentID.String(), "backend": current.normalized.Backend,
			"suite": current.normalized.Suite, "reserved_cost_milli": reservation, "confirmation_digest": preflight.ConfirmationDigest,
		}).Save(ctx); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return run, false, nil
}

func existingQuery(ctx context.Context, query *ent.DiagnosticRunQuery, projectID int, keyHash, fingerprint []byte) (*ent.DiagnosticRun, bool, error) {
	record, err := query.Where(
		diagnosticrun.ProjectIDEQ(projectID), diagnosticrun.IdempotencyKeyHashEQ(keyHash),
	).WithExperiment().WithProject().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !hmac.Equal(record.RequestFingerprint, fingerprint) {
		return nil, false, ErrIdempotencyConflict
	}
	return record, true, nil
}

func preflightMap(value Preflight) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func diagnosticRepositorySnapshot(record *ent.Repository, projectID string) map[string]any {
	return map[string]any{
		"id": record.PublicID.String(), "project_id": projectID, "name": record.Name, "ssh_url": record.SSHURL,
		"default_branch": record.DefaultBranch, "host_key_fingerprint": record.HostKeyFingerprint,
	}
}

func diagnosticEnvironmentSnapshot(record *ent.Environment) map[string]any {
	return map[string]any{"id": record.PublicID.String(), "name": record.Name, "backend": record.Backend, "image_uuid": record.ImageUUID}
}

func diagnosticResourceSnapshot(record *ent.ResourceProfile) map[string]any {
	return map[string]any{
		"id": record.PublicID.String(), "name": record.Name, "backend": record.Backend, "region": record.Region,
		"gpu_names": record.GpuNames, "gpu_num": record.GpuNum, "cuda_from": record.CudaFrom, "cuda_to": record.CudaTo,
		"cpu_from": record.CPUFrom, "cpu_to": record.CPUTo, "memory_from_gb": record.MemoryFromGB, "memory_to_gb": record.MemoryToGB,
		"price_from_milli": record.PriceFromMilli, "price_to_milli": record.PriceToMilli, "reuse_container": false,
	}
}

func retryableTransaction(err error) bool {
	var sqlState interface{ SQLState() string }
	return errors.As(err, &sqlState) && (sqlState.SQLState() == "40001" || sqlState.SQLState() == "40P01")
}
