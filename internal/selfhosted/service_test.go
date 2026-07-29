package selfhosted

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodecommand"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type serviceFixture struct {
	client     *ent.Client
	service    *Service
	node       *ent.SelfHostedNode
	experiment *ent.Experiment
	now        time.Time
}

func newServiceFixture(t *testing.T) serviceFixture {
	return newServiceFixtureWithArgv(t, nil)
}

func newServiceFixtureWithArgv(t *testing.T, argv []string) serviceFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	tenant, _ := client.Tenant.Create().SetName("tenant").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("project").SetSlug("project").
		SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).SetMaxRuntimeSeconds(3600).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(5).Save(ctx)
	repository, _ := client.Repository.Create().SetProjectID(project.ID).SetName("repository").SetSSHURL("git@github.com:o/r.git").
		SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetBackend("self_hosted").SetName("environment").
		SetImageUUID("registry.example/train@sha256:" + strings.Repeat("a", 64)).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetBackend("self_hosted").SetName("profile").SetRegion("self_hosted").
		SetGpuNames([]string{"NVIDIA GeForce RTX 3090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).
		SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
	agent, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("agent").SetPrefix("gmc_test").SetTokenHash([]byte("agent-hash")).Save(ctx)
	node, _ := client.SelfHostedNode.Create().SetTenantID(tenant.ID).SetLabel("gpu-node").SetTokenPrefix("gmn_test").SetTokenHash([]byte("node-hash")).
		SetStatus("active").SetObservedState("online").SetInstallationID(uuid.NewString()).SetMachineFingerprint(strings.Repeat("b", 64)).
		SetHostname("gpu-node").SetOperatingSystem("linux").SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion(nodeprotocol.Version).
		SetCapabilities(map[string]any{"gpus": []any{map[string]any{"uuid": "GPU-test", "name": "NVIDIA GeForce RTX 3090", "memory_bytes": float64(24 << 30)}}, "execution_modes": []any{"shell", "argv"}}).
		SetStorage(map[string]any{"available_bytes": float64(1 << 40)}).SetLastSeenAt(now).Save(ctx)
	_, _ = client.NodeProjectAccess.Create().SetTenantID(tenant.ID).SetNodeID(node.ID).SetProjectID(project.ID).Save(ctx)
	experimentID := uuid.New()
	experimentCreate := client.Experiment.Create().SetPublicID(experimentID).SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(agent.ID).
		SetRepositoryID(repository.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha(strings.Repeat("0", 40)).
		SetCommand("python train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(5).
		SetRepositorySnapshot(map[string]any{"id": repository.PublicID.String(), "project_id": project.PublicID.String()}).
		SetEnvironmentSnapshot(map[string]any{"id": environment.PublicID.String(), "backend": Backend, "image_uuid": environment.ImageUUID}).
		SetResourceSnapshot(map[string]any{"id": profile.PublicID.String(), "backend": Backend}).SetOutputPath("managed://experiments/" + experimentID.String() + "/outputs").
		SetReservedCostMilli(0)
	if len(argv) > 0 {
		experimentCreate.SetExecutionMode("argv").SetArgv(argv)
	}
	experiment, _ := experimentCreate.Save(ctx)
	_, _ = client.BudgetEntry.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetExperimentID(experiment.ID).
		SetPeriod("2026-07").SetKind("reservation").SetAmountMilli(0).SetDescription("unmetered").Save(ctx)
	config := DefaultConfig()
	config.Enabled = true
	config.InstanceID = "scheduler-test"
	service, err := NewService(client, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	return serviceFixture{client: client, service: service, node: node, experiment: experiment, now: now}
}

func (f serviceFixture) dispatch(t *testing.T) *ent.NodeAssignment {
	t.Helper()
	ctx := context.Background()
	tx, err := f.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := tx.Experiment.Get(ctx, f.experiment.ID)
	dispatched, err := f.service.Dispatch(ctx, tx, record, f.now)
	if err != nil || !dispatched {
		_ = tx.Rollback()
		t.Fatalf("Dispatch() dispatched=%t err=%v", dispatched, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.client.NodeAssignment.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return assignment
}

func TestArgvAssignmentCarriesNoShellCommand(t *testing.T) {
	f := newServiceFixtureWithArgv(t, []string{"python", "train.py", "--seed", "2"})
	assignment := f.dispatch(t)
	command, err := f.client.NodeCommand.Query().Where(nodecommand.AssignmentIDEQ(assignment.ID), nodecommand.KindEQ("start_workload")).Only(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if command.Payload["execution_mode"] != "argv" || command.Payload["command"] != "" {
		t.Fatalf("argv command payload = %+v", command.Payload)
	}
	values, ok := command.Payload["argv"].([]any)
	if !ok || len(values) != 4 || values[0] != "python" || values[3] != "2" {
		t.Fatalf("argv payload = %#v", command.Payload["argv"])
	}
}

func TestAssignmentLifecycleIsUnmeteredAndTokenless(t *testing.T) {
	f := newServiceFixture(t)
	assignment := f.dispatch(t)
	ctx := context.Background()
	command, _ := f.client.NodeCommand.Query().Where(nodecommand.AssignmentIDEQ(assignment.ID), nodecommand.KindEQ("start_workload")).Only(ctx)
	attempt, _ := f.client.Attempt.Query().Only(ctx)
	if len(attempt.RunnerTokenHash) != 0 || attempt.RunnerTokenCiphertext != "" || strings.Contains(strings.ToLower(fmt.Sprint(command.Payload)), "token") {
		t.Fatalf("Self-hosted command or Attempt contains Runner credentials: attempt=%+v payload=%v", attempt, command.Payload)
	}
	if count, _ := f.client.ProviderResource.Query().Count(ctx); count != 0 {
		t.Fatalf("Self-hosted dispatch created %d Provider resources", count)
	}

	tx, _ := f.client.Tx(ctx)
	node, _ := tx.SelfHostedNode.Get(ctx, f.node.ID)
	started := nodeprotocol.Event{ID: uuid.NewString(), Sequence: 1, Kind: "workload_started", OccurredAt: f.now, Payload: map[string]any{
		"assignment_id": assignment.PublicID.String(), "workload_id": "container-id",
	}}
	if err := f.service.ProjectNodeEvent(ctx, tx, node, started, f.now.Add(time.Second)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	experiment, _ := f.client.Experiment.Get(ctx, f.experiment.ID)
	if experiment.State != "running" || experiment.DeadlineAt == nil {
		t.Fatalf("started experiment=%+v", experiment)
	}

	tx, _ = f.client.Tx(ctx)
	node, _ = tx.SelfHostedNode.Get(ctx, f.node.ID)
	finished := nodeprotocol.Event{ID: uuid.NewString(), Sequence: 2, Kind: "workload_finished", OccurredAt: f.now, Payload: map[string]any{
		"assignment_id": assignment.PublicID.String(), "exit_code": float64(0), "reason": "completed", "log_tail": "done\n",
		"metrics": map[string]any{"accuracy": 0.9},
	}}
	if err := f.service.ProjectNodeEvent(ctx, tx, node, finished, f.now.Add(2*time.Second)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	experiment, _ = f.client.Experiment.Get(ctx, f.experiment.ID)
	attempt, _ = f.client.Attempt.Query().Only(ctx)
	assignment, _ = f.client.NodeAssignment.Query().Only(ctx)
	if experiment.State != "succeeded" || experiment.EstimatedCostMilli != 0 || attempt.State != "succeeded" || assignment.State != nodeassignment.StateSucceeded {
		t.Fatalf("final experiment=%+v attempt=%+v assignment=%+v", experiment, attempt, assignment)
	}
	tx, _ = f.client.Tx(ctx)
	node, _ = tx.SelfHostedNode.Get(ctx, f.node.ID)
	cleanup := nodeprotocol.Event{ID: uuid.NewString(), Sequence: 3, Kind: "workload_cleanup_complete", OccurredAt: f.now, Payload: map[string]any{
		"assignment_id": assignment.PublicID.String(), "workload_id": "container-id",
	}}
	if err := f.service.ProjectNodeEvent(ctx, tx, node, cleanup, f.now.Add(3*time.Second)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if exists, _ := f.client.AuditEvent.Query().Where(auditevent.ActionEQ("experiment.cleanup_complete")).Exist(ctx); !exists {
		t.Fatal("cleanup evidence was not recorded")
	}
	if entries, _ := f.client.BudgetEntry.Query().All(ctx); len(entries) != 2 || entries[1].AmountMilli != 0 || entries[1].Kind != "release" {
		t.Fatalf("budget entries=%+v", entries)
	}
}

func TestAssignmentProjectsRuntimeObservationAndLiveOutput(t *testing.T) {
	f := newServiceFixture(t)
	assignment := f.dispatch(t)
	ctx := context.Background()

	tx, _ := f.client.Tx(ctx)
	node, _ := tx.SelfHostedNode.Get(ctx, f.node.ID)
	started := nodeprotocol.Event{ID: uuid.NewString(), Sequence: 1, Kind: "workload_started", OccurredAt: f.now, Payload: map[string]any{
		"assignment_id": assignment.PublicID.String(), "workload_id": "container-id",
		"runtime_info": map[string]any{
			"source": "node_binding", "working_directory": "/workspace", "output_directory": "/outputs",
			"cuda_visible_devices": "GPU-test", "gpu_devices": []any{map[string]any{"index": float64(0), "uuid": "GPU-test", "name": "NVIDIA GeForce RTX 3090"}},
		},
	}}
	if err := f.service.ProjectNodeEvent(ctx, tx, node, started, f.now.Add(time.Second)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	tx, _ = f.client.Tx(ctx)
	node, _ = tx.SelfHostedNode.Get(ctx, f.node.ID)
	heartbeat := nodeprotocol.Event{ID: uuid.NewString(), Sequence: 2, Kind: "workload_heartbeat", OccurredAt: f.now, Payload: map[string]any{
		"assignment_id": assignment.PublicID.String(), "log_tail": "epoch 4 loss=0.38\n", "metrics": map[string]any{"epoch": float64(4), "loss": 0.38},
	}}
	if err := f.service.ProjectNodeEvent(ctx, tx, node, heartbeat, f.now.Add(16*time.Second)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	experimentRecord, _ := f.client.Experiment.Get(ctx, f.experiment.ID)
	attemptRecord, _ := f.client.Attempt.Query().Only(ctx)
	assignmentRecord, _ := f.client.NodeAssignment.Query().Only(ctx)
	if experimentRecord.LogTail == nil || *experimentRecord.LogTail != "epoch 4 loss=0.38\n" || experimentRecord.Metrics["loss"] != 0.38 ||
		attemptRecord.LastHeartbeatAt == nil || assignmentRecord.LastHeartbeatAt == nil || assignmentRecord.LogTail == nil {
		t.Fatalf("live projection experiment=%+v attempt=%+v assignment=%+v", experimentRecord, attemptRecord, assignmentRecord)
	}
	startAudit, err := f.client.AuditEvent.Query().Where(
		auditevent.ActionEQ("experiment.started"), auditevent.TargetIDEQ(f.experiment.PublicID.String()),
	).Only(ctx)
	if err != nil || startAudit.Metadata["runtime_info"] == nil {
		t.Fatalf("start audit=%+v err=%v", startAudit, err)
	}
}

func TestReconcileCancellationCreatesDurableStopCommand(t *testing.T) {
	f := newServiceFixture(t)
	assignment := f.dispatch(t)
	ctx := context.Background()
	_, _ = f.client.Experiment.UpdateOneID(f.experiment.ID).SetDesiredState("cancelled").SetState("cancelling").Save(ctx)
	handled, err := f.service.Reconcile(ctx, f.experiment.ID, f.now.Add(time.Second))
	if err != nil || !handled {
		t.Fatalf("Reconcile() handled=%t err=%v", handled, err)
	}
	stop, err := f.client.NodeCommand.Query().Where(nodecommand.AssignmentIDEQ(assignment.ID), nodecommand.KindEQ("stop_workload")).Only(ctx)
	if err != nil || stop.Payload["reason"] != "cancelled" {
		t.Fatalf("stop command=%+v err=%v", stop, err)
	}
	assignment, _ = f.client.NodeAssignment.Get(ctx, assignment.ID)
	if assignment.State != nodeassignment.StateStopping || assignment.StopRequestedAt == nil {
		t.Fatalf("stopping assignment=%+v", assignment)
	}
}

func TestRuntimeConfigCreatesPinnedUnmeteredPair(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	project, _ := f.client.Project.Query().Only(ctx)
	result, err := f.service.CreateRuntimeConfig(ctx, project.TenantID, "owner-1", project.PublicID.String(), RuntimeConfigInput{
		Name: "training", Image: "registry.example/training@sha256:" + strings.Repeat("c", 64),
		GPUNames: []string{"RTX 4090", "RTX 3090"}, CPULimit: 16, MemoryGB: 64, MakeDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Environment.IsDefault || !result.Profile.IsDefault || result.Profile.CPULimit != 16 || result.Profile.MemoryGB != 64 {
		t.Fatalf("runtime config=%+v", result)
	}
	profile, err := f.client.ResourceProfile.Query().Where(resourceprofile.PublicIDEQ(uuid.MustParse(result.Profile.ID))).Only(ctx)
	if err != nil || profile.PriceToMilli != 0 || profile.Backend != resourceprofile.BackendSelfHosted {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
	if _, err := f.service.CreateRuntimeConfig(ctx, project.TenantID, "owner-1", project.PublicID.String(), RuntimeConfigInput{
		Name: "tagged", Image: "registry.example/training:latest", GPUNames: []string{"RTX 3090"}, CPULimit: 8, MemoryGB: 32,
	}); err == nil {
		t.Fatal("runtime config accepted a mutable image tag")
	}
}

func TestTrustedWorkspaceAutomaticallyCreatesRuntimeFromNodeInventory(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	project, _ := f.client.Project.Query().Only(ctx)
	capabilities := map[string]any{
		"cpu_count": float64(24), "memory_bytes": float64(int64(31) << 30), "execution_modes": []any{"shell", "argv"}, "workspace_modes": []any{"trusted_rw"},
		"gpus": []any{map[string]any{"uuid": "GPU-test", "name": "NVIDIA RTX A4000", "memory_bytes": float64(int64(16) << 30)}},
	}
	if _, err := f.node.Update().SetCapabilities(capabilities).Save(ctx); err != nil {
		t.Fatal(err)
	}
	input := TrustedWorkspaceInput{NodeID: f.node.PublicID.String(), WorkspacePath: "/home/campus.ncl.ac.uk/nxl51/gemcp_tmp"}
	workspace, err := f.service.EnableTrustedWorkspace(ctx, project.TenantID, "owner-1", project.PublicID.String(), input)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.WorkspacePath != input.WorkspacePath || workspace.GPUName != "NVIDIA RTX A4000" || workspace.CPULimit != 22 || workspace.MemoryGB != 29 || !workspace.NodeReady {
		t.Fatalf("workspace=%+v", workspace)
	}
	environmentRecord, err := f.client.Environment.Query().Where(environment.PublicIDEQ(uuid.MustParse(workspace.EnvironmentID))).Only(ctx)
	if err != nil || environmentRecord.ImageUUID != workspaceImagePlaceholder || environmentRecord.RecipeRef != trustedWorkspaceRecipePrefix+f.node.PublicID.String() {
		t.Fatalf("environment=%+v err=%v", environmentRecord, err)
	}
	access, err := f.client.NodeProjectAccess.Query().Where(nodeprojectaccess.NodeIDEQ(f.node.ID), nodeprojectaccess.ProjectIDEQ(project.ID)).Only(ctx)
	if err != nil || access.ExecutionPolicy != nodeprojectaccess.ExecutionPolicyTrustedWorkspace || access.WorkspacePath == nil || *access.WorkspacePath != input.WorkspacePath {
		t.Fatalf("access=%+v err=%v", access, err)
	}
	listed, err := f.service.ListRuntimeConfigs(ctx, project.TenantID, project.PublicID.String())
	if err != nil || len(listed.Workspaces) != 1 || listed.Workspaces[0].EnvironmentID != workspace.EnvironmentID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	second, err := f.service.EnableTrustedWorkspace(ctx, project.TenantID, "owner-1", project.PublicID.String(), input)
	if err != nil || second.EnvironmentID != workspace.EnvironmentID {
		t.Fatalf("idempotent enable=%+v err=%v", second, err)
	}
	if count, _ := f.client.Environment.Query().Where(environment.RecipeRefEQ(trustedWorkspaceRecipePrefix + f.node.PublicID.String())).Count(ctx); count != 1 {
		t.Fatalf("workspace environment count=%d", count)
	}
	if _, err := f.service.EnableTrustedWorkspace(ctx, project.TenantID, "owner-1", project.PublicID.String(), TrustedWorkspaceInput{NodeID: f.node.PublicID.String(), WorkspacePath: "/etc"}); err == nil {
		t.Fatal("protected host root was accepted")
	}
	if _, err := f.service.EnableTrustedWorkspace(ctx, project.TenantID, "owner-1", project.PublicID.String(), TrustedWorkspaceInput{NodeID: f.node.PublicID.String(), WorkspacePath: "/etc/gemcp"}); err == nil {
		t.Fatal("protected host subtree was accepted")
	}
	disabled, err := f.service.DisableTrustedWorkspace(ctx, project.TenantID, "owner-1", project.PublicID.String(), f.node.PublicID.String())
	if err != nil || !disabled.Disabled {
		t.Fatalf("DisableTrustedWorkspace()=%+v err=%v", disabled, err)
	}
	access, _ = f.client.NodeProjectAccess.Get(ctx, access.ID)
	environmentRecord, _ = f.client.Environment.Get(ctx, environmentRecord.ID)
	if access.ExecutionPolicy != nodeprojectaccess.ExecutionPolicyStrict || access.WorkspacePath != nil || environmentRecord.Status != environment.StatusDisabled {
		t.Fatalf("disabled access=%+v environment=%+v", access, environmentRecord)
	}
}

func TestTrustedWorkspaceDispatchBindsApprovedNodePathAndTag(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	project, _ := f.client.Project.Query().Only(ctx)
	capabilities := map[string]any{
		"cpu_count": float64(24), "memory_bytes": float64(int64(31) << 30), "execution_modes": []any{"shell", "argv"}, "workspace_modes": []any{"trusted_rw"},
		"gpus": []any{map[string]any{"uuid": "GPU-test", "name": "NVIDIA RTX A4000", "memory_bytes": float64(int64(16) << 30)}},
	}
	if _, err := f.node.Update().SetCapabilities(capabilities).Save(ctx); err != nil {
		t.Fatal(err)
	}
	workspacePath := "/home/campus.ncl.ac.uk/nxl51/gemcp_tmp"
	workspace, err := f.service.EnableTrustedWorkspace(ctx, project.TenantID, "owner-1", project.PublicID.String(), TrustedWorkspaceInput{
		NodeID: f.node.PublicID.String(), WorkspacePath: workspacePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	environmentRecord, _ := f.client.Environment.Query().Where(environment.PublicIDEQ(uuid.MustParse(workspace.EnvironmentID))).Only(ctx)
	profileRecord, _ := f.client.ResourceProfile.Query().Where(resourceprofile.PublicIDEQ(uuid.MustParse(workspace.ResourceProfileID))).Only(ctx)
	base := f.experiment
	experimentID := uuid.New()
	experimentRecord, err := f.client.Experiment.Create().SetPublicID(experimentID).SetTenantID(base.TenantID).SetProjectID(base.ProjectID).
		SetAgentTokenID(*base.AgentTokenID).SetRepositoryID(base.RepositoryID).SetEnvironmentID(environmentRecord.ID).SetResourceProfileID(profileRecord.ID).
		SetCommitSha(strings.Repeat("1", 40)).SetExecutionMode("argv").SetArgv([]string{"python", "train.py"}).SetCommand("python train.py").
		SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(5).
		SetRepositorySnapshot(base.RepositorySnapshot).SetEnvironmentSnapshot(map[string]any{
		"id": environmentRecord.PublicID.String(), "backend": Backend, "image_uuid": "pytorch/pytorch:2.4.1-cuda12.1-cudnn9-runtime",
		"execution_policy": "trusted_workspace", "workspace_path": workspacePath, "workspace_node_id": f.node.PublicID.String(), "workspace_node_label": f.node.Label,
	}).SetResourceSnapshot(map[string]any{"id": profileRecord.PublicID.String(), "backend": Backend}).SetOutputPath("managed://experiments/" + experimentID.String() + "/outputs").SetReservedCostMilli(0).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.BudgetEntry.Create().SetTenantID(base.TenantID).SetProjectID(base.ProjectID).SetExperimentID(experimentRecord.ID).
		SetPeriod("2026-07").SetKind("reservation").SetAmountMilli(0).SetDescription("unmetered workspace").Save(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := f.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	transactionExperiment, _ := tx.Experiment.Get(ctx, experimentRecord.ID)
	dispatched, err := f.service.Dispatch(ctx, tx, transactionExperiment, f.now)
	if err != nil || !dispatched {
		_ = tx.Rollback()
		t.Fatalf("Dispatch()=%t err=%v", dispatched, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	command, err := f.client.NodeCommand.Query().Where(nodecommand.KindEQ("start_workload")).Only(ctx)
	if err != nil || command.Payload["workspace_mode"] != "trusted_rw" || command.Payload["workspace_path"] != workspacePath || command.Payload["image"] != "pytorch/pytorch:2.4.1-cuda12.1-cudnn9-runtime" {
		t.Fatalf("workspace command=%+v err=%v", command, err)
	}
	if _, err := f.service.DisableTrustedWorkspace(ctx, project.TenantID, "owner-1", project.PublicID.String(), f.node.PublicID.String()); err == nil {
		t.Fatal("trusted workspace was disabled during an active Assignment")
	}
	assignment, _ := f.client.NodeAssignment.Query().Only(ctx)
	tx, _ = f.client.Tx(ctx)
	node, _ := tx.SelfHostedNode.Get(ctx, f.node.ID)
	if err := f.service.ProjectNodeEvent(ctx, tx, node, nodeprotocol.Event{ID: uuid.NewString(), Sequence: 1, Kind: "workload_started", Payload: map[string]any{
		"assignment_id": assignment.PublicID.String(), "workload_id": "workspace-container",
	}}, f.now.Add(time.Second)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	resolvedImage := "pytorch/pytorch@sha256:" + strings.Repeat("e", 64)
	tx, _ = f.client.Tx(ctx)
	node, _ = tx.SelfHostedNode.Get(ctx, f.node.ID)
	if err := f.service.ProjectNodeEvent(ctx, tx, node, nodeprotocol.Event{ID: uuid.NewString(), Sequence: 2, Kind: "workload_finished", Payload: map[string]any{
		"assignment_id": assignment.PublicID.String(), "exit_code": float64(0), "reason": "completed", "log_tail": "done\n", "metrics": map[string]any{}, "resolved_image": resolvedImage,
	}}, f.now.Add(2*time.Second)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	environmentRecord, _ = f.client.Environment.Get(ctx, environmentRecord.ID)
	access, _ := f.client.NodeProjectAccess.Query().Where(nodeprojectaccess.NodeIDEQ(f.node.ID), nodeprojectaccess.ProjectIDEQ(project.ID)).Only(ctx)
	if environmentRecord.ImageUUID != resolvedImage || len(access.SuccessfulImages) != 1 || access.SuccessfulImages[0] != resolvedImage {
		t.Fatalf("recorded environment=%+v access=%+v", environmentRecord, access)
	}
}
