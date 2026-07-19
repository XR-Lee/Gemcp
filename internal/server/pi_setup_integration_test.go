package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/agentenrollment"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentaccess"
	"github.com/XR-Lee/Gemcp/internal/buildinfo"
	"github.com/XR-Lee/Gemcp/internal/config"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestPiSetupInstallerAgainstGemcpMCP(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	client := enttest.Open(t, dialect.SQLite, "file:pi-setup-integration?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).
		SetName("Research").
		SetSlug("research").
		SetMonthlyBudgetMilli(100_000).
		SetMaxExperimentMilli(20_000).
		SetMaxConcurrency(1).
		SetMaxRuntimeSeconds(3600).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	masterKey, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(masterKey)
	if err != nil {
		t.Fatal(err)
	}

	tlsServer := httptest.NewUnstartedServer(nil)
	publicURL := "https://" + tlsServer.Listener.Addr().String()
	application := New(Dependencies{
		Config:  config.Config{Environment: "test", Address: ":0", PublicURL: publicURL},
		Build:   buildinfo.New("test", "pi-setup", "now"),
		DB:      fakeDatabase{},
		Ent:     client,
		Secrets: box,
	})
	var mcpRequests atomic.Int32
	var identifiedMCPRequests atomic.Int32
	tlsServer.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			mcpRequests.Add(1)
			if r.Header.Get("User-Agent") == "Gemcp-Pi-Setup/1" {
				identifiedMCPRequests.Add(1)
			}
		}
		application.Handler.ServeHTTP(w, r)
	})
	tlsServer.StartTLS()
	defer tlsServer.Close()

	tokenDays := 30
	setupMinutes := 15
	issued, err := agentaccess.NewService(client, box, publicURL).IssueEnrollment(
		ctx, tenant.ID, "owner", project.PublicID.String(), agentaccess.EnrollmentIssueInput{
			Label: "pi-integration", Scopes: []string{"read", "submit", "cancel"},
			ExpiresInDays: &tokenDays, SetupExpiresInMinutes: &setupMinutes,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	parsedSetupURL, err := url.Parse(issued.SetupURL)
	if err != nil {
		t.Fatal(err)
	}
	setupCode := parsedSetupURL.Fragment
	if values, parseErr := url.ParseQuery(parsedSetupURL.Fragment); parseErr == nil {
		setupCode = values.Get("code")
	}

	response, err := tlsServer.Client().Get(publicURL + "/agent/setup/install.mjs")
	if err != nil {
		t.Fatal(err)
	}
	installer, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != 200 {
		t.Fatalf("installer status=%d err=%v", response.StatusCode, readErr)
	}
	installerPath := filepath.Join(t.TempDir(), "install.mjs")
	if err := os.WriteFile(installerPath, installer, 0o700); err != nil {
		t.Fatal(err)
	}
	agentDir := t.TempDir()
	adapterDir := filepath.Join(agentDir, "npm", "node_modules", "pi-mcp-adapter")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adapterDir, "package.json"), []byte(`{"version":"2.10.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, installerPath, issued.SetupURL)
	command.Env = append(os.Environ(),
		"PI_CODING_AGENT_DIR="+agentDir,
		"NODE_TLS_REJECT_UNAUTHORIZED=0",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("installer output=%s error=%v", output, err)
	}
	if !strings.Contains(string(output), "GEMCP_PI_SETUP_OK") || !strings.Contains(string(output), "checks=tools,guide,options,cost") {
		t.Fatalf("installer output=%s", output)
	}
	if strings.Contains(string(output), setupCode) || strings.Contains(string(output), "gmc_") {
		t.Fatalf("installer output contains a setup or Agent secret: %s", output)
	}

	enrollmentID, err := uuid.Parse(issued.Enrollment.ID)
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := client.AgentEnrollment.Query().Where(agentenrollment.PublicIDEQ(enrollmentID)).WithAgentToken().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := enrollment.Edges.AgentTokenOrErr()
	if err != nil {
		t.Fatal(err)
	}
	if enrollment.Status != agentenrollment.StatusCompleted || strings.Join(token.Scopes, ",") != "read,submit,cancel" {
		t.Fatalf("enrollment=%s token_scopes=%v", enrollment.Status, token.Scopes)
	}
	if mcpRequests.Load() == 0 || identifiedMCPRequests.Load() != mcpRequests.Load() {
		t.Fatalf("identified MCP requests=%d total=%d", identifiedMCPRequests.Load(), mcpRequests.Load())
	}
	configInfo, err := os.Stat(filepath.Join(agentDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("Pi MCP config mode=%v", configInfo.Mode().Perm())
	}
}
