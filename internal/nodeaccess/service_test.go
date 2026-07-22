package nodeaccess

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/nodecommand"
	"github.com/XR-Lee/Gemcp/ent/nodeenrollment"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type nodeFixture struct {
	client  *ent.Client
	box     *secrets.Box
	tenant  *ent.Tenant
	project *ent.Project
	service *Service
	now     time.Time
}

func newNodeFixture(t *testing.T) *nodeFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Research").SetSlug("research").
		SetMonthlyBudgetMilli(100_000).SetMaxExperimentMilli(20_000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	service := NewService(client, box, "https://gemcp.example.com", true)
	service.now = func() time.Time { return now }
	return &nodeFixture{client: client, box: box, tenant: tenant, project: project, service: service, now: now}
}

func TestEnrollmentClaimApprovalAndFirstSync(t *testing.T) {
	f := newNodeFixture(t)
	ctx := context.Background()
	issued, code := issueNodeEnrollment(t, f)
	storedEnrollment, err := f.client.NodeEnrollment.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedEnrollment.CodeHash) != 32 || string(storedEnrollment.CodeHash) == code {
		t.Fatal("node enrollment code was not stored as a fixed-length digest")
	}
	inventory := validInventory()
	claimed, err := f.service.ClaimEnrollment(ctx, nodeprotocol.EnrollmentClaimRequest{Code: code, Inventory: inventory})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.EnrollmentID != issued.Enrollment.ID || claimed.Status != "pending_verification" || !strings.HasPrefix(claimed.NodeToken, claimed.TokenPrefix+"_") {
		t.Fatalf("claim = %+v", claimed)
	}
	retried, err := f.service.ClaimEnrollment(ctx, nodeprotocol.EnrollmentClaimRequest{Code: code, Inventory: inventory})
	if err != nil || retried.NodeToken != claimed.NodeToken || retried.NodeID != claimed.NodeID || retried.PairingCode != claimed.PairingCode {
		t.Fatalf("repeated claim=%+v err=%v", retried, err)
	}
	nodeRecord, err := f.client.SelfHostedNode.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodeRecord.TokenHash) != 32 || string(nodeRecord.TokenHash) == claimed.NodeToken {
		t.Fatal("Node token was stored recoverably")
	}
	pendingSync, err := f.service.Sync(ctx, claimed.NodeToken, nodeprotocol.SyncRequest{
		InstallationID: inventory.InstallationID, MachineFingerprint: inventory.MachineFingerprint,
		AgentVersion: inventory.AgentVersion, ProtocolVersion: nodeprotocol.Version, ObservedState: "online",
		Events: []nodeprotocol.Event{{ID: uuid.NewString(), Sequence: 1, Kind: "heartbeat", OccurredAt: f.now}},
	})
	if err != nil || pendingSync.DesiredState != "pending_verification" || pendingSync.AckedEventSequence != 1 {
		t.Fatalf("pending sync=%+v err=%v", pendingSync, err)
	}
	approved, err := f.service.ApproveEnrollment(ctx, f.tenant.ID, "owner", claimed.EnrollmentID, ApprovalInput{
		PairingCode: claimed.PairingCode, ProjectIDs: []string{f.project.PublicID.String()},
	})
	if err != nil || approved.Status != "active" || len(approved.ProjectIDs) != 1 || approved.ProjectIDs[0] != f.project.PublicID.String() {
		t.Fatalf("approved=%+v err=%v", approved, err)
	}
	activeSync, err := f.service.Sync(ctx, claimed.NodeToken, nodeprotocol.SyncRequest{
		InstallationID: inventory.InstallationID, MachineFingerprint: inventory.MachineFingerprint,
		AgentVersion: inventory.AgentVersion, ProtocolVersion: nodeprotocol.Version, ObservedState: "online",
		Events: []nodeprotocol.Event{{ID: uuid.NewString(), Sequence: 2, Kind: "heartbeat", OccurredAt: f.now.Add(time.Second)}},
	})
	if err != nil || activeSync.DesiredState != "active" || activeSync.AckedEventSequence != 2 {
		t.Fatalf("active sync=%+v err=%v", activeSync, err)
	}
	completed, err := f.client.NodeEnrollment.Query().Only(ctx)
	if err != nil || completed.Status != nodeenrollment.StatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed enrollment=%+v err=%v", completed, err)
	}
	principal, err := f.service.Authenticate(ctx, claimed.NodeToken)
	if err != nil || principal.NodePublicID != claimed.NodeID || principal.Status != "active" {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
	listed, err := f.service.List(ctx, f.tenant.ID)
	if err != nil || len(listed.Nodes) != 1 || len(listed.Enrollments) != 1 {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	encoded, _ := json.Marshal(listed)
	if strings.Contains(string(encoded), claimed.NodeToken) || strings.Contains(string(encoded), code) {
		t.Fatal("Owner list contains a plaintext setup code or Node token")
	}
}

func TestSyncDeliversCommandsDeduplicatesEventsAndQuarantinesIdentityChange(t *testing.T) {
	f := newNodeFixture(t)
	ctx := context.Background()
	_, code := issueNodeEnrollment(t, f)
	inventory := validInventory()
	claimed, err := f.service.ClaimEnrollment(ctx, nodeprotocol.EnrollmentClaimRequest{Code: code, Inventory: inventory})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ApproveEnrollment(ctx, f.tenant.ID, "owner", claimed.EnrollmentID, ApprovalInput{
		PairingCode: claimed.PairingCode, ProjectIDs: []string{f.project.PublicID.String()},
	}); err != nil {
		t.Fatal(err)
	}
	nodeRecord, _ := f.client.SelfHostedNode.Query().Only(ctx)
	commandRecord, err := f.client.NodeCommand.Create().
		SetTenantID(f.tenant.ID).SetNodeID(nodeRecord.ID).SetSequence(1).
		SetKind("reconcile").SetIdempotencyKey("reconcile-1").SetPayload(map[string]any{"reason": "test"}).SetAvailableAt(f.now).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eventID := uuid.NewString()
	request := nodeprotocol.SyncRequest{
		InstallationID: inventory.InstallationID, MachineFingerprint: inventory.MachineFingerprint,
		AgentVersion: inventory.AgentVersion, ProtocolVersion: nodeprotocol.Version, ObservedState: "reconciling",
		Events: []nodeprotocol.Event{{ID: eventID, Sequence: 1, Kind: "reconcile_started", OccurredAt: f.now}},
	}
	first, err := f.service.Sync(ctx, claimed.NodeToken, request)
	if err != nil || len(first.Commands) != 1 || first.Commands[0].ID != commandRecord.PublicID.String() || first.AckedEventSequence != 1 {
		t.Fatalf("first sync=%+v err=%v", first, err)
	}
	repeated, err := f.service.Sync(ctx, claimed.NodeToken, request)
	if err != nil || repeated.AckedEventSequence != 1 {
		t.Fatalf("repeated sync=%+v err=%v", repeated, err)
	}
	if count, _ := f.client.NodeEvent.Query().Count(ctx); count != 1 {
		t.Fatalf("event count=%d", count)
	}
	completed, err := f.service.Sync(ctx, claimed.NodeToken, nodeprotocol.SyncRequest{
		InstallationID: inventory.InstallationID, MachineFingerprint: inventory.MachineFingerprint,
		AgentVersion: inventory.AgentVersion, ProtocolVersion: nodeprotocol.Version, ObservedState: "online", LastCommandSequence: 1,
		Acknowledgements: []nodeprotocol.CommandAcknowledgement{{CommandID: commandRecord.PublicID.String(), Status: "completed", Result: map[string]any{"ok": true}}},
	})
	if err != nil || len(completed.Commands) != 0 {
		t.Fatalf("completed sync=%+v err=%v", completed, err)
	}
	commandRecord, _ = f.client.NodeCommand.Get(ctx, commandRecord.ID)
	if commandRecord.Status != nodecommand.StatusCompleted || commandRecord.CompletedAt == nil {
		t.Fatalf("command=%+v", commandRecord)
	}
	changedFingerprint := strings.Repeat("b", 64)
	quarantined, err := f.service.Sync(ctx, claimed.NodeToken, nodeprotocol.SyncRequest{
		InstallationID: inventory.InstallationID, MachineFingerprint: changedFingerprint,
		AgentVersion: inventory.AgentVersion, ProtocolVersion: nodeprotocol.Version, ObservedState: "online",
	})
	if err != nil || quarantined.DesiredState != "verification_required" {
		t.Fatalf("quarantined sync=%+v err=%v", quarantined, err)
	}
	nodeRecord, _ = f.client.SelfHostedNode.Get(ctx, nodeRecord.ID)
	if nodeRecord.Status != selfhostednode.StatusVerificationRequired {
		t.Fatalf("node status=%s", nodeRecord.Status)
	}
}

func TestEnrollmentIsolationValidationAndRevocation(t *testing.T) {
	f := newNodeFixture(t)
	ctx := context.Background()
	issued, code := issueNodeEnrollment(t, f)
	inventory := validInventory()
	claimed, err := f.service.ClaimEnrollment(ctx, nodeprotocol.EnrollmentClaimRequest{Code: code, Inventory: inventory})
	if err != nil {
		t.Fatal(err)
	}
	changed := inventory
	changed.MachineFingerprint = strings.Repeat("b", 64)
	if _, err := f.service.ClaimEnrollment(ctx, nodeprotocol.EnrollmentClaimRequest{Code: code, Inventory: changed}); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("changed claim error=%v", err)
	}
	if _, err := f.service.ApproveEnrollment(ctx, f.tenant.ID, "owner", issued.Enrollment.ID, ApprovalInput{
		PairingCode: "WRONG-CODE", ProjectIDs: []string{f.project.PublicID.String()},
	}); err == nil {
		t.Fatal("approval with the wrong pairing code succeeded")
	}
	otherTenant, _ := f.client.Tenant.Create().SetName("Other").Save(ctx)
	if _, err := f.service.ApproveEnrollment(ctx, otherTenant.ID, "owner", issued.Enrollment.ID, ApprovalInput{
		PairingCode: claimed.PairingCode, ProjectIDs: []string{f.project.PublicID.String()},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant approval error=%v", err)
	}
	revoked, err := f.service.RevokeEnrollment(ctx, f.tenant.ID, "owner", issued.Enrollment.ID)
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("revoked=%+v err=%v", revoked, err)
	}
	if _, err := f.service.Authenticate(ctx, claimed.NodeToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked Node token authentication error=%v", err)
	}
	if _, err := f.service.RevokeEnrollment(ctx, f.tenant.ID, "owner", issued.Enrollment.ID); err != nil {
		t.Fatalf("idempotent revocation error=%v", err)
	}
	disabled := NewService(f.client, f.box, "https://gemcp.example.com", false)
	if _, err := disabled.List(ctx, f.tenant.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled service error=%v", err)
	}
}

func TestAuthenticateAcceptsURLSafeUnderscoresInTokenSecret(t *testing.T) {
	f := newNodeFixture(t)
	rawToken := "gmn_1234567_secret_with_multiple_under_scores_and_entropy"
	nodeRecord, err := f.client.SelfHostedNode.Create().
		SetTenantID(f.tenant.ID).SetLabel("underscore-token").
		SetTokenPrefix("gmn_1234567").SetTokenHash(f.box.Digest("node-token", rawToken)).
		SetStatus(selfhostednode.StatusActive).
		SetInstallationID(uuid.NewString()).SetMachineFingerprint(strings.Repeat("a", 64)).
		SetHostname("gpu-home-a").SetOperatingSystem("linux").SetArchitecture("amd64").
		SetAgentVersion("test").SetProtocolVersion(nodeprotocol.Version).Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	principal, err := f.service.Authenticate(context.Background(), rawToken)
	if err != nil || principal.NodePublicID != nodeRecord.PublicID.String() {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
}

func issueNodeEnrollment(t *testing.T, f *nodeFixture) (EnrollmentIssueResult, string) {
	t.Helper()
	issued, err := f.service.IssueEnrollment(context.Background(), f.tenant.ID, "owner", EnrollmentIssueInput{Label: "gpu-home-a"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(issued.SetupURL)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(parsed.Fragment)
	if err != nil || values.Get("code") == "" {
		t.Fatalf("setup URL=%q error=%v", issued.SetupURL, err)
	}
	return issued, values.Get("code")
}

func validInventory() nodeprotocol.Inventory {
	return nodeprotocol.Inventory{
		InstallationID: uuid.NewString(), MachineFingerprint: strings.Repeat("a", 64), Hostname: "gpu-home-a",
		OperatingSystem: "linux", Architecture: "amd64", AgentVersion: "test", ProtocolVersion: nodeprotocol.Version,
		CPUCount: 16, MemoryBytes: 64 << 30,
		GPUs:    []nodeprotocol.GPU{{UUID: "GPU-test-a", Name: "NVIDIA GeForce RTX 3090", MemoryBytes: 24 << 30}},
		Storage: nodeprotocol.Storage{Root: "/mnt/gemcp", TotalBytes: 2 << 40, AvailableBytes: 1 << 40},
	}
}
