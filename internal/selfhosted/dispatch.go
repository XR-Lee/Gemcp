package selfhosted

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/google/uuid"
)

var pinnedImagePattern = regexp.MustCompile(`^[^[:space:]@]+@sha256:[0-9a-f]{64}$`)

func (s *Service) Dispatch(ctx context.Context, tx *ent.Tx, experiment *ent.Experiment, now time.Time) (bool, error) {
	if !s.config.Enabled {
		return false, nil
	}
	profile, err := tx.ResourceProfile.Query().Where(
		resourceprofile.IDEQ(experiment.ResourceProfileID), resourceprofile.BackendEQ(resourceprofile.BackendSelfHosted),
		resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load Self-hosted profile: %w", err)
	}
	if experiment.ReservedCostMilli != 0 || len(experiment.SecretNames) != 0 {
		return false, fmt.Errorf("Self-hosted experiment has an incompatible reservation or Secret specification")
	}
	image := snapshotString(experiment.EnvironmentSnapshot, "image_uuid")
	if snapshotString(experiment.EnvironmentSnapshot, "backend") != Backend || !pinnedImagePattern.MatchString(image) {
		return false, fmt.Errorf("Self-hosted environment must use a digest-pinned public OCI image")
	}
	attemptNumber, err := tx.Attempt.Query().Where(attempt.ExperimentIDEQ(experiment.ID)).Count(ctx)
	if err != nil {
		return false, err
	}
	if attemptNumber >= s.config.MaxAttempts {
		return false, fmt.Errorf("maximum Self-hosted infrastructure attempts exhausted")
	}
	node, gpuUUID, err := s.availableNode(ctx, tx, experiment, profile, now, 0)
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
		return false, fmt.Errorf("create Self-hosted Attempt: %w", err)
	}
	assignmentID := uuid.New()
	outputRef := "experiments/" + experiment.PublicID.String() + "/attempts/" + attemptID.String() + "/outputs"
	assignment, err := tx.NodeAssignment.Create().
		SetPublicID(assignmentID).
		SetTenantID(experiment.TenantID).
		SetProjectID(experiment.ProjectID).
		SetExperimentID(experiment.ID).
		SetAttemptID(attemptRecord.ID).
		SetNodeID(node.ID).
		SetOutputRef(outputRef).
		SetHardDeadlineAt(now.Add(s.config.ProvisionTimeout)).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("create Node Assignment: %w", err)
	}
	executionCommand := experiment.Command
	argv := append([]string(nil), experiment.Argv...)
	if experiment.ExecutionMode == executioncmd.ModeArgv {
		executionCommand = ""
	} else {
		argv = nil
	}
	payload := map[string]any{
		"assignment_id":             assignmentID.String(),
		"experiment_id":             experiment.PublicID.String(),
		"attempt_id":                attemptID.String(),
		"image":                     image,
		"execution_mode":            experiment.ExecutionMode,
		"command":                   executionCommand,
		"argv":                      argv,
		"source_path":               "/api/v1/node-assignments/" + assignmentID.String() + "/source",
		"source_max_bytes":          s.config.SourceMaxBytes,
		"output_ref":                outputRef,
		"max_runtime_seconds":       experiment.MaxRuntimeSeconds,
		"timeout_extension_seconds": experiment.TimeoutExtensionSeconds,
		"termination_grace_seconds": experiment.TerminationGraceSeconds,
		"gpu_uuid":                  gpuUUID,
		"gpu_name":                  profile.GpuNames[0],
		"cpu_limit":                 profile.CPUTo,
		"memory_limit_bytes":        int64(profile.MemoryToGB) << 30,
	}
	commandRecord, err := enqueueCommand(ctx, tx, experiment.TenantID, node.ID, assignment.ID, "start_workload", "start:"+assignmentID.String(), payload, now)
	if err != nil {
		return false, err
	}
	if _, err := assignment.Update().SetStartCommandID(commandRecord.PublicID.String()).Save(ctx); err != nil {
		return false, err
	}
	resourceID := "self_hosted:" + assignmentID.String()
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
		SetAction("self_hosted.assignment_created").SetTargetType("node_assignment").SetTargetID(assignmentID.String()).
		SetMetadata(map[string]any{"experiment_id": experiment.PublicID.String(), "attempt_id": attemptID.String(), "node_id": node.PublicID.String()}).
		Save(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) availableNode(ctx context.Context, tx *ent.Tx, experiment *ent.Experiment, profile *ent.ResourceProfile, now time.Time, excludeNodeID int) (*ent.SelfHostedNode, string, error) {
	nodes, err := tx.SelfHostedNode.Query().Where(
		selfhostednode.TenantIDEQ(experiment.TenantID),
		selfhostednode.StatusEQ(selfhostednode.StatusActive),
		selfhostednode.ObservedStateEQ(selfhostednode.ObservedStateOnline),
		selfhostednode.LastSeenAtGT(now.Add(-s.config.NodeStaleAfter)),
		selfhostednode.HasProjectAccessWith(
			nodeprojectaccess.ProjectIDEQ(experiment.ProjectID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		),
	).Order(ent.Asc(selfhostednode.FieldID)).All(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("load eligible Self-hosted nodes: %w", err)
	}
	for _, node := range nodes {
		if node.ID == excludeNodeID {
			continue
		}
		busy, err := tx.NodeAssignment.Query().Where(
			nodeassignment.NodeIDEQ(node.ID),
			nodeassignment.StateIn(nodeassignment.StateStarting, nodeassignment.StateRunning, nodeassignment.StateStopping, nodeassignment.StateCollecting),
		).Exist(ctx)
		if err != nil {
			return nil, "", err
		}
		if busy {
			continue
		}
		if experiment.ExecutionMode == executioncmd.ModeArgv && !supportsExecutionMode(node.Capabilities, executioncmd.ModeArgv) {
			continue
		}
		gpuUUID, ok := matchingGPU(node.Capabilities, profile.GpuNames)
		if ok {
			return node, gpuUUID, nil
		}
	}
	return nil, "", nil
}

func supportsExecutionMode(capabilities map[string]any, mode string) bool {
	switch values := capabilities["execution_modes"].(type) {
	case []any:
		for _, value := range values {
			candidate, _ := value.(string)
			if candidate == mode {
				return true
			}
		}
	case []string:
		for _, candidate := range values {
			if candidate == mode {
				return true
			}
		}
	default:
		return mode == executioncmd.ModeShell
	}
	return false
}

func matchingGPU(capabilities map[string]any, accepted []string) (string, bool) {
	values, ok := capabilities["gpus"].([]any)
	if !ok || len(values) != 1 {
		return "", false
	}
	gpu, ok := values[0].(map[string]any)
	if !ok {
		return "", false
	}
	uuidValue, _ := gpu["uuid"].(string)
	name, _ := gpu["name"].(string)
	if uuidValue == "" || name == "" {
		return "", false
	}
	for _, candidate := range accepted {
		if strings.EqualFold(strings.TrimSpace(candidate), name) {
			return uuidValue, true
		}
	}
	return "", false
}

func snapshotString(snapshot map[string]any, key string) string {
	value, _ := snapshot[key].(string)
	return strings.TrimSpace(value)
}
