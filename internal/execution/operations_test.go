package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/ent/serviceheartbeat"
	"github.com/XR-Lee/Gemcp/internal/servicehealth"
)

func TestRuntimeStatusReportsIndependentHeartbeats(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	if err := servicehealth.Beat(ctx, f.client, serviceheartbeat.RoleScheduler, "scheduler-test", map[string]any{"poll_interval_seconds": 5.0, "dispatch_enabled": false}); err != nil {
		t.Fatal(err)
	}
	if err := servicehealth.Beat(ctx, f.client, serviceheartbeat.RoleWatchdog, "watchdog-test", map[string]any{"poll_interval_seconds": 10.0}); err != nil {
		t.Fatal(err)
	}
	if err := servicehealth.Beat(ctx, f.client, serviceheartbeat.RoleNotification, "notification-test", map[string]any{"poll_interval_seconds": 10.0}); err != nil {
		t.Fatal(err)
	}
	operations := NewOperations(f.client, WithRuntimeConfiguration(false, 2, true), WithSelfHostedEnabled(true))
	status, err := operations.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.SchedulerHealthy || !status.WatchdogHealthy || !status.NotificationWorkerHealthy || status.GlobalConcurrency != 2 || !status.PublicURLConfigured || !status.SelfHostedEnabled {
		t.Fatalf("status = %+v", status)
	}
}

func TestOperationsListsAndStopsOnlyManagedResource(t *testing.T) {
	f := newExecutionFixture(t)
	experimentRecord := f.addExperiment(t)
	ctx := context.Background()
	if err := f.engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	operations := NewOperations(f.client)
	resources, err := operations.List(ctx, f.tenant.ID)
	if err != nil || len(resources) != 1 || resources[0].ProviderID == nil {
		t.Fatalf("resources=%+v err=%v", resources, err)
	}
	stopped, err := operations.RequestStop(ctx, f.tenant.ID, "owner-1", *resources[0].ProviderID)
	if err != nil || stopped.StopReason == nil || *stopped.StopReason != "owner_stop" {
		t.Fatalf("stopped=%+v err=%v", stopped, err)
	}
	experimentRecord, _ = f.client.Experiment.Get(ctx, experimentRecord.ID)
	if experimentRecord.State != "cancelling" || experimentRecord.DesiredState != "cancelled" {
		t.Fatalf("experiment=%+v", experimentRecord)
	}
	if _, err := operations.RequestStop(ctx, f.tenant.ID, "owner-1", "unmanaged-deployment"); err != ErrManagedResourceNotFound {
		t.Fatalf("unmanaged stop error=%v", err)
	}
}

func TestEmergencyStopIncludesSelfHostedAssignments(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	experimentRecord := f.addExperiment(t)
	attemptRecord, err := f.client.Attempt.Create().SetTenantID(f.tenant.ID).SetProjectID(f.project.ID).
		SetExperimentID(experimentRecord.ID).SetNumber(1).SetState("running").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	node, err := f.client.SelfHostedNode.Create().SetTenantID(f.tenant.ID).SetLabel("node").SetTokenPrefix("gmn_node").SetTokenHash([]byte("node-emergency")).
		SetStatus("active").SetObservedState("online").SetInstallationID("installation").SetMachineFingerprint(strings.Repeat("a", 64)).
		SetHostname("node").SetOperatingSystem("linux").SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion("1").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assignmentRecord, err := f.client.NodeAssignment.Create().SetTenantID(f.tenant.ID).SetProjectID(f.project.ID).
		SetExperimentID(experimentRecord.ID).SetAttemptID(attemptRecord.ID).SetNodeID(node.ID).SetState("running").SetOutputRef("experiments/test/outputs").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	operations := NewOperations(f.client)
	result, err := operations.EmergencyStop(ctx, f.tenant.ID, "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	experimentRecord, _ = f.client.Experiment.Get(ctx, experimentRecord.ID)
	assignmentRecord, _ = f.client.NodeAssignment.Get(ctx, assignmentRecord.ID)
	if result.Requested != 1 || experimentRecord.DesiredState != "cancelled" || experimentRecord.FailureCode == nil || *experimentRecord.FailureCode != "emergency_stop" ||
		assignmentRecord.StopReason == nil || *assignmentRecord.StopReason != "emergency" {
		t.Fatalf("result=%+v experiment=%+v assignment=%+v", result, experimentRecord, assignmentRecord)
	}
}

func TestEmergencyStopMarksAllOwnedResources(t *testing.T) {
	f := newExecutionFixture(t)
	f.addExperiment(t)
	if err := f.engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	operations := NewOperations(f.client)
	operations.now = func() time.Time { return f.now }
	result, err := operations.EmergencyStop(context.Background(), f.tenant.ID, "owner-1")
	if err != nil || result.Requested != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	resources, _ := operations.List(context.Background(), f.tenant.ID)
	if len(resources) != 1 || resources[0].StopReason == nil || *resources[0].StopReason != "emergency" {
		t.Fatalf("resources=%+v", resources)
	}
}
