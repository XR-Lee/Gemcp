package nodeaccess

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/nodecommand"
	"github.com/XR-Lee/Gemcp/ent/nodeenrollment"
	"github.com/XR-Lee/Gemcp/ent/nodeevent"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
)

func (s *Service) Authenticate(ctx context.Context, raw string) (Principal, error) {
	var principal Principal
	if err := s.ready(); err != nil {
		return principal, err
	}
	raw = strings.TrimSpace(raw)
	const prefixLength = len("gmn_") + 7
	if len(raw) < prefixLength+1+22 || !strings.HasPrefix(raw, "gmn_") || raw[prefixLength] != '_' {
		return principal, ErrInvalidToken
	}
	prefix := raw[:prefixLength]
	candidates, err := s.client.SelfHostedNode.Query().Where(
		selfhostednode.TokenPrefixEQ(prefix), selfhostednode.StatusNEQ(selfhostednode.StatusRevoked),
	).WithTenant().All(ctx)
	if err != nil {
		return principal, fmt.Errorf("query Node token: %w", err)
	}
	digest := s.box.Digest("node-token", raw)
	var matched *ent.SelfHostedNode
	for _, candidate := range candidates {
		if hmac.Equal(candidate.TokenHash, digest) {
			matched = candidate
			break
		}
	}
	if matched == nil || matched.Edges.Tenant == nil || matched.Edges.Tenant.Status != "active" {
		return principal, ErrInvalidToken
	}
	return Principal{
		TenantID: matched.TenantID, TenantPublicID: matched.Edges.Tenant.PublicID.String(),
		NodeID: matched.ID, NodePublicID: matched.PublicID.String(), NodeLabel: matched.Label, Status: string(matched.Status),
	}, nil
}

func (s *Service) Sync(ctx context.Context, rawToken string, input nodeprotocol.SyncRequest) (SyncResult, error) {
	var result SyncResult
	principal, err := s.Authenticate(ctx, rawToken)
	if err != nil {
		return result, err
	}
	if err := validateSyncRequest(input); err != nil {
		return result, err
	}
	now := s.now().UTC()
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin node synchronization: %w", err)
	}
	defer tx.Rollback()
	nodeRecord, err := tx.SelfHostedNode.Query().Where(
		selfhostednode.IDEQ(principal.NodeID), selfhostednode.StatusNEQ(selfhostednode.StatusRevoked),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrInvalidToken
	}
	if err != nil {
		return result, fmt.Errorf("reload synchronized node: %w", err)
	}
	if nodeRecord.InstallationID != input.InstallationID {
		return result, ErrInvalidToken
	}
	if nodeRecord.MachineFingerprint != input.MachineFingerprint {
		nodeRecord, err = nodeRecord.Update().
			SetStatus(selfhostednode.StatusVerificationRequired).
			SetObservedState(selfhostednode.ObservedStateReconciling).
			SetAgentVersion(input.AgentVersion).
			SetProtocolVersion(input.ProtocolVersion).
			SetLastSeenAt(now).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("quarantine changed node identity: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return result, fmt.Errorf("commit changed node identity: %w", err)
		}
		return SyncResult{
			NodeID: nodeRecord.PublicID.String(), DesiredState: string(nodeRecord.Status), ServerTime: now,
			NextSyncSeconds: 15, Commands: []nodeprotocol.Command{},
		}, nil
	}
	observedState := selfhostednode.ObservedState(input.ObservedState)
	update := nodeRecord.Update().
		SetObservedState(observedState).
		SetAgentVersion(input.AgentVersion).
		SetProtocolVersion(input.ProtocolVersion).
		SetLastSeenAt(now)
	if input.Capabilities != nil {
		update.SetCapabilities(input.Capabilities)
	}
	if input.Storage != nil {
		update.SetStorage(input.Storage)
	}
	nodeRecord, err = update.Save(ctx)
	if err != nil {
		return result, fmt.Errorf("update synchronized node: %w", err)
	}
	if err := completeApprovedEnrollment(ctx, tx, nodeRecord, now); err != nil {
		return result, err
	}
	if err := applyCommandAcknowledgements(ctx, tx, nodeRecord, input.Acknowledgements, now, s.projector); err != nil {
		return result, err
	}
	ackedSequence, err := ingestEvents(ctx, tx, nodeRecord, input.Events, s.projector, now)
	if err != nil {
		return result, err
	}
	commands, err := deliverCommands(ctx, tx, nodeRecord, input.LastCommandSequence, now)
	if err != nil {
		return result, err
	}
	if nodeRecord.Status == selfhostednode.StatusPendingVerification || nodeRecord.Status == selfhostednode.StatusVerificationRequired {
		commands = []nodeprotocol.Command{}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit node synchronization: %w", err)
	}
	return SyncResult{
		NodeID: nodeRecord.PublicID.String(), DesiredState: string(nodeRecord.Status), ServerTime: now,
		AckedEventSequence: ackedSequence, NextSyncSeconds: 15, Commands: commands,
	}, nil
}

func validateSyncRequest(input nodeprotocol.SyncRequest) error {
	if _, err := uuid.Parse(strings.TrimSpace(input.InstallationID)); err != nil {
		return invalid("installation_id must be a UUID")
	}
	if len(input.MachineFingerprint) != 64 {
		return invalid("machine_fingerprint must be a 64-character digest")
	}
	if input.ProtocolVersion != nodeprotocol.Version {
		return ErrProtocol
	}
	if strings.TrimSpace(input.AgentVersion) == "" || len(input.AgentVersion) > 64 {
		return invalid("agent_version is required and must not exceed 64 characters")
	}
	switch selfhostednode.ObservedState(input.ObservedState) {
	case selfhostednode.ObservedStateOnline, selfhostednode.ObservedStateExternallyBusy, selfhostednode.ObservedStateReconciling, selfhostednode.ObservedStateIncompatible:
	default:
		return invalid("observed_state must be online, externally_busy, reconciling, or incompatible")
	}
	if input.LastCommandSequence < 0 {
		return invalid("last_command_sequence must not be negative")
	}
	if len(input.Events) > maxEventsPerSync || len(input.Acknowledgements) > maxAcksPerSync {
		return invalid("node synchronization batch exceeds its limit")
	}
	return nil
}

func completeApprovedEnrollment(ctx context.Context, tx *ent.Tx, nodeRecord *ent.SelfHostedNode, now time.Time) error {
	record, err := tx.NodeEnrollment.Query().Where(
		nodeenrollment.NodeIDEQ(nodeRecord.ID), nodeenrollment.StatusEQ(nodeenrollment.StatusApproved),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load approved node enrollment: %w", err)
	}
	record, err = record.Update().SetStatus(nodeenrollment.StatusCompleted).SetCompletedAt(now).Save(ctx)
	if err != nil {
		return fmt.Errorf("complete node enrollment: %w", err)
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(nodeRecord.TenantID).SetActorType("self_hosted_node").SetActorID(nodeRecord.PublicID.String()).
		SetAction("node_enrollment.completed").SetTargetType("node_enrollment").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"node_id": nodeRecord.PublicID.String(), "completed_at": now.Format(time.RFC3339)}).
		Save(ctx); err != nil {
		return fmt.Errorf("write node completion audit: %w", err)
	}
	return nil
}

func ingestEvents(ctx context.Context, tx *ent.Tx, nodeRecord *ent.SelfHostedNode, events []nodeprotocol.Event, projector EventProjector, now time.Time) (int64, error) {
	latest, err := tx.NodeEvent.Query().Where(nodeevent.NodeIDEQ(nodeRecord.ID)).Order(ent.Desc(nodeevent.FieldSequence)).First(ctx)
	current := int64(0)
	if err == nil {
		current = latest.Sequence
	} else if !ent.IsNotFound(err) {
		return 0, fmt.Errorf("load node event sequence: %w", err)
	}
	previousInput := int64(0)
	for _, event := range events {
		if _, err := uuid.Parse(strings.TrimSpace(event.ID)); err != nil || event.Sequence <= 0 || strings.TrimSpace(event.Kind) == "" || len(event.Kind) > 80 || event.OccurredAt.IsZero() {
			return current, invalid("node event contains invalid id, sequence, kind, or occurred_at")
		}
		if previousInput != 0 && event.Sequence <= previousInput {
			return current, ErrConflict
		}
		previousInput = event.Sequence
		if event.Sequence <= current {
			exists, err := tx.NodeEvent.Query().Where(
				nodeevent.NodeIDEQ(nodeRecord.ID), nodeevent.EventIDEQ(event.ID),
				nodeevent.SequenceEQ(event.Sequence), nodeevent.KindEQ(event.Kind),
			).Exist(ctx)
			if err != nil {
				return current, fmt.Errorf("verify repeated node event: %w", err)
			}
			if !exists {
				return current, ErrConflict
			}
			continue
		}
		if event.Sequence != current+1 {
			return current, ErrConflict
		}
		payload := event.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		if _, err := tx.NodeEvent.Create().
			SetTenantID(nodeRecord.TenantID).SetNodeID(nodeRecord.ID).
			SetEventID(event.ID).SetSequence(event.Sequence).SetKind(event.Kind).
			SetPayload(payload).SetOccurredAt(event.OccurredAt.UTC()).Save(ctx); err != nil {
			return current, fmt.Errorf("store node event: %w", err)
		}
		if projector != nil {
			if err := projector.ProjectNodeEvent(ctx, tx, nodeRecord, event, now); err != nil {
				return current, fmt.Errorf("project node event: %w", err)
			}
		}
		current = event.Sequence
	}
	return current, nil
}

func applyCommandAcknowledgements(ctx context.Context, tx *ent.Tx, nodeRecord *ent.SelfHostedNode, acknowledgements []nodeprotocol.CommandAcknowledgement, now time.Time, projector EventProjector) error {
	for _, acknowledgement := range acknowledgements {
		id, err := uuid.Parse(strings.TrimSpace(acknowledgement.CommandID))
		if err != nil || len(acknowledgement.Error) > 4096 {
			return invalid("command acknowledgement contains an invalid command_id or error")
		}
		record, err := tx.NodeCommand.Query().Where(nodecommand.PublicIDEQ(id), nodecommand.NodeIDEQ(nodeRecord.ID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrConflict
		}
		if err != nil {
			return fmt.Errorf("load acknowledged node command: %w", err)
		}
		switch acknowledgement.Status {
		case "acknowledged":
			if record.Status == nodecommand.StatusPending || record.Status == nodecommand.StatusDelivered {
				if _, err := record.Update().SetStatus(nodecommand.StatusAcknowledged).SetAcknowledgedAt(now).Save(ctx); err != nil {
					return fmt.Errorf("acknowledge node command: %w", err)
				}
			}
		case "completed":
			if record.Status != nodecommand.StatusCompleted {
				update := record.Update().SetStatus(nodecommand.StatusCompleted).SetCompletedAt(now).ClearLastError()
				if acknowledgement.Result != nil {
					update.SetResult(acknowledgement.Result)
				}
				if _, err := update.Save(ctx); err != nil {
					return fmt.Errorf("complete node command: %w", err)
				}
			}
		case "failed":
			if record.Status != nodecommand.StatusFailed {
				update := record.Update().SetStatus(nodecommand.StatusFailed).SetCompletedAt(now).SetLastError(acknowledgement.Error)
				if acknowledgement.Result != nil {
					update.SetResult(acknowledgement.Result)
				}
				if _, err := update.Save(ctx); err != nil {
					return fmt.Errorf("fail node command: %w", err)
				}
			}
		default:
			return invalid("command acknowledgement status must be acknowledged, completed, or failed")
		}
		if projector != nil {
			if err := projector.ProjectCommandAcknowledgement(ctx, tx, nodeRecord, record, acknowledgement, now); err != nil {
				return fmt.Errorf("project command acknowledgement: %w", err)
			}
		}
	}
	return nil
}

func deliverCommands(ctx context.Context, tx *ent.Tx, nodeRecord *ent.SelfHostedNode, lastSequence int64, now time.Time) ([]nodeprotocol.Command, error) {
	records, err := tx.NodeCommand.Query().Where(
		nodecommand.NodeIDEQ(nodeRecord.ID),
		nodecommand.StatusIn(nodecommand.StatusPending, nodecommand.StatusDelivered),
		nodecommand.AvailableAtLTE(now), nodecommand.SequenceGT(lastSequence),
	).Order(ent.Asc(nodecommand.FieldSequence)).Limit(maxCommandsPerSync).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load node commands: %w", err)
	}
	commands := make([]nodeprotocol.Command, 0, len(records))
	for _, record := range records {
		if record.Status == nodecommand.StatusPending {
			if _, err := record.Update().SetStatus(nodecommand.StatusDelivered).SetDeliveredAt(now).Save(ctx); err != nil {
				return nil, fmt.Errorf("mark node command delivered: %w", err)
			}
		}
		commands = append(commands, nodeprotocol.Command{
			ID: record.PublicID.String(), Sequence: record.Sequence, Kind: record.Kind, Payload: cloneMap(record.Payload),
		})
	}
	return commands, nil
}
