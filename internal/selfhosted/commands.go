package selfhosted

import (
	"context"
	"fmt"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/nodecommand"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
)

func enqueueCommand(ctx context.Context, tx *ent.Tx, tenantID, nodeID, assignmentID int, kind, key string, payload map[string]any, now time.Time) (*ent.NodeCommand, error) {
	existing, err := tx.NodeCommand.Query().Where(
		nodecommand.NodeIDEQ(nodeID), nodecommand.IdempotencyKeyEQ(key),
	).Only(ctx)
	if err == nil {
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("load idempotent node command: %w", err)
	}
	latest, err := tx.NodeCommand.Query().Where(nodecommand.NodeIDEQ(nodeID)).Order(ent.Desc(nodecommand.FieldSequence)).First(ctx)
	sequence := int64(1)
	if err == nil {
		sequence = latest.Sequence + 1
	} else if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("load node command sequence: %w", err)
	}
	record, err := tx.NodeCommand.Create().
		SetTenantID(tenantID).
		SetNodeID(nodeID).
		SetAssignmentID(assignmentID).
		SetSequence(sequence).
		SetKind(kind).
		SetIdempotencyKey(key).
		SetPayload(payload).
		SetAvailableAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create node command: %w", err)
	}
	return record, nil
}

func (s *Service) ProjectCommandAcknowledgement(ctx context.Context, tx *ent.Tx, node *ent.SelfHostedNode, command *ent.NodeCommand, acknowledgement nodeprotocol.CommandAcknowledgement, now time.Time) error {
	if command.AssignmentID == nil {
		return nil
	}
	assignment, err := tx.NodeAssignment.Get(ctx, *command.AssignmentID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if assignment.NodeID != node.ID {
		return ErrInvalidEvent
	}
	if acknowledgement.Status == "failed" {
		_, err = assignment.Update().SetLastError(acknowledgement.Error).Save(ctx)
		return err
	}
	if command.Kind == "start_workload" && acknowledgement.Status == "completed" {
		if workloadID := payloadString(acknowledgement.Result, "workload_id"); workloadID != "" {
			_, err = assignment.Update().SetWorkloadID(workloadID).Save(ctx)
			return err
		}
	}
	return nil
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
