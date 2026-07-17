package watchdog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/ent/serviceheartbeat"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/notification"
	"github.com/XR-Lee/Gemcp/internal/servicehealth"
)

type Config struct {
	InstanceID     string
	PollInterval   time.Duration
	LeaseDuration  time.Duration
	ReconcileDelay time.Duration
	BatchSize      int
}

func DefaultConfig() Config {
	return Config{PollInterval: 10 * time.Second, LeaseDuration: 2 * time.Minute, ReconcileDelay: 2 * time.Minute, BatchSize: 100}
}

type Service struct {
	client   *ent.Client
	provider execution.LifecycleProvider
	config   Config
	now      func() time.Time
}

func New(client *ent.Client, provider execution.LifecycleProvider, config Config) (*Service, error) {
	if client == nil || provider == nil {
		return nil, fmt.Errorf("watchdog dependencies are required")
	}
	if strings.TrimSpace(config.InstanceID) == "" || config.PollInterval <= 0 || config.LeaseDuration < 30*time.Second || config.ReconcileDelay <= 0 || config.BatchSize <= 0 || config.BatchSize > 1000 {
		return nil, fmt.Errorf("watchdog configuration is invalid")
	}
	return &Service{client: client, provider: provider, config: config, now: time.Now}, nil
}

func (s *Service) Run(ctx context.Context) error {
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("watchdog tick failed", "error", err)
	}
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("watchdog tick failed", "error", err)
			}
		}
	}
}

func (s *Service) Tick(ctx context.Context) error {
	now := s.now().UTC()
	heartbeatErr := servicehealth.Beat(ctx, s.client, serviceheartbeat.RoleWatchdog, s.config.InstanceID, map[string]any{
		"poll_interval_seconds": s.config.PollInterval.Seconds(), "batch_size": s.config.BatchSize,
	})
	records, err := s.client.ProviderResource.Query().Where(
		providerresource.OwnedEQ(true),
		providerresource.StateIn(
			providerresource.StateCreating, providerresource.StateActive, providerresource.StateStopping,
			providerresource.StateStopped, providerresource.StateDeleting,
		),
		providerresource.Or(
			providerresource.HardDeadlineAtLTE(now),
			providerresource.And(
				providerresource.StopRequestedAtNotNil(),
				providerresource.Or(providerresource.StopReasonIsNil(), providerresource.StopReasonNEQ("timeout"), providerresource.HardDeadlineAtIsNil()),
			),
		),
		providerresource.Or(providerresource.LeaseExpiresAtIsNil(), providerresource.LeaseExpiresAtLT(now)),
	).Order(ent.Asc(providerresource.FieldHardDeadlineAt), ent.Asc(providerresource.FieldID)).Limit(s.config.BatchSize).All(ctx)
	if err != nil {
		return err
	}
	var tickErrors []error
	for _, record := range records {
		claimed, err := s.client.ProviderResource.Update().Where(
			providerresource.IDEQ(record.ID),
			providerresource.Or(providerresource.LeaseExpiresAtIsNil(), providerresource.LeaseExpiresAtLT(now)),
		).SetLeaseOwner(s.config.InstanceID).SetLeaseExpiresAt(now.Add(s.config.LeaseDuration)).Save(ctx)
		if err != nil {
			tickErrors = append(tickErrors, err)
			continue
		}
		if claimed != 1 {
			continue
		}
		if err := s.enforce(ctx, record.ID, now); err != nil {
			tickErrors = append(tickErrors, fmt.Errorf("resource %d: %w", record.ID, err))
		}
		s.release(record.ID)
	}
	return errors.Join(append([]error{heartbeatErr}, tickErrors...)...)
}

func (s *Service) enforce(ctx context.Context, resourceID int, now time.Time) error {
	record, err := s.client.ProviderResource.Query().Where(providerresource.IDEQ(resourceID)).WithExperiment().WithProviderAccount().Only(ctx)
	if err != nil {
		return err
	}
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return err
	}
	account, err := record.Edges.ProviderAccountOrErr()
	if err != nil {
		return err
	}
	reason := ""
	if record.StopReason != nil {
		reason = *record.StopReason
	}
	if reason == "" && record.HardDeadlineAt != nil && !now.Before(*record.HardDeadlineAt) {
		if experimentRecord.State == "provisioning" {
			reason = "provision_timeout"
		} else {
			reason = "timeout"
		}
		if _, err := record.Update().SetStopRequestedAt(now).SetStopReason(reason).Save(ctx); err != nil {
			return err
		}
	}
	if reason == "" {
		reason = "watchdog"
	}
	if experimentRecord.State != "collecting" && !terminalExperiment(experimentRecord.State) {
		update := experimentRecord.Update().SetState("cancelling")
		if reason == "provision_timeout" {
			update.SetFailureCode("provision_timeout").SetFailureReason("Watchdog enforced the provisioning deadline")
		}
		if _, err := update.Save(ctx); err != nil {
			return err
		}
	}

	if record.ProviderID == nil {
		observation, err := s.provider.FindByName(ctx, account, record.Name)
		if err != nil {
			return s.recordError(record.ID, observation.RequestIDs, err)
		}
		if !observation.Found {
			if record.CreateAttempts > 0 && record.CreateAttemptedAt != nil && now.Before(record.CreateAttemptedAt.Add(s.config.ReconcileDelay)) {
				return nil
			}
			if _, err := record.Update().SetState(providerresource.StateDeleted).SetDeletedAt(now).Save(ctx); err != nil {
				return err
			}
			return s.audit(ctx, experimentRecord, record, reason, "not_found")
		}
		record, err = record.Update().SetProviderID(observation.Deployment.UUID).SetState(providerresource.StateActive).
			SetProviderStatus(observation.Deployment.Status).SetLastSeenAt(now).SetRequestIds(merge(record.RequestIds, observation.RequestIDs)).Save(ctx)
		if err != nil {
			return err
		}
	}

	if record.State == providerresource.StateDeleting {
		return s.confirmDeletion(ctx, experimentRecord, record, account, reason, now)
	}
	if record.State != providerresource.StateStopped {
		record, err = record.Update().SetState(providerresource.StateStopping).Save(ctx)
		if err != nil {
			return err
		}
		requestIDs, err := s.provider.Stop(ctx, account, *record.ProviderID)
		if err != nil {
			return s.recordError(record.ID, requestIDs, err)
		}
		record, err = record.Update().SetState(providerresource.StateStopped).SetStoppedAt(now).
			SetRequestIds(merge(record.RequestIds, requestIDs)).ClearLastError().Save(ctx)
		if err != nil {
			return err
		}
	}
	record, err = record.Update().SetState(providerresource.StateDeleting).Save(ctx)
	if err != nil {
		return err
	}
	requestIDs, err := s.provider.Delete(ctx, account, *record.ProviderID)
	if err != nil {
		return s.recordError(record.ID, requestIDs, err)
	}
	record, err = record.Update().SetDeleteRequestedAt(now).
		SetRequestIds(merge(record.RequestIds, requestIDs)).ClearLastError().Save(ctx)
	if err != nil {
		return err
	}
	return s.confirmDeletion(ctx, experimentRecord, record, account, reason, now)
}

func (s *Service) confirmDeletion(ctx context.Context, experimentRecord *ent.Experiment, record *ent.ProviderResource, account *ent.ProviderAccount, reason string, now time.Time) error {
	observation, err := s.provider.Observe(ctx, account, *record.ProviderID)
	if err != nil {
		return s.recordError(record.ID, observation.RequestIDs, err)
	}
	requestIDs := merge(record.RequestIds, observation.RequestIDs)
	if !observation.Found {
		if _, err := record.Update().SetState(providerresource.StateDeleted).SetDeletedAt(now).
			SetRequestIds(requestIDs).ClearLastError().Save(ctx); err != nil {
			return err
		}
		return s.audit(ctx, experimentRecord, record, reason, "deleted")
	}
	record, err = record.Update().SetProviderStatus(observation.Deployment.Status).SetLastSeenAt(now).
		SetRequestIds(requestIDs).ClearLastError().Save(ctx)
	if err != nil {
		return err
	}
	if record.DeleteRequestedAt != nil && now.Before(record.DeleteRequestedAt.Add(s.config.ReconcileDelay)) {
		return nil
	}
	requestIDs, err = s.provider.Delete(ctx, account, *record.ProviderID)
	if err != nil {
		return s.recordError(record.ID, requestIDs, err)
	}
	_, err = record.Update().SetDeleteRequestedAt(now).SetRequestIds(merge(record.RequestIds, requestIDs)).ClearLastError().Save(ctx)
	return err
}

func (s *Service) audit(ctx context.Context, experimentRecord *ent.Experiment, record *ent.ProviderResource, reason, outcome string) error {
	if err := notification.Enqueue(ctx, s.client.Notification, notification.EnqueueInput{
		TenantID: record.TenantID, DedupKey: "watchdog-stop:" + record.PublicID.String(), Kind: "watchdog_stop",
		Severity: "critical", Subject: "[Gemcp] Watchdog enforced Provider shutdown",
		Body: fmt.Sprintf("Experiment: %s\nManaged resource: %s\nReason: %s\nOutcome: %s", experimentRecord.PublicID.String(), record.PublicID.String(), reason, outcome),
	}); err != nil {
		return err
	}
	_, err := s.client.AuditEvent.Create().SetTenantID(record.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
		SetAction("watchdog.stop_enforced").SetTargetType("provider_resource").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"experiment_id": experimentRecord.PublicID.String(), "reason": reason, "outcome": outcome}).Save(ctx)
	return err
}

func (s *Service) recordError(resourceID int, requestIDs map[string]string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	record, err := s.client.ProviderResource.Get(ctx, resourceID)
	if err != nil {
		return err
	}
	_, updateErr := record.Update().SetLastError(boundError(cause)).SetRequestIds(merge(record.RequestIds, requestIDs)).Save(ctx)
	if updateErr != nil {
		return errors.Join(cause, updateErr)
	}
	return cause
}

func (s *Service) release(resourceID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.client.ProviderResource.Update().Where(
		providerresource.IDEQ(resourceID), providerresource.LeaseOwnerEQ(s.config.InstanceID),
	).ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx)
}

func merge(existing, incoming map[string]string) map[string]string {
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

func boundError(err error) string {
	value := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512]) + "..."
	}
	return value
}

func terminalExperiment(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "timed_out", "budget_stopped", "provider_error":
		return true
	default:
		return false
	}
}
