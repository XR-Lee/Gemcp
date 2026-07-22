package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/nodeenrollment"
	"github.com/XR-Lee/Gemcp/internal/buildinfo"
	"github.com/XR-Lee/Gemcp/internal/config"
	"github.com/XR-Lee/Gemcp/internal/nodeaccess"
	"github.com/XR-Lee/Gemcp/internal/nodeagent"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestNodeClientEnrollmentAgainstGemcpHTTP(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:node-setup-integration?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1000).SetMaxExperimentMilli(1000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	masterKey, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(masterKey)
	tlsServer := httptest.NewUnstartedServer(nil)
	publicURL := "https://" + tlsServer.Listener.Addr().String()
	application := New(Dependencies{
		Config: config.Config{Environment: "test", Address: ":0", PublicURL: publicURL, SelfHostedEnabled: true},
		Build:  buildinfo.New("test", "node-setup", "now"), DB: fakeDatabase{}, Ent: client, Secrets: box,
	})
	tlsServer.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		application.Handler.ServeHTTP(response, request)
	})
	tlsServer.StartTLS()
	defer tlsServer.Close()
	service := nodeaccess.NewService(client, box, publicURL, true)
	issued, err := service.IssueEnrollment(ctx, tenant.ID, "owner", nodeaccess.EnrollmentIssueInput{Label: "gpu-home-a"})
	if err != nil {
		t.Fatal(err)
	}
	origin, code, err := nodeagent.OriginFromSetupURL(issued.SetupURL)
	if err != nil || origin != publicURL {
		t.Fatalf("origin=%q code-prefix=%t err=%v", origin, strings.HasPrefix(code, "gne_"), err)
	}
	nodeClient, err := nodeagent.NewClient(origin, "test", nodeagent.WithHTTPClient(tlsServer.Client()))
	if err != nil {
		t.Fatal(err)
	}
	inventory := nodeprotocol.Inventory{
		InstallationID: uuid.NewString(), MachineFingerprint: strings.Repeat("a", 64), Hostname: "gpu-home-a",
		OperatingSystem: "linux", Architecture: "amd64", AgentVersion: "test", ProtocolVersion: nodeprotocol.Version,
		CPUCount: 8, MemoryBytes: 32 << 30, GPUs: []nodeprotocol.GPU{{UUID: "GPU-test", Name: "RTX 3090", MemoryBytes: 24 << 30}},
		Storage: nodeprotocol.Storage{Root: "/mnt/gemcp", TotalBytes: 1 << 40, AvailableBytes: 1 << 39},
	}
	claimed, err := nodeClient.Claim(ctx, code, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveEnrollment(ctx, tenant.ID, "owner", claimed.EnrollmentID, nodeaccess.ApprovalInput{
		PairingCode: claimed.PairingCode, ProjectIDs: []string{project.PublicID.String()},
	}); err != nil {
		t.Fatal(err)
	}
	syncResult, err := nodeClient.Sync(ctx, claimed.NodeToken, nodeprotocol.SyncRequest{
		InstallationID: inventory.InstallationID, MachineFingerprint: inventory.MachineFingerprint,
		AgentVersion: "test", ProtocolVersion: nodeprotocol.Version, ObservedState: "online",
		Events: []nodeprotocol.Event{{ID: uuid.NewString(), Sequence: 1, Kind: "heartbeat", OccurredAt: time.Now().UTC()}},
	})
	if err != nil || syncResult.DesiredState != "active" || syncResult.AckedEventSequence != 1 {
		t.Fatalf("sync=%+v err=%v", syncResult, err)
	}
	enrollment, err := client.NodeEnrollment.Query().Where(nodeenrollment.PublicIDEQ(uuid.MustParse(claimed.EnrollmentID))).Only(ctx)
	if err != nil || enrollment.Status != nodeenrollment.StatusCompleted {
		t.Fatalf("enrollment=%+v err=%v", enrollment, err)
	}
}
