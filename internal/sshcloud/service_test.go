package sshcloud

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/cloudsshassignment"
	"github.com/XR-Lee/Gemcp/ent/cloudsshnode"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/ssh"
)

type scriptedConn struct {
	runs    []string
	uploads []string
	script  map[string]scriptedResult
}

type scriptedResult struct {
	output string
	err    error
}

func (c *scriptedConn) Run(_ context.Context, command string, _ int) (string, error) {
	c.runs = append(c.runs, command)
	for _, marker := range []string{"gemcp-host-start", "gemcp-host-status", "gemcp-host-logs", "gemcp-host-stop"} {
		if strings.Contains(command, marker) {
			if result, ok := c.script[marker]; ok {
				return result.output, result.err
			}
			switch marker {
			case "gemcp-host-start":
				return "gemcp-host-start\nPID 4242", nil
			case "gemcp-host-status":
				return "gemcp-host-status\nRUNNING 4242", nil
			case "gemcp-host-logs":
				return "gemcp-host-logs\nepoch 1 loss=0.41\n", nil
			default:
				return marker, nil
			}
		}
	}
	for key, result := range c.script {
		if strings.Contains(command, key) {
			return result.output, result.err
		}
	}
	return "", nil
}

func (c *scriptedConn) Upload(_ context.Context, dest string, _ io.Reader) error {
	c.uploads = append(c.uploads, dest)
	return nil
}

func (c *scriptedConn) Close() error { return nil }

func healthyScript() map[string]scriptedResult {
	return map[string]scriptedResult{
		"uname -s -m": {output: "Linux x86_64"},
		"nvidia-smi":  {output: "NVIDIA GeForce RTX 4090, GPU-12345678-1234-1234-1234-123456789abc, 24576"},
		"nproc":       {output: "16"},
		"MemTotal":    {output: "65331148"},
	}
}

func newServiceFixture(t *testing.T) (*Service, *ent.Client, *ent.Tenant, *ent.Project) {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := client.Tenant.Create().SetName("lab").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().SetTenantID(tenant.ID).SetName("research").SetSlug("research").
		SetMonthlyBudgetMilli(1000).SetMaxExperimentMilli(1000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.Enabled = true
	config.InstanceID = "test"
	service, err := NewService(client, box, config)
	if err != nil {
		t.Fatal(err)
	}
	service.skipAsyncProbe = true
	return service, client, tenant, project
}

func TestEncryptCredentialNeverStoresPlaintextAndRejectsBadAAD(t *testing.T) {
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	ciphertext, err := EncryptCredential(box, Credential{Method: "password", Password: "cloud-instance-password"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "cloud-instance-password") {
		t.Fatal("ciphertext contained the password")
	}
	got, err := DecryptCredential(box, ciphertext)
	if err != nil || got.Password != "cloud-instance-password" || got.Method != "password" {
		t.Fatalf("round-trip credential=%+v err=%v", got, err)
	}
	if _, err := box.Decrypt(ciphertext, "gemcp:provider-token:v1"); err == nil {
		t.Fatal("Cloud SSH credential accepted Provider AAD")
	}
}

func TestValidateTargetRejectsLoopback(t *testing.T) {
	if _, err := normalizeTarget("127.0.0.1", 22, "ubuntu"); err == nil {
		t.Fatal("accepted loopback IP")
	}
	if _, err := normalizeTarget("localhost", 22, "ubuntu"); err == nil {
		t.Fatal("accepted localhost")
	}
	if _, err := normalizeTarget("10.0.0.8", 22, "ubuntu"); err != nil {
		t.Fatalf("rejected private VPC address: %v", err)
	}
}

func TestProbeRecordsInventoryWithoutSecrets(t *testing.T) {
	service, _, tenant, _ := newServiceFixture(t)
	ctx := context.Background()
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return &scriptedConn{script: healthyScript()}, "SHA256:fixed-fingerprint", nil
	})
	view, err := service.Create(ctx, tenant.ID, "owner", CreateInput{
		Label: "gpu-cloud-1", Host: "203.0.113.10", User: "ubuntu", AuthMethod: "password", Password: "super-secret-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, forbidden := range []string{"super-secret-password", "BEGIN", "ciphertext", "private_key"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) && forbidden != "ciphertext" {
			t.Fatalf("node view leaked %q: %s", forbidden, body)
		}
	}
	if strings.Contains(body, "ciphertext") {
		t.Fatalf("node view leaked ciphertext: %s", body)
	}
	if view.Status != string(cloudsshnode.StatusActive) || view.HostKeyFingerprint != "SHA256:fixed-fingerprint" {
		t.Fatalf("view=%+v", view)
	}
	if len(inventoryGPUNames(view.Inventory)) != 1 {
		t.Fatalf("inventory=%v", view.Inventory)
	}
}

func TestProbeDoesNotInstallSoftwareAndAllowsNoGPU(t *testing.T) {
	service, _, tenant, _ := newServiceFixture(t)
	ctx := context.Background()
	script := healthyScript()
	script["nvidia-smi"] = scriptedResult{output: "No devices were found", err: io.EOF}
	conn := &scriptedConn{script: script}
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return conn, "SHA256:fixed-fingerprint", nil
	})
	view, err := service.Create(ctx, tenant.ID, "owner", CreateInput{
		Label: "cpu-cloud-1", Host: "203.0.113.10", User: "ubuntu", AuthMethod: "password", Password: "super-secret-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != string(cloudsshnode.StatusActive) {
		t.Fatalf("view=%+v", view)
	}
	for _, command := range conn.runs {
		if strings.Contains(command, "docker") || strings.Contains(command, "apt-get") || strings.Contains(command, "nvidia-ctk") {
			t.Fatalf("probe installed software: %s", command)
		}
	}
}

func TestHostKeyChangeMarksNodeUnschedulable(t *testing.T) {
	service, client, tenant, _ := newServiceFixture(t)
	ctx := context.Background()
	service.WithDial(func(_ context.Context, _ Target, _ Credential, expected string) (Conn, string, error) {
		if expected != "" && expected != "SHA256:first" {
			return nil, "SHA256:second", ErrHostKeyChanged
		}
		return &scriptedConn{script: healthyScript()}, "SHA256:first", nil
	})
	view, err := service.Create(ctx, tenant.ID, "owner", CreateInput{
		Label: "gpu-cloud-1", Host: "203.0.113.10", User: "ubuntu", AuthMethod: "password", Password: "super-secret-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return nil, "SHA256:second", ErrHostKeyChanged
	})
	if _, err := service.Probe(ctx, tenant.ID, "owner", view.ID); err != ErrHostKeyChanged {
		t.Fatalf("probe error=%v", err)
	}
	record, err := client.CloudSSHNode.Query().Where(cloudsshnode.TenantIDEQ(tenant.ID)).Only(ctx)
	if err != nil || record.Status != cloudsshnode.StatusHostKeyChanged {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func TestAuthorizeCreatesZeroCostSSHCloudRuntime(t *testing.T) {
	service, client, tenant, project := newServiceFixture(t)
	ctx := context.Background()
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return &scriptedConn{script: healthyScript()}, "SHA256:fixed-fingerprint", nil
	})
	view, err := service.Create(ctx, tenant.ID, "owner", CreateInput{
		Label: "gpu-cloud-1", Host: "203.0.113.10", User: "ubuntu", AuthMethod: "password", Password: "super-secret-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := service.Authorize(ctx, tenant.ID, "owner", view.ID, AuthorizeInput{
		ProjectIDs: []string{project.PublicID.String()}, Image: "pytorch/pytorch@sha256:" + strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(authorized.ProjectIDs) != 1 {
		t.Fatalf("project_ids=%v", authorized.ProjectIDs)
	}
	environmentRecord, err := client.Environment.Query().Where(environment.ProjectIDEQ(project.ID), environment.BackendEQ(environment.BackendSSHCloud)).Only(ctx)
	if err != nil || environmentRecord.ImageUUID != HostImage {
		t.Fatalf("environment=%+v err=%v", environmentRecord, err)
	}
	profile, err := client.ResourceProfile.Query().Where(resourceprofile.ProjectIDEQ(project.ID), resourceprofile.BackendEQ(resourceprofile.BackendSSHCloud)).Only(ctx)
	if err != nil || profile.PriceToMilli != 0 || profile.Region != Backend {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
}

func TestPrivateKeyCredentialRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(block)
	boxKey, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(boxKey)
	ciphertext, err := EncryptCredential(box, Credential{Method: "private_key", PrivateKey: string(pemBytes)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "BEGIN") {
		t.Fatal("ciphertext contained PEM")
	}
	got, err := DecryptCredential(box, ciphertext)
	if err != nil || !strings.Contains(got.PrivateKey, "BEGIN") {
		t.Fatalf("credential=%+v err=%v", got, err)
	}
}

func TestDispatchDoesNotCrossBackends(t *testing.T) {
	service, client, tenant, project := newServiceFixture(t)
	ctx := context.Background()
	environmentRecord, err := client.Environment.Create().SetProjectID(project.ID).SetBackend("self_hosted").SetName("local").
		SetImageUUID("pytorch/pytorch@sha256:" + strings.Repeat("b", 64)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := client.ResourceProfile.Create().SetProjectID(project.ID).SetBackend("self_hosted").SetName("local").
		SetRegion("self_hosted").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).
		SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := client.Repository.Create().SetProjectID(project.ID).SetName("repo").SetSSHURL("git@github.com:o/r.git").
		SetSSHHost("github.com").SetDefaultBranch("main").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := client.Experiment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetRepositoryID(repo.ID).
		SetEnvironmentID(environmentRecord.ID).SetResourceProfileID(profile.ID).SetCommitSha(strings.Repeat("c", 40)).
		SetCommand("true").SetMaxRuntimeSeconds(60).SetTimeoutExtensionSeconds(30).SetTerminationGraceSeconds(5).
		SetReservedCostMilli(0).SetOutputPath("managed://x").
		SetEnvironmentSnapshot(map[string]any{"backend": "self_hosted", "image_uuid": environmentRecord.ImageUUID}).
		SetResourceSnapshot(map[string]any{"backend": "self_hosted"}).SetRepositorySnapshot(map[string]any{"id": repo.PublicID.String()}).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := service.Dispatch(ctx, tx, experiment, time.Now().UTC())
	_ = tx.Rollback()
	if err != nil || ok {
		t.Fatalf("self-hosted experiment was accepted by Cloud SSH dispatch ok=%t err=%v", ok, err)
	}
}

func TestEnsureForProjectProbesAndAuthorizesWithoutGPU(t *testing.T) {
	service, client, tenant, project := newServiceFixture(t)
	ctx := context.Background()
	skipProbe := false
	script := healthyScript()
	script["nvidia-smi"] = scriptedResult{output: "", err: io.EOF}
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return &scriptedConn{script: script}, "SHA256:fixed-fingerprint", nil
	})
	view, err := service.Create(ctx, tenant.ID, "agent:token-1", CreateInput{
		Label: "cpu-cloud-1", Host: "203.0.113.10", User: "ubuntu", AuthMethod: "password", Password: "super-secret-password",
		Probe: &skipProbe, ProjectID: project.PublicID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != string(cloudsshnode.StatusPendingProbe) {
		t.Fatalf("create without probe status=%s", view.Status)
	}
	got, err := service.EnsureForProject(ctx, tenant.ID, "agent:token-1", project.PublicID.String(), "ignored-image")
	if err != nil || !got.Probed || got.EnvironmentName == "" || got.NodeID != view.ID || got.ResolvedImage != HostImage {
		t.Fatalf("ensure=%+v err=%v", got, err)
	}
	environmentRecord, err := client.Environment.Query().Where(environment.ProjectIDEQ(project.ID), environment.BackendEQ(environment.BackendSSHCloud)).Only(ctx)
	if err != nil || environmentRecord.ImageUUID != HostImage {
		t.Fatalf("environment=%+v err=%v", environmentRecord, err)
	}
}

func TestParseMetricsJSONAcceptsBoundedObjectsOnly(t *testing.T) {
	if got := parseMetricsJSON(`{"overall_accuracy": 86.4}`); got["overall_accuracy"] != 86.4 {
		t.Fatalf("parseMetricsJSON() = %#v", got)
	}
	if got := parseMetricsJSON(`["not-an-object"]`); len(got) != 0 {
		t.Fatalf("accepted a JSON array: %#v", got)
	}
	if got := parseMetricsJSON(""); len(got) != 0 {
		t.Fatalf("accepted empty metrics: %#v", got)
	}
}

func TestReconcileProjectsLiveLogsAndMetrics(t *testing.T) {
	service, client, tenant, project := newServiceFixture(t)
	ctx := context.Background()
	experiment, _ := seedRunningCloudSSH(t, service, client, tenant, project)
	conn := &scriptedConn{script: map[string]scriptedResult{
		"gemcp-host-status":    {output: "gemcp-host-status\nRUNNING 4242\n"},
		"gemcp-host-logs":      {output: "gemcp-host-logs\nepoch 1 loss=0.41\n"},
		"outputs/metrics.json": {output: `{"overall_accuracy":86.4,"loss":0.41}`},
	}}
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return conn, "SHA256:fixed-fingerprint", nil
	})
	handled, err := service.Reconcile(ctx, experiment.ID, time.Now().UTC())
	if err != nil || !handled {
		t.Fatalf("Reconcile() handled=%t err=%v", handled, err)
	}
	experiment, _ = client.Experiment.Get(ctx, experiment.ID)
	if experiment.State != "running" || experiment.LogTail == nil || *experiment.LogTail != "epoch 1 loss=0.41\n" {
		t.Fatalf("live log projection = state=%s log=%v", experiment.State, experiment.LogTail)
	}
	if experiment.Metrics["overall_accuracy"] != 86.4 {
		t.Fatalf("live metrics projection = %#v", experiment.Metrics)
	}
}

func TestReconcileCollectsMetricsBeforeRemoteCleanup(t *testing.T) {
	service, client, tenant, project := newServiceFixture(t)
	ctx := context.Background()
	experiment, _ := seedRunningCloudSSH(t, service, client, tenant, project)
	conn := &scriptedConn{script: map[string]scriptedResult{
		"gemcp-host-status":    {output: "gemcp-host-status\nSTOPPED 0\n"},
		"gemcp-host-logs":      {output: "gemcp-host-logs\noverall_accuracy 86.4\n"},
		"outputs/metrics.json": {output: `{"overall_accuracy":86.4}`},
	}}
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return conn, "SHA256:fixed-fingerprint", nil
	})
	if _, err := service.Reconcile(ctx, experiment.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	experiment, _ = client.Experiment.Get(ctx, experiment.ID)
	if experiment.State != "succeeded" || experiment.Metrics["overall_accuracy"] != 86.4 {
		t.Fatalf("terminal experiment = state=%s metrics=%#v", experiment.State, experiment.Metrics)
	}
	catAt, removedAt := -1, -1
	for index, command := range conn.runs {
		if strings.Contains(command, "outputs/metrics.json") && catAt < 0 {
			catAt = index
		}
		if strings.Contains(command, "rm -rf") {
			removedAt = index
			if strings.Contains(command, "/home/") || strings.Contains(command, "/root/") {
				t.Fatalf("cleanup deleted a home path: %s", command)
			}
		}
	}
	if catAt < 0 || removedAt < 0 || catAt > removedAt {
		t.Fatalf("metrics were not collected before cleanup cat=%d rm=%d runs=%q", catAt, removedAt, conn.runs)
	}
}

func TestCancelKillsProcessWithoutDeletingCwd(t *testing.T) {
	service, client, tenant, project := newServiceFixture(t)
	ctx := context.Background()
	experiment, _ := seedRunningCloudSSH(t, service, client, tenant, project)
	if _, err := client.Experiment.UpdateOneID(experiment.ID).SetDesiredState("cancelled").Save(ctx); err != nil {
		t.Fatal(err)
	}
	conn := &scriptedConn{script: map[string]scriptedResult{
		"gemcp-host-status": {output: "gemcp-host-status\nSTOPPED 137\n"},
		"gemcp-host-logs":   {output: "gemcp-host-logs\nkilled\n"},
	}}
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return conn, "SHA256:fixed-fingerprint", nil
	})
	if _, err := service.Reconcile(ctx, experiment.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	killed := false
	for _, command := range conn.runs {
		if strings.Contains(command, "gemcp-host-stop") {
			killed = true
		}
		if strings.Contains(command, "rm -rf") && !strings.Contains(command, "/var/tmp/gemcp/") {
			t.Fatalf("cancel deleted cwd: %s", command)
		}
	}
	if !killed {
		t.Fatalf("cancel did not stop the host process: %q", conn.runs)
	}
}

func TestDispatchStartsHostProcessWithoutImage(t *testing.T) {
	service, client, tenant, project := newServiceFixture(t)
	ctx := context.Background()
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return &scriptedConn{script: healthyScript()}, "SHA256:fixed-fingerprint", nil
	})
	view, err := service.Create(ctx, tenant.ID, "owner", CreateInput{
		Label: "host-1", Host: "203.0.113.10", User: "ubuntu", AuthMethod: "password", Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authorize(ctx, tenant.ID, "owner", view.ID, AuthorizeInput{
		ProjectIDs: []string{project.PublicID.String()}, Image: HostImage,
	}); err != nil {
		t.Fatal(err)
	}
	environmentRecord, err := client.Environment.Query().Where(environment.BackendEQ(environment.BackendSSHCloud)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := client.ResourceProfile.Query().Where(resourceprofile.BackendEQ(resourceprofile.BackendSSHCloud)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := client.Experiment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).
		SetEnvironmentID(environmentRecord.ID).SetResourceProfileID(profile.ID).SetCommitSha(HostCommit).
		SetExecutionMode("argv").SetArgv([]string{"python", "train.py"}).SetCommand("python train.py").
		SetMaxRuntimeSeconds(60).SetTimeoutExtensionSeconds(0).SetTerminationGraceSeconds(5).
		SetReservedCostMilli(0).SetOutputPath("managed://x").
		SetEnvironmentSnapshot(map[string]any{"backend": Backend, "image_uuid": HostImage, "working_directory": "/opt/exp"}).
		SetResourceSnapshot(map[string]any{"backend": Backend}).SetRepositorySnapshot(map[string]any{"source": "host"}).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := service.Dispatch(ctx, tx, experiment, time.Now().UTC())
	if err != nil || !ok {
		_ = tx.Rollback()
		t.Fatalf("Dispatch() ok=%t err=%v", ok, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	conn := &scriptedConn{script: map[string]scriptedResult{}}
	service.WithDial(func(context.Context, Target, Credential, string) (Conn, string, error) {
		return conn, "SHA256:fixed-fingerprint", nil
	})
	if _, err := service.Reconcile(ctx, experiment.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	started := false
	for _, command := range conn.runs {
		if strings.Contains(command, "docker") {
			t.Fatalf("dispatch used Docker: %s", command)
		}
		if strings.Contains(command, "gemcp-host-start") && strings.Contains(command, "/opt/exp") {
			started = true
		}
	}
	if !started {
		t.Fatalf("host process was not started in cwd: %q", conn.runs)
	}
}

func seedRunningCloudSSH(t *testing.T, service *Service, client *ent.Client, tenant *ent.Tenant, project *ent.Project) (*ent.Experiment, *ent.CloudSSHAssignment) {
	t.Helper()
	ctx := context.Background()
	ciphertext, err := EncryptCredential(service.box, Credential{Method: "password", Password: "cloud-instance-password"})
	if err != nil {
		t.Fatal(err)
	}
	environmentRecord, err := client.Environment.Create().SetProjectID(project.ID).SetBackend("ssh_cloud").SetName("ssh").
		SetImageUUID(HostImage).SetRecipeRef(recipePrefix + "11111111-1111-1111-1111-111111111111").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := client.ResourceProfile.Create().SetProjectID(project.ID).SetBackend("ssh_cloud").SetName("ssh").
		SetRegion("ssh_cloud").SetGpuNames([]string{"host"}).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).
		SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := client.Experiment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).
		SetEnvironmentID(environmentRecord.ID).SetResourceProfileID(profile.ID).SetCommitSha(HostCommit).
		SetCommand("python train.py").SetState("running").SetMaxRuntimeSeconds(60).SetTimeoutExtensionSeconds(30).
		SetTerminationGraceSeconds(5).SetReservedCostMilli(0).SetOutputPath("managed://experiments/x/outputs").
		SetEnvironmentSnapshot(map[string]any{"backend": Backend, "image_uuid": HostImage, "working_directory": "/opt/exp"}).
		SetResourceSnapshot(map[string]any{"backend": Backend, "cpu_to": 8, "memory_to_gb": 32}).
		SetRepositorySnapshot(map[string]any{"source": "host"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attemptRecord, err := client.Attempt.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).
		SetExperimentID(experiment.ID).SetNumber(1).SetState("running").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	node, err := client.CloudSSHNode.Create().SetTenantID(tenant.ID).SetLabel("cloud-gpu").SetSSHHost("203.0.113.10").
		SetSSHUser("ubuntu").SetAuthMethod("password").SetCredentialCiphertext(ciphertext).SetStatus("active").
		SetHostKeyFingerprint("SHA256:fixed-fingerprint").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := client.CloudSSHAssignment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).
		SetExperimentID(experiment.ID).SetAttemptID(attemptRecord.ID).SetNodeID(node.ID).
		SetState(cloudsshassignment.StateRunning).SetRemoteDir("/var/tmp/gemcp/11111111-1111-1111-1111-111111111111").
		SetOutputRef("experiments/x/outputs").SetContainerID("4242").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return experiment, assignment
}

func TestCreateEnforcesTwentyNodeCap(t *testing.T) {
	service, _, tenant, _ := newServiceFixture(t)
	ctx := context.Background()
	skip := false
	for i := 0; i < maxActiveNodes; i++ {
		if _, err := service.Create(ctx, tenant.ID, "owner", CreateInput{
			Label: fmt.Sprintf("n-%d", i), Host: fmt.Sprintf("203.0.113.%d", i+1), User: "ubuntu",
			AuthMethod: "password", Password: "secret", Probe: &skip,
		}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, err := service.Create(ctx, tenant.ID, "owner", CreateInput{
		Label: "overflow", Host: "203.0.113.250", User: "ubuntu", AuthMethod: "password", Password: "secret", Probe: &skip,
	}); err != ErrNodeLimit {
		t.Fatalf("overflow error=%v", err)
	}
}
