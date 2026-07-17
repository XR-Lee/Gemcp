package servicehealth

import (
	"context"
	"fmt"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/serviceheartbeat"
)

type Heartbeat struct {
	Role       string         `json:"role"`
	InstanceID string         `json:"instance_id"`
	Status     string         `json:"status"`
	LastSeenAt time.Time      `json:"last_seen_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func Beat(ctx context.Context, client *ent.Client, role serviceheartbeat.Role, instanceID string, metadata map[string]any) error {
	if client == nil || instanceID == "" {
		return fmt.Errorf("service heartbeat is not initialized")
	}
	if err := serviceheartbeat.RoleValidator(role); err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	now := time.Now().UTC()
	record, err := client.ServiceHeartbeat.Query().Where(
		serviceheartbeat.RoleEQ(role), serviceheartbeat.InstanceIDEQ(instanceID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		_, createErr := client.ServiceHeartbeat.Create().SetRole(role).SetInstanceID(instanceID).
			SetLastSeenAt(now).SetStatus(serviceheartbeat.StatusRunning).SetMetadata(metadata).Save(ctx)
		if ent.IsConstraintError(createErr) {
			_, createErr = client.ServiceHeartbeat.Update().Where(
				serviceheartbeat.RoleEQ(role), serviceheartbeat.InstanceIDEQ(instanceID),
			).SetLastSeenAt(now).SetStatus(serviceheartbeat.StatusRunning).SetMetadata(metadata).Save(ctx)
		}
		return createErr
	}
	if err != nil {
		return err
	}
	_, err = record.Update().SetLastSeenAt(now).SetStatus(serviceheartbeat.StatusRunning).SetMetadata(metadata).Save(ctx)
	return err
}

func Latest(ctx context.Context, client *ent.Client, role serviceheartbeat.Role) (*Heartbeat, error) {
	record, err := client.ServiceHeartbeat.Query().Where(serviceheartbeat.RoleEQ(role)).
		Order(ent.Desc(serviceheartbeat.FieldLastSeenAt)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Heartbeat{Role: string(record.Role), InstanceID: record.InstanceID, Status: string(record.Status), LastSeenAt: record.LastSeenAt, Metadata: record.Metadata}, nil
}
