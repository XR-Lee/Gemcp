package nodeagent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
)

func TestConfigSeparatesCredentialAndUsesSecureModes(t *testing.T) {
	directory := t.TempDir()
	configPath := directory + "/config.json"
	credentialPath := directory + "/credential"
	config := Config{
		ServerURL: "https://gemcp.example.com", NodeID: uuid.NewString(), InstallationID: uuid.NewString(),
		MachineFingerprint: strings.Repeat("a", 64), CredentialPath: credentialPath,
		StatePath: directory + "/state.db", StorageRoot: directory + "/storage",
	}
	token := "gmn_1234567_abcdefghijklmnopqrstuvwxyz1234567890"
	if err := SaveCredential(credentialPath, token); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(configPath, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(configPath)
	if err != nil || loaded.NodeID != config.NodeID {
		t.Fatalf("loaded config=%+v err=%v", loaded, err)
	}
	loadedToken, err := LoadCredential(credentialPath)
	if err != nil || loadedToken != token {
		t.Fatalf("loaded token=%q err=%v", loadedToken, err)
	}
	for _, filename := range []string{configPath, credentialPath} {
		info, err := os.Stat(filename)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%v err=%v", filename, info.Mode().Perm(), err)
		}
	}
	payload, _ := os.ReadFile(configPath)
	if strings.Contains(string(payload), token) {
		t.Fatal("node config contains the plaintext credential")
	}
}

func TestClientClaimAndSyncContract(t *testing.T) {
	var requests int
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("User-Agent") != "Gemcp-Node/test" {
			t.Errorf("User-Agent=%q", request.Header.Get("User-Agent"))
		}
		switch request.URL.Path {
		case "/api/v1/node-enrollments/claim":
			if request.Header.Get("Authorization") != "" {
				t.Error("claim request included authorization")
			}
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), "gne_test") {
				t.Fatalf("claim body=%s", body)
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"data": nodeprotocol.EnrollmentClaimResponse{
				EnrollmentID: uuid.NewString(), NodeID: uuid.NewString(), NodeToken: "gmn_1234567_secret", PairingCode: "ABCD-EFGH",
			}})
		case "/api/v1/nodes/sync":
			if request.Header.Get("Authorization") != "Bearer gmn_1234567_secret" {
				t.Errorf("Authorization=%q", request.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"data": nodeprotocol.SyncResponse{NodeID: uuid.NewString(), DesiredState: "active", NextSyncSeconds: 15}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	origin, code, err := OriginFromSetupURL(server.URL + "/node/setup?lang=zh#code=gne_test_code_that_is_long_enough_for_setup")
	if err != nil || origin != server.URL || !strings.HasPrefix(code, "gne_") {
		t.Fatalf("origin=%q code=%q err=%v", origin, code, err)
	}
	client, err := NewClient(origin, "test", WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := client.Claim(context.Background(), code, nodeprotocol.Inventory{})
	if err != nil || claim.PairingCode != "ABCD-EFGH" {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	syncResult, err := client.Sync(context.Background(), claim.NodeToken, nodeprotocol.SyncRequest{})
	if err != nil || syncResult.DesiredState != "active" || requests != 2 {
		t.Fatalf("sync=%+v requests=%d err=%v", syncResult, requests, err)
	}
}

func TestOriginFromSetupURLRestrictsLanguageQuery(t *testing.T) {
	for _, language := range []string{"zh", "en"} {
		origin, code, err := OriginFromSetupURL("https://gemcp.example.com/node/setup?lang=" + language + "#code=gne_test_code_that_is_long_enough")
		if err != nil || origin != "https://gemcp.example.com" || !strings.HasPrefix(code, "gne_") {
			t.Fatalf("language=%s origin=%q code=%q err=%v", language, origin, code, err)
		}
	}
	for _, raw := range []string{
		"https://gemcp.example.com/node/setup?lang=fr#code=gne_test_code_that_is_long_enough",
		"https://gemcp.example.com/node/setup?lang=ZH#code=gne_test_code_that_is_long_enough",
		"https://gemcp.example.com/node/setup?lang=en&next=bad#code=gne_test_code_that_is_long_enough",
		"https://gemcp.example.com/node/setup?next=bad#code=gne_test_code_that_is_long_enough",
	} {
		if _, _, err := OriginFromSetupURL(raw); err == nil {
			t.Fatalf("OriginFromSetupURL(%q) succeeded", raw)
		}
	}
}
