package guides

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEmbeddedGuidesContainSafetyWorkflow(t *testing.T) {
	for name, guide := range map[string]string{"agent": AgentMCP(), "owner": OwnerMCP()} {
		for _, required := range []string{"get_project_options", "get_project_cost", "idempotency", "Agent Token"} {
			if !strings.Contains(guide, required) {
				t.Fatalf("%s guide does not contain %q", name, required)
			}
		}
		if strings.Contains(guide, "gmc_") || strings.Contains(guide, "Bearer gmc") {
			t.Fatalf("%s guide contains a token-shaped example", name)
		}
	}
	if !strings.Contains(AgentMCP(), "Wait for explicit human approval") {
		t.Fatal("Agent guide omits the paid-work approval boundary")
	}
	if !strings.Contains(OwnerMCP(), "every prepared proposal requires per-experiment Owner confirmation of the exact digest") || strings.Contains(OwnerMCP(), "Establish one of these policies before use") {
		t.Fatal("Owner guide does not enforce the current exact-digest confirmation boundary")
	}
}

func TestPiSetupAssetsUseTrustedRenderedOrigin(t *testing.T) {
	origin := "https://gemcp.example.com"
	setup := PiSetup(origin)
	installer := PiSetupInstaller(origin)
	tool := GemcpTool()
	for name, content := range map[string]string{"setup": setup, "installer": installer, "tool": tool} {
		if strings.Contains(content, "{{GEMCP_PUBLIC_URL}}") {
			t.Fatalf("%s retained the public URL placeholder", name)
		}
	}
	for _, required := range []string{origin + "/agent/setup/install.mjs", "GEMCP_PI_SETUP_OK", "explicit human approval", "Claude Code", "Codex", "OpenCode", "Grok"} {
		if !strings.Contains(setup, required) {
			t.Fatalf("setup guide does not contain %q", required)
		}
	}
	if !strings.Contains(installer, "const trustedOrigin = '"+origin+"'") || !strings.Contains(installer, "GEMCP_PI_SETUP_INSTALLER_V1") {
		t.Fatal("installer does not pin the rendered public origin")
	}
	if !strings.Contains(tool, "GEMCP_TOOL_HELPER_V1") || !strings.Contains(tool, "verifyConfiguredServer") {
		t.Fatal("Gemcp helper is incomplete")
	}
}

func TestNodeSetupIsSelfContainedAgentHandoff(t *testing.T) {
	origin := "https://gemcp.example.com"
	commit := strings.Repeat("a", 40)
	setup := NodeSetup(origin, "0.10.3", commit, "en")
	chinese := NodeSetup(origin, "0.10.3", commit, "zh")
	for _, required := range []string{
		origin + "/node/setup?lang=en#code=...", origin + "/node/setup?lang=zh", "v0.10.3", commit,
		"git@github.com:XR-Lee/Gemcp.git", "make build-node", "pending_verification",
		"Do not install or upgrade the NVIDIA Driver", "/var/lib/gemcp-node/storage", "upgrade-gemcp-node.sh",
		`git fetch --depth 1 origin "` + commit + `"`,
		"do not assume a `v0.10.3` git tag exists",
	} {
		if !strings.Contains(setup, required) {
			t.Fatalf("node setup guide does not contain %q", required)
		}
	}
	if strings.Contains(setup, `git clone --branch "v0.10.3"`) || strings.Contains(chinese, `git clone --branch "v0.10.3"`) {
		t.Fatal("node setup must clone the exact commit, not a version tag")
	}
	for _, required := range []string{
		origin + "/node/setup?lang=zh#code=...", origin + "/node/setup?lang=en", "v0.10.3", commit,
		"代码仓库", "不得安装或升级 NVIDIA Driver", "/var/lib/gemcp-node/storage", "pending_verification", "upgrade-gemcp-node.sh",
		`git fetch --depth 1 origin "` + commit + `"`,
		"不要假设存在 `v0.10.3` 标签",
	} {
		if !strings.Contains(chinese, required) {
			t.Fatalf("Chinese node setup guide does not contain %q", required)
		}
	}
	for _, content := range []string{setup, chinese} {
		for _, placeholder := range []string{"{{GEMCP_PUBLIC_URL}}", "{{GEMCP_VERSION}}", "{{GEMCP_COMMIT}}"} {
			if strings.Contains(content, placeholder) {
				t.Fatalf("node setup guide retained %q", placeholder)
			}
		}
	}
}

func TestLocalDevBootstrapMatchesBinary(t *testing.T) {
	for _, filename := range []string{
		filepath.Join("..", "scripts", "bootstrap-local.sh"),
		filepath.Join("..", "scripts", "dev-serve.sh"),
		filepath.Join("..", "scripts", "local-http-smoke.sh"),
		filepath.Join("..", "deploy", "env.local.example"),
	} {
		info, err := os.Stat(filename)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(filename, ".sh") && info.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable", filename)
		}
		payload, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		body := string(payload)
		switch {
		case strings.HasSuffix(filename, "bootstrap-local.sh"):
			for _, required := range []string{"deploy/env.local.example", "GEMCP_MASTER_KEY", "make build", "PostgreSQL", "no GPU"} {
				if !strings.Contains(body, required) {
					t.Fatalf("%s does not contain %q", filename, required)
				}
			}
		case strings.HasSuffix(filename, "dev-serve.sh"):
			if !strings.Contains(body, "gemcp\" serve") && !strings.Contains(body, "gemcp serve") {
				t.Fatalf("%s does not exec gemcp serve", filename)
			}
		case strings.HasSuffix(filename, "local-http-smoke.sh"):
			for _, required := range []string{"/healthz", "/readyz", "/api/v1/setup/status", "skip_provider", "/mcp", "initialize"} {
				if !strings.Contains(body, required) {
					t.Fatalf("%s does not contain %q", filename, required)
				}
			}
		case strings.HasSuffix(filename, "env.local.example"):
			for _, required := range []string{
				"GEMCP_ENV=development", "GEMCP_SECURE_COOKIES=false", "GEMCP_AUTO_MIGRATE=true",
				"GEMCP_PUBLIC_URL=http://127.0.0.1:8080",
			} {
				if !strings.Contains(body, required) {
					t.Fatalf("%s does not contain %q", filename, required)
				}
			}
		}
	}
}

func TestEnvExampleVersionMatchesReleaseFile(t *testing.T) {
	version, err := os.ReadFile(filepath.Join("..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile(filepath.Join("..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	want := "GEMCP_VERSION=" + strings.TrimSpace(string(version))
	if !strings.Contains(string(example), want) {
		t.Fatalf(".env.example does not contain %q", want)
	}
}

func TestNodeUpgradeScriptPreservesEnrollmentAndRollsBack(t *testing.T) {
	filename := filepath.Join("..", "deploy", "upgrade-gemcp-node.sh")
	payload, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("node upgrade script is not executable")
	}
	script := string(payload)
	for _, required := range []string{
		"GEMCP_NODE_EXPECTED_VERSION", "GEMCP_NODE_EXPECTED_COMMIT", "docker ps -a",
		"/etc/gemcp-node/config.json", "/etc/gemcp-node/credential", "/var/lib/gemcp-node/state.db",
		"gemcp-node.previous", "systemctl is-active --quiet", "automatic rollback",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("node upgrade script does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"gemcp-node enroll", "/node-enrollments", "cat /etc/gemcp-node/credential", "rm -rf /etc/gemcp-node"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("node upgrade script contains forbidden operation %q", forbidden)
		}
	}
}

func TestPiSetupInstallerRecoversLostCompletionResponse(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	var claims atomic.Int32
	var completions atomic.Int32
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Gemcp-Pi-Setup/1" {
			t.Errorf("%s User-Agent = %q", r.URL.Path, r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/agent/setup/gemcp-tool.mjs":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`// GEMCP_TOOL_HELPER_V1
import fs from 'node:fs/promises'
export async function verifyConfiguredServer(configPath, serverName) {
  const config = JSON.parse(await fs.readFile(configPath, 'utf8'))
  const server = config.mcpServers?.[serverName]
  if (!server?.bearerToken?.startsWith('gmc_')) throw new Error('missing test credential')
  return { toolCount: 28, checks: ['tools', 'guide', 'options', 'cost'] }
}
`))
		case "/api/v1/agent-enrollments/claim":
			claims.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"enrollment_id": "enrollment-test", "project_id": "project-test", "project_name": "Research",
				"server_name": "gemcp-research", "scopes": []string{"read", "submit", "cancel"}, "agent_token": "gmc_test_install_secret",
				"pi_config": map[string]any{
					"type": "http", "url": serverURL + "/mcp", "auth": "bearer",
					"bearerToken": "gmc_test_install_secret", "lifecycle": "lazy", "exposeResources": true,
					"directTools": []string{"get_usage_guide", "list_repository_registrations", "register_repository", "verify_repository", "list_workspace_datasets", "register_workspace_dataset", "remove_workspace_dataset", "list_dataset_bindings", "register_dataset_binding", "remove_dataset_binding", "register_environment", "remove_environment", "get_research_workspace", "update_research_workspace", "get_next_actions", "close_run", "report_agent_activity", "prepare_experiment", "submit_prepared_experiment", "get_project_options", "get_project_cost", "submit_experiment", "get_experiment", "list_experiments", "cancel_experiment", "list_artifacts", "register_ssh_cloud_node", "rotate_ssh_cloud_node_credential"},
				},
			}})
		case "/api/v1/agent-enrollments/complete":
			if completions.Add(1) == 1 {
				http.Error(w, "simulated lost response", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"enrollment": map[string]any{"id": "enrollment-test", "status": "completed"},
				"token":      map[string]any{"id": "token-test", "prefix": "gmc_test", "status": "active", "scopes": []string{"read", "submit", "cancel"}, "expires_at": "2026-10-01T00:00:00Z"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	agentDir := t.TempDir()
	adapterDir := filepath.Join(agentDir, "npm", "node_modules", "pi-mcp-adapter")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adapterDir, "package.json"), []byte(`{"version":"2.10.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	initialConfig := `{"mcpServers":{"existing":{"type":"stdio","command":"existing"}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	installer := PiSetupInstaller(server.URL)
	installerPath := filepath.Join(t.TempDir(), "install.mjs")
	if err := os.WriteFile(installerPath, []byte(installer), 0o700); err != nil {
		t.Fatal(err)
	}
	code := "gme_test_1234567890abcdefghijklmnopqrstuvwxyz"
	setupURL := server.URL + "/agent/setup#code=" + code
	run := func() (string, error) {
		command := exec.Command(node, installerPath, setupURL)
		command.Env = append(os.Environ(), "PI_CODING_AGENT_DIR="+agentDir)
		output, runErr := command.CombinedOutput()
		return string(output), runErr
	}

	lockPath := filepath.Join(agentDir, "mcp.json.gemcp-setup.lock")
	liveLock := []byte(fmt.Sprintf(`{"pid":%d,"created_at":"2026-07-18T00:00:00Z"}`, os.Getpid()))
	if err := os.WriteFile(lockPath, liveLock, 0o600); err != nil {
		t.Fatal(err)
	}
	lockedOutput, err := run()
	if err == nil || !strings.Contains(lockedOutput, "Another Gemcp setup") || claims.Load() != 0 {
		t.Fatalf("live-lock installer output=%q claims=%d err=%v", lockedOutput, claims.Load(), err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"pid":99999999,"created_at":"2026-07-18T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	firstOutput, err := run()
	if err == nil || !strings.Contains(firstOutput, "setup completion") {
		t.Fatalf("first installer run output=%q err=%v", firstOutput, err)
	}
	if claims.Load() != 1 || completions.Load() != 1 {
		t.Fatalf("first run calls: claims=%d completions=%d", claims.Load(), completions.Load())
	}
	receipts, err := filepath.Glob(filepath.Join(agentDir, "gemcp", "setup-*.json"))
	if err != nil || len(receipts) != 1 {
		t.Fatalf("verified receipts=%v err=%v", receipts, err)
	}
	verified, err := os.ReadFile(receipts[0])
	if err != nil || !strings.Contains(string(verified), `"status": "verified"`) {
		t.Fatalf("verified receipt=%q err=%v", verified, err)
	}
	if strings.Contains(string(verified), code) || strings.Contains(string(verified), "gmc_test_install_secret") {
		t.Fatal("verified receipt contains a setup or Agent secret")
	}

	secondOutput, err := run()
	if err != nil || !strings.Contains(secondOutput, "GEMCP_PI_SETUP_OK") {
		t.Fatalf("resumed installer output=%q err=%v", secondOutput, err)
	}
	if claims.Load() != 1 || completions.Load() != 2 {
		t.Fatalf("resumed run calls: claims=%d completions=%d", claims.Load(), completions.Load())
	}
	thirdOutput, err := run()
	if err != nil || !strings.Contains(thirdOutput, "already_completed=true") {
		t.Fatalf("repeated installer output=%q err=%v", thirdOutput, err)
	}
	if claims.Load() != 1 || completions.Load() != 2 {
		t.Fatalf("repeated run made API calls: claims=%d completions=%d", claims.Load(), completions.Load())
	}

	configBytes, err := os.ReadFile(filepath.Join(agentDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.MCPServers) != 2 || config.MCPServers["existing"] == nil || config.MCPServers["gemcp-research"] == nil {
		t.Fatalf("merged config=%s", configBytes)
	}
	for _, file := range []struct {
		path string
		mode os.FileMode
	}{
		{path: filepath.Join(agentDir, "mcp.json"), mode: 0o600},
		{path: receipts[0], mode: 0o600},
		{path: filepath.Join(agentDir, "gemcp"), mode: 0o700},
		{path: filepath.Join(agentDir, "gemcp", "gemcp-tool.mjs"), mode: 0o700},
	} {
		info, statErr := os.Stat(file.path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != file.mode {
			t.Fatalf("%s mode=%v, want %v", file.path, info.Mode().Perm(), file.mode)
		}
	}
	finalReceipt, err := os.ReadFile(receipts[0])
	if err != nil || !strings.Contains(string(finalReceipt), `"status": "completed"`) {
		t.Fatalf("completed receipt=%q err=%v", finalReceipt, err)
	}
	if strings.Contains(string(finalReceipt), code) || strings.Contains(string(finalReceipt), "gmc_test_install_secret") {
		t.Fatal("completed receipt contains a setup or Agent secret")
	}
}
