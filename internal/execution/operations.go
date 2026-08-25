package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/cloudsshassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/ent/serviceheartbeat"
	"github.com/XR-Lee/Gemcp/internal/notification"
	"github.com/XR-Lee/Gemcp/internal/servicehealth"
)

var (
	ErrManagedResourceNotFound = errors.New("managed Provider resource not found")
	ErrManagedResourceTerminal = errors.New("managed Provider resource is already terminal")
)

type ManagedResource struct {
	ID              string     `json:"id"`
	ExperimentID    string     `json:"experiment_id"`
	AttemptID       string     `json:"attempt_id"`
	ProviderID      *string    `json:"provider_id,omitempty"`
	Name            string     `json:"name"`
	State           string     `json:"state"`
	ProviderStatus  *string    `json:"provider_status,omitempty"`
	HardDeadlineAt  *time.Time `json:"hard_deadline_at,omitempty"`
	StopRequestedAt *time.Time `json:"stop_requested_at,omitempty"`
	StopReason      *string    `json:"stop_reason,omitempty"`
	LastSeenAt      *time.Time `json:"last_seen_at,omitempty"`
	LastError       *string    `json:"last_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type EmergencyStopResult struct {
	Requested int       `json:"requested"`
	At        time.Time `json:"at"`
}

type RuntimeStatus struct {
	SchedulerEnabled          bool                     `json:"scheduler_enabled"`
	SelfHostedEnabled         bool                     `json:"self_hosted_enabled"`
	SSHCloudEnabled           bool                     `json:"ssh_cloud_enabled"`
	GlobalConcurrency         int                      `json:"global_concurrency"`
	PublicURLConfigured       bool                     `json:"public_url_configured"`
	PublicURLHTTPS            bool                     `json:"public_url_https"`
	SchedulerHealthy          bool                     `json:"scheduler_healthy"`
	WatchdogHealthy           bool                     `json:"watchdog_healthy"`
	NotificationWorkerHealthy bool                     `json:"notification_worker_healthy"`
	SchedulerHeartbeat        *servicehealth.Heartbeat `json:"scheduler_heartbeat,omitempty"`
	WatchdogHeartbeat         *servicehealth.Heartbeat `json:"watchdog_heartbeat,omitempty"`
	NotificationHeartbeat     *servicehealth.Heartbeat `json:"notification_heartbeat,omitempty"`
	GeneratedAt               time.Time                `json:"generated_at"`
}

type Operations struct {
	client              *ent.Client
	schedulerEnabled    bool
	selfHostedEnabled   bool
	sshCloudEnabled     bool
	globalConcurrency   int
	publicURLConfigured bool
	publicURLHTTPS      bool
	now                 func() time.Time
}

type OperationsOption func(*Operations)

func WithRuntimeConfiguration(schedulerEnabled bool, globalConcurrency int, publicURLConfigured bool, publicURLHTTPS ...bool) OperationsOption {
	return func(operations *Operations) {
		operations.schedulerEnabled = schedulerEnabled
		operations.globalConcurrency = globalConcurrency
		operations.publicURLConfigured = publicURLConfigured
		if len(publicURLHTTPS) > 0 {
			operations.publicURLHTTPS = publicURLHTTPS[0]
		}
	}
}

func WithSelfHostedEnabled(enabled bool) OperationsOption {
	return func(operations *Operations) {
		operations.selfHostedEnabled = enabled
	}
}

func WithSSHCloudEnabled(enabled bool) OperationsOption {
	return func(operations *Operations) {
		operations.sshCloudEnabled = enabled
	}
}

func NewOperations(client *ent.Client, options ...OperationsOption) *Operations {
	operations := &Operations{client: client, globalConcurrency: 1, now: time.Now}
	for _, option := range options {
		option(operations)
	}
	return operations
}

func (s *Operations) Status(ctx context.Context) (RuntimeStatus, error) {
	result := RuntimeStatus{
		SchedulerEnabled: s.schedulerEnabled, SelfHostedEnabled: s.selfHostedEnabled, SSHCloudEnabled: s.sshCloudEnabled,
		GlobalConcurrency: s.globalConcurrency, PublicURLConfigured: s.publicURLConfigured, PublicURLHTTPS: s.publicURLHTTPS,
		GeneratedAt: s.now().UTC(),
	}
	var err error
	result.SchedulerHeartbeat, err = servicehealth.Latest(ctx, s.client, serviceheartbeat.RoleScheduler)
	if err != nil {
		return RuntimeStatus{}, err
	}
	result.WatchdogHeartbeat, err = servicehealth.Latest(ctx, s.client, serviceheartbeat.RoleWatchdog)
	if err != nil {
		return RuntimeStatus{}, err
	}
	result.NotificationHeartbeat, err = servicehealth.Latest(ctx, s.client, serviceheartbeat.RoleNotification)
	if err != nil {
		return RuntimeStatus{}, err
	}
	result.SchedulerHealthy = heartbeatHealthy(result.SchedulerHeartbeat, result.GeneratedAt)
	result.WatchdogHealthy = heartbeatHealthy(result.WatchdogHeartbeat, result.GeneratedAt)
	result.NotificationWorkerHealthy = heartbeatHealthy(result.NotificationHeartbeat, result.GeneratedAt)
	return result, nil
}

func (s *Operations) List(ctx context.Context, tenantID int) ([]ManagedResource, error) {
	records, err := s.client.ProviderResource.Query().Where(
		providerresource.TenantIDEQ(tenantID), providerresource.OwnedEQ(true),
	).WithExperiment().WithAttempt().Order(ent.Desc(providerresource.FieldCreatedAt)).Limit(500).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ManagedResource, 0, len(records))
	for _, record := range records {
		view, err := managedView(record)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *Operations) RequestStop(ctx context.Context, tenantID int, actorID, providerID string) (ManagedResource, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || len(providerID) > 255 {
		return ManagedResource{}, ErrManagedResourceNotFound
	}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ManagedResource{}, err
	}
	defer tx.Rollback()
	record, err := tx.ProviderResource.Query().Where(
		providerresource.TenantIDEQ(tenantID), providerresource.ProviderIDEQ(providerID), providerresource.OwnedEQ(true),
	).WithExperiment().WithAttempt().Only(ctx)
	if ent.IsNotFound(err) {
		return ManagedResource{}, ErrManagedResourceNotFound
	}
	if err != nil {
		return ManagedResource{}, err
	}
	if record.State == providerresource.StateDeleted || record.State == providerresource.StateError {
		return ManagedResource{}, ErrManagedResourceTerminal
	}
	now := s.now().UTC()
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return ManagedResource{}, err
	}
	if record.StopRequestedAt == nil {
		record, err = tx.ProviderResource.UpdateOneID(record.ID).SetStopRequestedAt(now).SetStopReason("owner_stop").Save(ctx)
		if err != nil {
			return ManagedResource{}, err
		}
	}
	if !isTerminalExperiment(experimentRecord.State) {
		update := tx.Experiment.UpdateOneID(experimentRecord.ID).SetDesiredState("cancelled").SetState("cancelling")
		if experimentRecord.CancelRequestedAt == nil {
			update.SetCancelRequestedAt(now)
		}
		if _, err := update.Save(ctx); err != nil {
			return ManagedResource{}, err
		}
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("provider.resource_stop_requested").SetTargetType("provider_resource").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"provider_id": providerID, "experiment_id": experimentRecord.PublicID.String()}).Save(ctx); err != nil {
		return ManagedResource{}, err
	}
	if err := tx.Commit(); err != nil {
		return ManagedResource{}, err
	}
	updated, err := s.client.ProviderResource.Query().Where(providerresource.IDEQ(record.ID)).WithExperiment().WithAttempt().Only(ctx)
	if err != nil {
		return ManagedResource{}, err
	}
	return managedView(updated)
}

func (s *Operations) EmergencyStop(ctx context.Context, tenantID int, actorID string) (EmergencyStopResult, error) {
	result := EmergencyStopResult{At: s.now().UTC()}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	records, err := tx.ProviderResource.Query().Where(
		providerresource.TenantIDEQ(tenantID), providerresource.OwnedEQ(true),
		providerresource.StateIn(providerresource.StateCreating, providerresource.StateActive, providerresource.StateStopping, providerresource.StateStopped, providerresource.StateDeleting),
	).WithExperiment().All(ctx)
	if err != nil {
		return result, err
	}
	for _, record := range records {
		experimentRecord, err := record.Edges.ExperimentOrErr()
		if err != nil {
			return result, err
		}
		if record.StopRequestedAt == nil {
			if _, err := tx.ProviderResource.UpdateOneID(record.ID).SetStopRequestedAt(result.At).SetStopReason("emergency").Save(ctx); err != nil {
				return result, err
			}
		}
		if !isTerminalExperiment(experimentRecord.State) {
			update := tx.Experiment.UpdateOneID(experimentRecord.ID).SetDesiredState("cancelled").SetState("cancelling").
				SetFailureCode("emergency_stop").SetFailureReason("Owner requested emergency shutdown")
			if experimentRecord.CancelRequestedAt == nil {
				update.SetCancelRequestedAt(result.At)
			}
			if _, err := update.Save(ctx); err != nil {
				return result, err
			}
		}
		result.Requested++
	}
	assignments, err := tx.NodeAssignment.Query().Where(
		nodeassignment.TenantIDEQ(tenantID),
		nodeassignment.StateIn(nodeassignment.StateStarting, nodeassignment.StateRunning, nodeassignment.StateStopping, nodeassignment.StateCollecting),
	).WithExperiment().All(ctx)
	if err != nil {
		return result, err
	}
	for _, assignment := range assignments {
		experimentRecord, err := assignment.Edges.ExperimentOrErr()
		if err != nil {
			return result, err
		}
		if assignment.StopRequestedAt == nil {
			if _, err := tx.NodeAssignment.UpdateOneID(assignment.ID).SetStopRequestedAt(result.At).SetStopReason("emergency").Save(ctx); err != nil {
				return result, err
			}
		}
		if !isTerminalExperiment(experimentRecord.State) {
			update := tx.Experiment.UpdateOneID(experimentRecord.ID).SetDesiredState("cancelled").SetState("cancelling").
				SetFailureCode("emergency_stop").SetFailureReason("Owner requested emergency shutdown")
			if experimentRecord.CancelRequestedAt == nil {
				update.SetCancelRequestedAt(result.At)
			}
			if _, err := update.Save(ctx); err != nil {
				return result, err
			}
		}
		result.Requested++
	}
	sshAssignments, err := tx.CloudSSHAssignment.Query().Where(
		cloudsshassignment.TenantIDEQ(tenantID),
		cloudsshassignment.StateIn(cloudsshassignment.StateStarting, cloudsshassignment.StateRunning, cloudsshassignment.StateStopping, cloudsshassignment.StateCollecting),
	).WithExperiment().All(ctx)
	if err != nil {
		return result, err
	}
	for _, assignment := range sshAssignments {
		experimentRecord, err := assignment.Edges.ExperimentOrErr()
		if err != nil {
			return result, err
		}
		if assignment.StopRequestedAt == nil {
			if _, err := tx.CloudSSHAssignment.UpdateOneID(assignment.ID).SetStopRequestedAt(result.At).SetStopReason("emergency").Save(ctx); err != nil {
				return result, err
			}
		}
		if !isTerminalExperiment(experimentRecord.State) {
			update := tx.Experiment.UpdateOneID(experimentRecord.ID).SetDesiredState("cancelled").SetState("cancelling").
				SetFailureCode("emergency_stop").SetFailureReason("Owner requested emergency shutdown")
			if experimentRecord.CancelRequestedAt == nil {
				update.SetCancelRequestedAt(result.At)
			}
			if _, err := update.Save(ctx); err != nil {
				return result, err
			}
		}
		result.Requested++
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("provider.emergency_stop_requested").SetTargetType("provider_account").
		SetMetadata(map[string]any{"resource_count": result.Requested}).Save(ctx); err != nil {
		return result, err
	}
	if result.Requested > 0 {
		if err := notification.Enqueue(ctx, tx.Notification, notification.EnqueueInput{
			TenantID: tenantID, DedupKey: "emergency-stop:" + result.At.Format("20060102T150405.000000000Z"),
			Kind: "emergency_stop", Severity: "critical", Subject: "[Gemcp] Emergency stop requested",
			Body: fmt.Sprintf("The Owner requested emergency shutdown for %d managed compute resources at %s.", result.Requested, result.At.Format(time.RFC3339)),
		}); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func heartbeatHealthy(heartbeat *servicehealth.Heartbeat, now time.Time) bool {
	if heartbeat == nil || heartbeat.Status != "running" {
		return false
	}
	maximumAge := 2 * time.Minute
	if value, ok := heartbeat.Metadata["poll_interval_seconds"].(float64); ok && value > 0 {
		candidate := time.Duration(value*3)*time.Second + 15*time.Second
		if candidate > maximumAge {
			maximumAge = candidate
		}
	}
	return now.Sub(heartbeat.LastSeenAt) <= maximumAge
}

func managedView(record *ent.ProviderResource) (ManagedResource, error) {
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return ManagedResource{}, fmt.Errorf("managed resource has no experiment: %w", err)
	}
	attemptRecord, err := record.Edges.AttemptOrErr()
	if err != nil {
		return ManagedResource{}, fmt.Errorf("managed resource has no attempt: %w", err)
	}
	return ManagedResource{
		ID: record.PublicID.String(), ExperimentID: experimentRecord.PublicID.String(), AttemptID: attemptRecord.PublicID.String(),
		ProviderID: record.ProviderID, Name: record.Name, State: string(record.State), ProviderStatus: record.ProviderStatus,
		HardDeadlineAt: record.HardDeadlineAt, StopRequestedAt: record.StopRequestedAt, StopReason: record.StopReason,
		LastSeenAt: record.LastSeenAt, LastError: record.LastError, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}
