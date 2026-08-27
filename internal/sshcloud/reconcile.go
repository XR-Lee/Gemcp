package sshcloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/cloudsshassignment"
	"github.com/XR-Lee/Gemcp/ent/cloudsshnode"
	"github.com/XR-Lee/Gemcp/internal/executioncmd"
)

func (s *Service) Reconcile(ctx context.Context, experimentID int, now time.Time) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	assignment, err := s.client.CloudSSHAssignment.Query().Where(cloudsshassignment.ExperimentIDEQ(experimentID)).
		Order(ent.Desc(cloudsshassignment.FieldCreatedAt), ent.Desc(cloudsshassignment.FieldID)).
		WithAttempt().WithExperiment().WithNode().First(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	experiment, err := assignment.Edges.ExperimentOrErr()
	if err != nil {
		return true, err
	}
	attempt, err := assignment.Edges.AttemptOrErr()
	if err != nil {
		return true, err
	}
	node, err := assignment.Edges.NodeOrErr()
	if err != nil {
		return true, err
	}
	if assignmentTerminal(assignment.State) || experiment.BudgetFinalizedAt != nil {
		return true, nil
	}
	if assignment.State == cloudsshassignment.StateStarting && assignment.ContainerID == nil {
		if assignment.HardDeadlineAt != nil && !now.Before(*assignment.HardDeadlineAt) {
			return true, s.failAttempt(ctx, assignment, attempt, experiment, "provision_timeout", "Cloud SSH workload did not start before the provisioning deadline", now)
		}
		if err := s.provision(ctx, assignment, experiment, node, now); err != nil {
			if assignment.HardDeadlineAt != nil && !now.Before(*assignment.HardDeadlineAt) {
				return true, s.failAttempt(ctx, assignment, attempt, experiment, "provision_timeout", err.Error(), now)
			}
			return true, s.recordStartError(ctx, assignment, err)
		}
		return true, nil
	}
	if assignment.State == cloudsshassignment.StateRunning && assignment.DeadlineAt != nil && !now.Before(*assignment.DeadlineAt) &&
		experiment.TimeoutExtendedAt == nil && experiment.TimeoutExtensionSeconds > 0 {
		deadline := assignment.DeadlineAt.Add(time.Duration(experiment.TimeoutExtensionSeconds) * time.Second)
		hardDeadline := deadline.Add(time.Duration(experiment.TerminationGraceSeconds)*time.Second + 30*time.Second)
		if _, err := assignment.Update().SetDeadlineAt(deadline).SetHardDeadlineAt(hardDeadline).Save(ctx); err != nil {
			return true, err
		}
		if _, err := s.client.Experiment.UpdateOneID(experiment.ID).SetDeadlineAt(deadline).SetTimeoutExtendedAt(now).Save(ctx); err != nil {
			return true, err
		}
		if _, err := s.client.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
			SetAction("experiment.timeout_extended").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
			SetMetadata(map[string]any{"deadline_at": deadline, "extension_seconds": experiment.TimeoutExtensionSeconds, "backend": Backend}).Save(ctx); err != nil {
			return true, err
		}
		return true, nil
	}
	if experiment.DesiredState == "cancelled" || assignment.StopRequestedAt != nil || shouldTimeout(assignment, experiment, now) {
		reason := stopReason(experiment, assignment, now)
		if err := s.stopAndCollect(ctx, assignment, attempt, experiment, node, reason, now); err != nil {
			return true, s.recordStartError(ctx, assignment, err)
		}
		return true, nil
	}
	if assignment.State == cloudsshassignment.StateRunning || assignment.State == cloudsshassignment.StateStarting {
		if err := s.observe(ctx, assignment, attempt, experiment, node, now); err != nil {
			return true, s.recordStartError(ctx, assignment, err)
		}
	}
	return true, nil
}

func (s *Service) provision(ctx context.Context, assignment *ent.CloudSSHAssignment, experiment *ent.Experiment, node *ent.CloudSSHNode, now time.Time) error {
	credential, err := DecryptCredential(s.box, node.CredentialCiphertext)
	if err != nil {
		return err
	}
	conn, fingerprint, err := s.openNode(ctx, Target{Host: node.SSHHost, Port: node.SSHPort, User: node.SSHUser}, credential, node.HostKeyFingerprint)
	if err == ErrHostKeyChanged {
		_, _ = node.Update().SetStatus(cloudsshnode.StatusHostKeyChanged).Save(ctx)
		return ErrHostKeyChanged
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	if fingerprint != "" && node.HostKeyFingerprint == "" {
		_, _ = node.Update().SetHostKeyFingerprint(fingerprint).Save(ctx)
	}
	spec := workloadSpec(assignment, experiment, node)
	startCtx, cancel := context.WithTimeout(ctx, s.config.ProvisionTimeout)
	defer cancel()
	pid, err := startHostProcess(startCtx, conn, spec)
	if err != nil {
		cleanupHostProcess(context.Background(), conn, spec, "")
		return err
	}
	deadline := now.Add(time.Duration(experiment.MaxRuntimeSeconds) * time.Second)
	hardDeadline := deadline.Add(time.Duration(experiment.TimeoutExtensionSeconds+experiment.TerminationGraceSeconds)*time.Second + 30*time.Second)
	if _, err := assignment.Update().SetState(cloudsshassignment.StateRunning).SetContainerID(pid).
		SetStartedAt(now).SetLastHeartbeatAt(now).SetDeadlineAt(deadline).SetHardDeadlineAt(hardDeadline).ClearLastError().Save(ctx); err != nil {
		return err
	}
	if _, err := s.client.Attempt.UpdateOneID(assignment.AttemptID).SetState("running").SetStartedAt(now).SetLastHeartbeatAt(now).Save(ctx); err != nil {
		return err
	}
	if _, err := s.client.Experiment.UpdateOneID(experiment.ID).SetState("running").SetStartedAt(now).SetDeadlineAt(deadline).
		SetProviderStatus("running").Save(ctx); err != nil {
		return err
	}
	_, err = s.client.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
		SetAction("ssh_cloud.workload_started").SetTargetType("cloud_ssh_assignment").SetTargetID(assignment.PublicID.String()).
		SetMetadata(map[string]any{"experiment_id": experiment.PublicID.String(), "pid": pid}).Save(ctx)
	return err
}

func (s *Service) observe(ctx context.Context, assignment *ent.CloudSSHAssignment, attempt *ent.Attempt, experiment *ent.Experiment, node *ent.CloudSSHNode, now time.Time) error {
	if assignment.ContainerID == nil {
		return nil
	}
	conn, err := s.dialNode(ctx, node)
	if err != nil {
		return err
	}
	defer conn.Close()
	spec := workloadSpec(assignment, experiment, node)
	state, err := inspectHostProcess(ctx, conn, spec, *assignment.ContainerID)
	if err != nil {
		return err
	}
	logTail, _ := logsHostProcess(ctx, conn, spec)
	metrics := metricsRemote(ctx, conn, assignment.RemoteDir)
	if state.Running {
		return s.projectLiveObservation(ctx, assignment, attempt, experiment, logTail, metrics, now)
	}
	return s.finalizeRemote(ctx, conn, assignment, attempt, experiment, node, state.ExitCode, "completed", logTail, metrics, now)
}

func (s *Service) stopAndCollect(ctx context.Context, assignment *ent.CloudSSHAssignment, attempt *ent.Attempt, experiment *ent.Experiment, node *ent.CloudSSHNode, reason string, now time.Time) error {
	conn, err := s.dialNode(ctx, node)
	if err != nil {
		return s.failAttempt(ctx, assignment, attempt, experiment, "ssh_unreachable", err.Error(), now)
	}
	defer conn.Close()
	spec := workloadSpec(assignment, experiment, node)
	pid := ""
	if assignment.ContainerID != nil {
		pid = *assignment.ContainerID
		if err := stopHostProcess(ctx, conn, spec, pid, experiment.TerminationGraceSeconds); err != nil {
			return err
		}
	}
	logTail := ""
	exitCode := 137
	if pid != "" {
		if state, inspectErr := inspectHostProcess(ctx, conn, spec, pid); inspectErr == nil {
			exitCode = state.ExitCode
		}
		logTail, _ = logsHostProcess(ctx, conn, spec)
	}
	metrics := metricsRemote(ctx, conn, assignment.RemoteDir)
	return s.finalizeRemote(ctx, conn, assignment, attempt, experiment, node, exitCode, reason, logTail, metrics, now)
}

func (s *Service) projectLiveObservation(ctx context.Context, assignment *ent.CloudSSHAssignment, attempt *ent.Attempt, experiment *ent.Experiment, logTail string, metrics map[string]any, now time.Time) error {
	logTail = truncateLog(logTail)
	metrics = mergeMetrics(assignment.Metrics, metrics)
	if _, err := assignment.Update().SetState(cloudsshassignment.StateRunning).SetLastHeartbeatAt(now).SetLogTail(logTail).SetMetrics(metrics).Save(ctx); err != nil {
		return err
	}
	if _, err := s.client.Attempt.UpdateOneID(attempt.ID).SetState("running").SetLastHeartbeatAt(now).SetLogTail(logTail).SetMetrics(metrics).Save(ctx); err != nil {
		return err
	}
	_, err := s.client.Experiment.UpdateOneID(experiment.ID).SetState("running").SetProviderStatus("running").SetLogTail(logTail).SetMetrics(metrics).Save(ctx)
	return err
}

func (s *Service) finalizeRemote(ctx context.Context, conn Conn, assignment *ent.CloudSSHAssignment, attempt *ent.Attempt, experiment *ent.Experiment, node *ent.CloudSSHNode, exitCode int, reason, logTail string, metrics map[string]any, now time.Time) error {
	spec := workloadSpec(assignment, experiment, node)
	pid := ""
	if assignment.ContainerID != nil {
		pid = *assignment.ContainerID
	}
	metrics = mergeMetrics(assignment.Metrics, metrics)
	logTail = truncateLog(logTail)
	cleanupHostProcess(ctx, conn, spec, pid)
	finalState, attemptState, failureCode, failureReason := sshCloudResult(experiment, exitCode, reason)
	assignmentState := cloudsshassignment.State(finalState)
	if finalState == "timed_out" {
		assignmentState = cloudsshassignment.StateTimedOut
	}
	assignmentUpdate := assignment.Update().SetState(assignmentState).SetFinishedAt(now).SetLastHeartbeatAt(now).
		SetExitCode(exitCode).SetLogTail(logTail).SetMetrics(metrics).ClearLastError()
	if failureCode != "" {
		assignmentUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := assignmentUpdate.Save(ctx); err != nil {
		return err
	}
	attemptUpdate := s.client.Attempt.UpdateOneID(attempt.ID).SetState(attemptState).SetFinishedAt(now).SetLastHeartbeatAt(now).
		SetExitCode(exitCode).SetLogTail(logTail).SetMetrics(metrics).SetEstimatedCostMilli(0)
	if failureCode != "" {
		attemptUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := attemptUpdate.Save(ctx); err != nil {
		return err
	}
	experimentUpdate := s.client.Experiment.UpdateOneID(experiment.ID).SetState(finalState).SetFinishedAt(now).SetExitCode(exitCode).
		SetLogTail(logTail).SetMetrics(metrics).SetEstimatedCostMilli(0).SetBudgetFinalizedAt(now).SetProviderStatus("finished").
		ClearLeaseOwner().ClearLeaseExpiresAt()
	if failureCode != "" {
		experimentUpdate.SetFailureCode(failureCode).SetFailureReason(failureReason)
	}
	if _, err := experimentUpdate.Save(ctx); err != nil {
		return err
	}
	if err := releaseReservation(ctx, s.client, experiment); err != nil {
		return err
	}
	_, err := s.client.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
		SetAction("experiment.finalized").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
		SetMetadata(map[string]any{"state": finalState, "attempt_id": attempt.PublicID.String(), "assignment_id": assignment.PublicID.String(), "exit_code": exitCode, "backend": Backend}).Save(ctx)
	return err
}

func (s *Service) failAttempt(ctx context.Context, assignment *ent.CloudSSHAssignment, attempt *ent.Attempt, experiment *ent.Experiment, code, reason string, now time.Time) error {
	if _, err := assignment.Update().SetState(cloudsshassignment.StateFailed).SetFinishedAt(now).SetFailureCode(code).SetFailureReason(reason).SetLastError(reason).Save(ctx); err != nil {
		return err
	}
	if experiment.DesiredState == "cancelled" {
		if _, err := s.client.Attempt.UpdateOneID(attempt.ID).SetState("cancelled").SetFinishedAt(now).SetFailureCode("cancelled").SetFailureReason("experiment was cancelled").Save(ctx); err != nil {
			return err
		}
		if _, err := s.client.Experiment.UpdateOneID(experiment.ID).SetState("cancelled").SetFinishedAt(now).SetBudgetFinalizedAt(now).
			SetFailureCode("cancelled").SetFailureReason("experiment was cancelled").ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx); err != nil {
			return err
		}
		return releaseReservation(ctx, s.client, experiment)
	}
	if _, err := s.client.Attempt.UpdateOneID(attempt.ID).SetState("failed").SetFinishedAt(now).SetRetryReason(code).
		SetFailureCode(code).SetFailureReason(reason).Save(ctx); err != nil {
		return err
	}
	if attempt.Number < s.config.MaxAttempts {
		backoff := time.Duration(attempt.Number*30) * time.Second
		if _, err := s.client.Experiment.UpdateOneID(experiment.ID).SetState("queued").SetNextAttemptAt(now.Add(backoff)).
			ClearProviderResourceID().ClearProviderStatus().ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx); err != nil {
			return err
		}
		_, err := s.client.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
			SetAction("experiment.retry_scheduled").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
			SetMetadata(map[string]any{"attempt_id": attempt.PublicID.String(), "attempt_number": attempt.Number, "retry_at": now.Add(backoff), "reason": code, "backend": Backend}).Save(ctx)
		return err
	}
	if _, err := s.client.Experiment.UpdateOneID(experiment.ID).SetState("provider_error").SetFinishedAt(now).SetBudgetFinalizedAt(now).
		SetFailureCode(code).SetFailureReason(reason).ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx); err != nil {
		return err
	}
	if err := releaseReservation(ctx, s.client, experiment); err != nil {
		return err
	}
	_, err := s.client.AuditEvent.Create().SetTenantID(experiment.TenantID).SetActorType("system").SetActorID(s.config.InstanceID).
		SetAction("experiment.finalized").SetTargetType("experiment").SetTargetID(experiment.PublicID.String()).
		SetMetadata(map[string]any{"state": "provider_error", "attempt_id": attempt.PublicID.String(), "failure_code": code, "backend": Backend}).Save(ctx)
	return err
}

func (s *Service) recordStartError(ctx context.Context, assignment *ent.CloudSSHAssignment, cause error) error {
	_, err := assignment.Update().SetLastError(strings.TrimSpace(cause.Error())).Save(ctx)
	return err
}

func (s *Service) dialNode(ctx context.Context, node *ent.CloudSSHNode) (Conn, error) {
	credential, err := DecryptCredential(s.box, node.CredentialCiphertext)
	if err != nil {
		return nil, err
	}
	conn, _, err := s.openNode(ctx, Target{Host: node.SSHHost, Port: node.SSHPort, User: node.SSHUser}, credential, node.HostKeyFingerprint)
	if err == ErrHostKeyChanged {
		_, _ = node.Update().SetStatus(cloudsshnode.StatusHostKeyChanged).Save(ctx)
	}
	return conn, err
}

func workloadSpec(assignment *ent.CloudSSHAssignment, experiment *ent.Experiment, node *ent.CloudSSHNode) remoteWorkload {
	image := snapshotString(experiment.EnvironmentSnapshot, "image_uuid")
	cpuLimit := snapshotInt(experiment.ResourceSnapshot, "cpu_to")
	if cpuLimit <= 0 {
		cpuLimit = defaultCPULimit
	}
	memoryGB := snapshotInt(experiment.ResourceSnapshot, "memory_to_gb")
	if memoryGB <= 0 {
		memoryGB = defaultMemoryGB
	}
	command := experiment.Command
	argv := append([]string(nil), experiment.Argv...)
	if experiment.ExecutionMode == executioncmd.ModeArgv {
		command = ""
	} else {
		argv = nil
	}
	return remoteWorkload{
		AssignmentID: assignment.PublicID.String(), Image: image, ExecutionMode: string(experiment.ExecutionMode),
		Command: command, Argv: argv, WorkingDir: snapshotString(experiment.EnvironmentSnapshot, "working_directory"),
		GPUUUID: inventoryGPUUUID(node.Inventory), CPULimit: cpuLimit,
		MemoryLimitBytes: int64(memoryGB) << 30, RemoteDir: assignment.RemoteDir, GraceSeconds: experiment.TerminationGraceSeconds,
		Network: inventoryDockerNetwork(node.Inventory), DatasetEnv: snapshotDatasetEnv(experiment.EnvironmentSnapshot),
	}
}

func shouldTimeout(assignment *ent.CloudSSHAssignment, experiment *ent.Experiment, now time.Time) bool {
	if assignment.State != cloudsshassignment.StateRunning || assignment.DeadlineAt == nil {
		return false
	}
	if now.Before(*assignment.DeadlineAt) {
		return false
	}
	return experiment.TimeoutExtendedAt != nil || experiment.TimeoutExtensionSeconds == 0
}

func stopReason(experiment *ent.Experiment, assignment *ent.CloudSSHAssignment, now time.Time) string {
	if experiment.FailureCode != nil && *experiment.FailureCode == "emergency_stop" {
		return "emergency"
	}
	if experiment.DesiredState == "cancelled" || (assignment.StopReason != nil && *assignment.StopReason == "emergency") {
		if assignment.StopReason != nil && *assignment.StopReason == "emergency" {
			return "emergency"
		}
		return "cancelled"
	}
	if assignment.DeadlineAt != nil && !now.Before(*assignment.DeadlineAt) {
		return "timeout"
	}
	if assignment.StopReason != nil {
		return *assignment.StopReason
	}
	return "cancelled"
}

func sshCloudResult(experiment *ent.Experiment, exitCode int, reason string) (string, string, string, string) {
	if experiment.DesiredState == "cancelled" || reason == "cancelled" || reason == "emergency" {
		code := "cancelled"
		message := "experiment was cancelled by an authorized request"
		if reason == "emergency" || (experiment.FailureCode != nil && *experiment.FailureCode == "emergency_stop") {
			code, message = "emergency_stop", "experiment was stopped by the Owner emergency action"
		}
		return "cancelled", "cancelled", code, message
	}
	switch reason {
	case "timeout":
		return "timed_out", "failed", "runtime_timeout", "experiment exceeded its extended runtime deadline"
	case "oom":
		return "failed", "failed", "oom", "the workload was terminated after exhausting GPU or host memory"
	}
	if exitCode == 0 {
		return "succeeded", "succeeded", "", ""
	}
	return "failed", "failed", "command_failed", fmt.Sprintf("experiment command exited with code %d", exitCode)
}

func assignmentTerminal(state cloudsshassignment.State) bool {
	switch state {
	case cloudsshassignment.StateSucceeded, cloudsshassignment.StateFailed, cloudsshassignment.StateCancelled, cloudsshassignment.StateTimedOut, cloudsshassignment.StateLost:
		return true
	default:
		return false
	}
}

func releaseReservation(ctx context.Context, client *ent.Client, experiment *ent.Experiment) error {
	if experiment.ReservedCostMilli == 0 {
		return nil
	}
	period := ""
	reservation, err := client.BudgetEntry.Query().Where(budgetentry.ExperimentIDEQ(experiment.ID), budgetentry.KindEQ("reservation")).
		Order(ent.Desc(budgetentry.FieldID)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if err == nil {
		period = reservation.Period
	}
	_, err = client.BudgetEntry.Create().SetTenantID(experiment.TenantID).SetProjectID(experiment.ProjectID).SetExperimentID(experiment.ID).
		SetPeriod(period).SetKind("release").SetAmountMilli(-experiment.ReservedCostMilli).
		SetDescription("Cloud SSH reservation release").Save(ctx)
	return err
}

func truncateLog(value string) string {
	if len(value) <= maxLogTailBytes {
		return value
	}
	return value[len(value)-maxLogTailBytes:]
}

func snapshotInt(snapshot map[string]any, key string) int {
	return anyInt(snapshot[key])
}
