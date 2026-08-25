package sshcloud

import (
	"context"
	"fmt"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/cloudsshassignment"
	"github.com/XR-Lee/Gemcp/ent/cloudsshnode"
	"github.com/XR-Lee/Gemcp/ent/cloudsshprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/google/uuid"
)

func (s *Service) Dispatch(ctx context.Context, tx *ent.Tx, experiment *ent.Experiment, now time.Time) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	profile, err := tx.ResourceProfile.Query().Where(
		resourceprofile.IDEQ(experiment.ResourceProfileID), resourceprofile.BackendEQ(resourceprofile.BackendSSHCloud),
		resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load Cloud SSH profile: %w", err)
	}
	_ = profile
	if experiment.ReservedCostMilli != 0 || len(experiment.SecretNames) != 0 {
		return false, fmt.Errorf("Cloud SSH experiment has an incompatible reservation or Secret specification")
	}
	if snapshotString(experiment.EnvironmentSnapshot, "backend") != Backend {
		return false, fmt.Errorf("Cloud SSH environment backend is invalid")
	}
	attemptNumber, err := tx.Attempt.Query().Where(attempt.ExperimentIDEQ(experiment.ID)).Count(ctx)
	if err != nil {
		return false, err
	}
	if attemptNumber >= s.config.MaxAttempts {
		return false, fmt.Errorf("maximum Cloud SSH infrastructure attempts exhausted")
	}
	node, err := s.availableNode(ctx, tx, experiment)
	if err != nil || node == nil {
		return false, err
	}
	attemptID := uuid.New()
	attemptRecord, err := tx.Attempt.Create().
		SetPublicID(attemptID).
		SetTenantID(experiment.TenantID).
		SetProjectID(experiment.ProjectID).
		SetExperimentID(experiment.ID).
		SetNumber(attemptNumber + 1).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("create Cloud SSH Attempt: %w", err)
	}
	assignmentID := uuid.New()
	outputRef := "experiments/" + experiment.PublicID.String() + "/attempts/" + attemptID.String() + "/outputs"
	remoteDir := "/var/tmp/gemcp/" + assignmentID.String()
	_, err = tx.CloudSSHAssignment.Create().
		SetPublicID(assignmentID).
		SetTenantID(experiment.TenantID).
		SetProjectID(experiment.ProjectID).
		SetExperimentID(experiment.ID).
		SetAttemptID(attemptRecord.ID).
		SetNodeID(node.ID).
		SetRemoteDir(remoteDir).
		SetOutputRef(outputRef).
		SetHardDeadlineAt(now.Add(s.config.ProvisionTimeout)).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("create Cloud SSH Assignment: %w", err)
	}
	resourceID := Backend + ":" + assignmentID.String()
	if _, err := tx.Attempt.UpdateOneID(attemptRecord.ID).SetProviderResourceID(resourceID).Save(ctx); err != nil {
		return false, err
	}
	if _, err := tx.Experiment.UpdateOneID(experiment.ID).
		SetState("provisioning").SetProviderResourceID(resourceID).SetProviderStatus("assignment_pending").
		Save(ctx); err != nil {
		return false, err
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
		SetAction("ssh_cloud.assignment_created").SetTargetType("cloud_ssh_assignment").SetTargetID(assignmentID.String()).
		SetMetadata(map[string]any{"experiment_id": experiment.PublicID.String(), "attempt_id": attemptID.String(), "node_id": node.PublicID.String()}).
		Save(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) availableNode(ctx context.Context, tx *ent.Tx, experiment *ent.Experiment) (*ent.CloudSSHNode, error) {
	nodes, err := tx.CloudSSHNode.Query().Where(
		cloudsshnode.TenantIDEQ(experiment.TenantID),
		cloudsshnode.StatusEQ(cloudsshnode.StatusActive),
		cloudsshnode.HasProjectAccessWith(
			cloudsshprojectaccess.ProjectIDEQ(experiment.ProjectID), cloudsshprojectaccess.StatusEQ(cloudsshprojectaccess.StatusActive),
		),
	).Order(ent.Asc(cloudsshnode.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load eligible Cloud SSH nodes: %w", err)
	}
	for _, node := range nodes {
		busy, err := tx.CloudSSHAssignment.Query().Where(
			cloudsshassignment.NodeIDEQ(node.ID),
			cloudsshassignment.StateIn(cloudsshassignment.StateStarting, cloudsshassignment.StateRunning, cloudsshassignment.StateStopping, cloudsshassignment.StateCollecting),
		).Exist(ctx)
		if err != nil {
			return nil, err
		}
		if busy {
			continue
		}
		return node, nil
	}
	return nil, nil
}

func snapshotString(snapshot map[string]any, key string) string {
	value, _ := snapshot[key].(string)
	return value
}
