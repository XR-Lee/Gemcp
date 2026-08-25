package runner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/internal/executionmeta"
	"github.com/XR-Lee/Gemcp/internal/notification"
	"github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

const (
	defaultSourceMaxBytes     int64 = 256 << 20
	shutdownObservationMargin       = 30 * time.Second
)

type SourceArchiver interface {
	ArchiveCommit(context.Context, int, string, int64) (repository.Archive, error)
}

type Service struct {
	client         *ent.Client
	box            *secrets.Box
	archiver       SourceArchiver
	sourceMaxBytes int64
	now            func() time.Time
}

type Option func(*Service)

func WithSourceMaxBytes(value int64) Option {
	return func(service *Service) {
		if value > 0 {
			service.sourceMaxBytes = value
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func NewService(client *ent.Client, box *secrets.Box, archiver SourceArchiver, options ...Option) *Service {
	service := &Service{client: client, box: box, archiver: archiver, sourceMaxBytes: defaultSourceMaxBytes, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

type session struct {
	attempt    *ent.Attempt
	experiment *ent.Experiment
	resource   *ent.ProviderResource
}

func (s *Service) Spec(ctx context.Context, token string) (Spec, error) {
	session, err := s.authenticate(ctx, token)
	if err != nil {
		return Spec{}, err
	}
	if isTerminalState(session.experiment.State) || session.experiment.DesiredState == "cancelled" || (session.experiment.State != "provisioning" && session.experiment.State != "running") {
		return Spec{}, ErrTerminal
	}
	var provisioningSecondsRemaining int
	if session.resource.HardDeadlineAt != nil {
		remaining := session.resource.HardDeadlineAt.Sub(s.now().UTC())
		if remaining > 0 {
			provisioningSecondsRemaining = int(remaining / time.Second)
		}
	}
	executionMode := string(session.experiment.ExecutionMode)
	command := session.experiment.Command
	argv := append([]string(nil), session.experiment.Argv...)
	if executionMode == "argv" {
		command = ""
	} else {
		argv = nil
	}
	return Spec{
		ExperimentID: session.experiment.PublicID.String(), AttemptID: session.attempt.PublicID.String(),
		ExecutionMode: executionMode, Command: command, Argv: argv, OutputPath: session.experiment.OutputPath,
		MaxRuntimeSeconds:        session.experiment.MaxRuntimeSeconds,
		TimeoutExtensionSeconds:  session.experiment.TimeoutExtensionSeconds,
		TerminationGraceSeconds:  session.experiment.TerminationGraceSeconds,
		HeartbeatIntervalSeconds: 15, SourceMaxBytes: sourceTransferLimit(s.sourceMaxBytes),
		ProvisioningSecondsRemaining: provisioningSecondsRemaining,
		TokenExpiresAt:               *session.attempt.RunnerTokenExpiresAt,
		DatasetBindings:              runnerDatasetBindings(session.experiment.EnvironmentSnapshot),
	}, nil
}

func (s *Service) Source(ctx context.Context, token string) (repository.Archive, error) {
	session, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if isTerminalState(session.experiment.State) || session.experiment.DesiredState == "cancelled" || (session.experiment.State != "provisioning" && session.experiment.State != "running") {
		return nil, ErrTerminal
	}
	updated, err := s.client.Attempt.Update().Where(
		attempt.IDEQ(session.attempt.ID), attempt.SourceDownloadsLT(MaxSourceDownloads),
	).AddSourceDownloads(1).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve source download: %w", err)
	}
	if updated != 1 {
		return nil, ErrSourceLimit
	}
	releaseReservation := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.client.Attempt.Update().Where(attempt.IDEQ(session.attempt.ID), attempt.SourceDownloadsGT(0)).AddSourceDownloads(-1).Save(releaseCtx)
	}
	if s.archiver == nil {
		releaseReservation()
		return nil, fmt.Errorf("Runner source archiver is unavailable")
	}
	if session.experiment.RepositoryID == nil {
		releaseReservation()
		return nil, fmt.Errorf("experiment source repository is missing")
	}
	archive, err := s.archiver.ArchiveCommit(ctx, *session.experiment.RepositoryID, session.experiment.CommitSha, s.sourceMaxBytes)
	if err != nil {
		releaseReservation()
		return nil, fmt.Errorf("archive experiment source: %w", err)
	}
	return archive, nil
}

func (s *Service) Event(ctx context.Context, token string, input EventInput) (Control, error) {
	if err := validateEvent(input); err != nil {
		return Control{}, err
	}
	session, err := s.authenticate(ctx, token)
	if err != nil {
		return Control{}, err
	}

	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Control{}, fmt.Errorf("begin Runner event transaction: %w", err)
	}
	defer tx.Rollback()
	record, err := tx.Attempt.Query().Where(attempt.IDEQ(session.attempt.ID)).WithExperiment().WithOwnedResource().Only(ctx)
	if err != nil {
		return Control{}, fmt.Errorf("reload Runner attempt: %w", err)
	}
	now := s.now().UTC()
	if len(record.RunnerTokenHash) == 0 || record.RunnerTokenExpiresAt == nil || !now.Before(*record.RunnerTokenExpiresAt) ||
		(record.State != "starting" && record.State != "running" && record.State != "collecting") {
		return Control{}, ErrTerminal
	}
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return Control{}, err
	}
	resourceRecord, err := record.Edges.OwnedResourceOrErr()
	if err != nil {
		return Control{}, err
	}
	if isTerminalState(experimentRecord.State) {
		control := controlFrom(experimentRecord, resourceRecord)
		if err := tx.Commit(); err != nil {
			return Control{}, err
		}
		return control, nil
	}

	switch input.Type {
	case "diagnostic":
		err = s.recordBootstrapDiagnostic(ctx, tx, record, experimentRecord, resourceRecord, input.Stage, input.ErrorType, now)
	case "started":
		err = s.recordStarted(ctx, tx, record, experimentRecord, resourceRecord, input, now)
	case "heartbeat":
		err = s.recordHeartbeat(ctx, tx, record, experimentRecord, resourceRecord, input, now)
	case "finished":
		err = s.recordFinished(ctx, tx, record, experimentRecord, resourceRecord, input, now)
	}
	if err != nil {
		return Control{}, err
	}

	experimentRecord, err = tx.Experiment.Get(ctx, experimentRecord.ID)
	if err != nil {
		return Control{}, err
	}
	resourceRecord, err = tx.ProviderResource.Get(ctx, resourceRecord.ID)
	if err != nil {
		return Control{}, err
	}
	control := controlFrom(experimentRecord, resourceRecord)
	if err := tx.Commit(); err != nil {
		return Control{}, fmt.Errorf("commit Runner event: %w", err)
	}
	return control, nil
}

func (s *Service) recordStarted(ctx context.Context, tx *ent.Tx, record *ent.Attempt, experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource, input EventInput, now time.Time) error {
	var runtimeInfo *executionmeta.RuntimeInfo
	if input.RuntimeInfo != nil {
		validated, err := executionmeta.Validate(*input.RuntimeInfo, experimentRecord.OutputPath)
		if err != nil {
			return ErrInvalidEvent
		}
		runtimeInfo = &validated
	}
	if record.State == "starting" {
		update := tx.Attempt.UpdateOneID(record.ID).SetState("running").SetLastHeartbeatAt(now)
		if record.StartedAt == nil {
			update.SetStartedAt(now)
		}
		if _, err := update.Save(ctx); err != nil {
			return err
		}
	} else if _, err := tx.Attempt.UpdateOneID(record.ID).SetLastHeartbeatAt(now).Save(ctx); err != nil {
		return err
	}
	if experimentRecord.State == "provisioning" {
		deadline := now.Add(time.Duration(experimentRecord.MaxRuntimeSeconds) * time.Second)
		hardDeadline := deadline.Add(time.Duration(experimentRecord.TimeoutExtensionSeconds+experimentRecord.TerminationGraceSeconds)*time.Second + shutdownObservationMargin)
		update := tx.Experiment.UpdateOneID(experimentRecord.ID).SetState("running").SetStartedAt(now).SetDeadlineAt(deadline)
		if _, err := update.Save(ctx); err != nil {
			return err
		}
		if _, err := tx.ProviderResource.UpdateOneID(resourceRecord.ID).SetState("active").SetLastSeenAt(now).SetHardDeadlineAt(hardDeadline).Save(ctx); err != nil {
			return err
		}
		if err := s.recordDiagnostic(ctx, tx, record, experimentRecord, "started", "", now); err != nil {
			return err
		}
		metadata := map[string]any{"attempt_id": record.PublicID.String(), "deadline_at": deadline}
		if runtimeInfo != nil {
			metadata["runtime_info"] = runtimeInfo
		}
		_, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID("runner").
			SetAction("experiment.started").SetTargetType("experiment").SetTargetID(experimentRecord.PublicID.String()).
			SetMetadata(metadata).Save(ctx)
		return err
	}
	return nil
}

func (s *Service) recordDiagnostic(ctx context.Context, tx *ent.Tx, record *ent.Attempt, experimentRecord *ent.Experiment, stage, errorType string, now time.Time) error {
	metadata := map[string]any{
		"attempt_id":  record.PublicID.String(),
		"stage":       stage,
		"recorded_at": now,
	}
	if errorType != "" {
		metadata["error_type"] = errorType
	}
	_, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID("runner").
		SetAction("runner.bootstrap_stage").SetTargetType("experiment").SetTargetID(experimentRecord.PublicID.String()).
		SetMetadata(metadata).Save(ctx)
	return err
}

func (s *Service) recordBootstrapDiagnostic(ctx context.Context, tx *ent.Tx, record *ent.Attempt, experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource, stage, errorType string, now time.Time) error {
	if err := s.recordDiagnostic(ctx, tx, record, experimentRecord, stage, errorType, now); err != nil {
		return err
	}
	if !strings.HasPrefix(stage, "bootstrap_failed_") {
		return nil
	}
	if experimentRecord.DesiredState == "cancelled" {
		return requestResourceStop(ctx, tx, resourceRecord, "cancelled", now)
	}
	phase := strings.ReplaceAll(strings.TrimPrefix(stage, "bootstrap_failed_"), "_", " ")
	reason := fmt.Sprintf("Runner bootstrap failed %s (%s)", phase, errorType)
	if _, err := tx.Attempt.UpdateOneID(record.ID).SetState("failed").SetFinishedAt(now).SetRunnerTokenExpiresAt(now).
		ClearRunnerTokenHash().ClearRunnerTokenCiphertext().SetFailureCode("runner_bootstrap_failed").SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if _, err := tx.Experiment.UpdateOneID(experimentRecord.ID).SetState("collecting").SetFailureCode("runner_bootstrap_failed").SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	return requestResourceStop(ctx, tx, resourceRecord, "runner_bootstrap_failed", now)
}

func (s *Service) recordHeartbeat(ctx context.Context, tx *ent.Tx, record *ent.Attempt, experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource, input EventInput, now time.Time) error {
	attemptUpdate := tx.Attempt.UpdateOneID(record.ID).SetLastHeartbeatAt(now)
	experimentUpdate := tx.Experiment.UpdateOneID(experimentRecord.ID)
	if input.LogTail != "" {
		attemptUpdate.SetLogTail(input.LogTail)
		experimentUpdate.SetLogTail(input.LogTail)
	}
	if len(input.Metrics) > 0 {
		attemptUpdate.SetMetrics(input.Metrics)
		experimentUpdate.SetMetrics(input.Metrics)
	}
	if _, err := attemptUpdate.Save(ctx); err != nil {
		return err
	}
	if input.LogTail != "" || len(input.Metrics) > 0 {
		if _, err := experimentUpdate.Save(ctx); err != nil {
			return err
		}
	}
	if experimentRecord.DesiredState == "cancelled" {
		return requestResourceStop(ctx, tx, resourceRecord, "cancelled", now)
	}
	if experimentRecord.DeadlineAt == nil || now.Before(*experimentRecord.DeadlineAt) {
		return nil
	}
	if experimentRecord.TimeoutExtendedAt == nil && experimentRecord.TimeoutExtensionSeconds > 0 {
		deadline := experimentRecord.DeadlineAt.Add(time.Duration(experimentRecord.TimeoutExtensionSeconds) * time.Second)
		if _, err := tx.Experiment.UpdateOneID(experimentRecord.ID).SetDeadlineAt(deadline).SetTimeoutExtendedAt(now).Save(ctx); err != nil {
			return err
		}
		hardDeadline := deadline.Add(time.Duration(experimentRecord.TerminationGraceSeconds)*time.Second + shutdownObservationMargin)
		if _, err := tx.ProviderResource.UpdateOneID(resourceRecord.ID).SetHardDeadlineAt(hardDeadline).Save(ctx); err != nil {
			return err
		}
		if _, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID("runner").
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
			return nil
		}
	}
	if _, err := tx.Experiment.UpdateOneID(experimentRecord.ID).SetState("cancelling").Save(ctx); err != nil {
		return err
	}
	return requestResourceStop(ctx, tx, resourceRecord, "timeout", now)
}

func (s *Service) recordFinished(ctx context.Context, tx *ent.Tx, record *ent.Attempt, experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource, input EventInput, now time.Time) error {
	if input.Metrics == nil {
		input.Metrics = map[string]any{}
	}
	if record.State == "collecting" || record.State == "succeeded" || record.State == "failed" || record.State == "cancelled" {
		return nil
	}
	if _, err := tx.Attempt.UpdateOneID(record.ID).SetState("collecting").SetFinishedAt(now).SetLastHeartbeatAt(now).
		SetExitCode(*input.ExitCode).SetLogTail(input.LogTail).SetMetrics(input.Metrics).Save(ctx); err != nil {
		return err
	}
	experimentState := "collecting"
	stopReason := "completed"
	if experimentRecord.DesiredState == "cancelled" || input.Reason == "cancelled" || input.Reason == "emergency" {
		experimentState = "cancelling"
		stopReason = input.Reason
		if stopReason == "completed" {
			stopReason = "cancelled"
		}
	} else if input.Reason == "timeout" {
		experimentState = "cancelling"
		stopReason = "timeout"
	}
	if _, err := tx.Experiment.UpdateOneID(experimentRecord.ID).SetState(experimentState).SetExitCode(*input.ExitCode).
		SetLogTail(input.LogTail).SetMetrics(input.Metrics).Save(ctx); err != nil {
		return err
	}
	if err := requestResourceStop(ctx, tx, resourceRecord, stopReason, now); err != nil {
		return err
	}
	_, err := tx.AuditEvent.Create().SetTenantID(experimentRecord.TenantID).SetActorType("system").SetActorID("runner").
		SetAction("experiment.runner_finished").SetTargetType("experiment").SetTargetID(experimentRecord.PublicID.String()).
		SetMetadata(map[string]any{"attempt_id": record.PublicID.String(), "exit_code": *input.ExitCode, "reason": input.Reason}).Save(ctx)
	return err
}

func requestResourceStop(ctx context.Context, tx *ent.Tx, resourceRecord *ent.ProviderResource, reason string, now time.Time) error {
	update := tx.ProviderResource.UpdateOneID(resourceRecord.ID)
	if resourceRecord.StopRequestedAt == nil {
		update.SetStopRequestedAt(now).SetStopReason(reason)
	}
	_, err := update.Save(ctx)
	return err
}

func (s *Service) authenticate(ctx context.Context, token string) (session, error) {
	var result session
	if s == nil || s.client == nil || s.box == nil {
		return result, ErrUnauthenticated
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 4096 {
		return result, ErrUnauthenticated
	}
	hash := s.box.Digest(TokenDigestDomain, token)
	record, err := s.client.Attempt.Query().Where(attempt.RunnerTokenHashEQ(hash)).WithExperiment().WithOwnedResource().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return result, ErrUnauthenticated
		}
		return result, fmt.Errorf("authenticate Runner: %w", err)
	}
	if record.RunnerTokenExpiresAt == nil || !s.now().UTC().Before(*record.RunnerTokenExpiresAt) {
		return result, ErrExpired
	}
	if record.State != "starting" && record.State != "running" && record.State != "collecting" {
		return result, ErrTerminal
	}
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return result, ErrUnauthenticated
	}
	resourceRecord, err := record.Edges.OwnedResourceOrErr()
	if err != nil {
		return result, ErrUnauthenticated
	}
	result.attempt = record
	result.experiment = experimentRecord
	result.resource = resourceRecord
	return result, nil
}

func validateEvent(input EventInput) error {
	switch input.Type {
	case "diagnostic":
		failure := strings.HasPrefix(input.Stage, "bootstrap_failed_")
		if _, ok := diagnosticStages[input.Stage]; !ok || input.ExitCode != nil || input.LogTail != "" || len(input.Metrics) != 0 || input.Reason != "" ||
			input.RuntimeInfo != nil || (failure && !validErrorType(input.ErrorType)) || (!failure && input.ErrorType != "") {
			return ErrInvalidEvent
		}
	case "started":
		if input.Stage != "" || input.ErrorType != "" || input.ExitCode != nil || input.LogTail != "" || len(input.Metrics) != 0 || input.Reason != "" {
			return ErrInvalidEvent
		}
		if input.RuntimeInfo != nil {
			if _, err := executionmeta.Validate(*input.RuntimeInfo); err != nil {
				return ErrInvalidEvent
			}
		}
	case "heartbeat":
		if input.Stage != "" || input.ErrorType != "" || input.ExitCode != nil || input.Reason != "" || input.RuntimeInfo != nil || validateLiveOutput(input) != nil {
			return ErrInvalidEvent
		}
	case "finished":
		if input.Stage != "" || input.ErrorType != "" || input.RuntimeInfo != nil || input.ExitCode == nil || *input.ExitCode < 0 || *input.ExitCode > 255 {
			return ErrInvalidEvent
		}
		switch input.Reason {
		case "completed", "cancelled", "timeout", "emergency", "runner_error":
		default:
			return ErrInvalidEvent
		}
		if validateLiveOutput(input) != nil {
			return ErrInvalidEvent
		}
	default:
		return ErrInvalidEvent
	}
	return nil
}

func validateLiveOutput(input EventInput) error {
	if len([]byte(input.LogTail)) > MaxLogTailBytes || !utf8.ValidString(input.LogTail) {
		return ErrInvalidEvent
	}
	encoded, err := json.Marshal(input.Metrics)
	if err != nil || len(encoded) > MaxMetricsBytes {
		return ErrInvalidEvent
	}
	return nil
}

var diagnosticStages = map[string]struct{}{
	"runner_entered":                           {},
	"spec_loaded":                              {},
	"source_downloaded":                        {},
	"source_extracted":                         {},
	"bootstrap_failed_before_spec":             {},
	"bootstrap_failed_during_source_download":  {},
	"bootstrap_failed_during_source_extract":   {},
	"bootstrap_failed_during_started_callback": {},
}

func validErrorType(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for index := range value {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func controlFrom(experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource) Control {
	control := Control{DeadlineAt: experimentRecord.DeadlineAt}
	if experimentRecord.DesiredState == "cancelled" {
		control.StopRequested = true
		control.StopReason = "cancelled"
	}
	if resourceRecord.StopRequestedAt != nil {
		control.StopRequested = true
		if resourceRecord.StopReason != nil {
			control.StopReason = *resourceRecord.StopReason
		}
	}
	return control
}

func sourceTransferLimit(rawLimit int64) int64 {
	return rawLimit + rawLimit/100 + (64 << 10)
}

func isTerminalState(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "timed_out", "budget_stopped", "provider_error":
		return true
	default:
		return false
	}
}

func runnerDatasetBindings(snapshot map[string]any) []DatasetBinding {
	raw, ok := snapshot["dataset_bindings"]
	if !ok || raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var bindings []DatasetBinding
	if err := json.Unmarshal(encoded, &bindings); err != nil {
		return nil
	}
	return bindings
}
