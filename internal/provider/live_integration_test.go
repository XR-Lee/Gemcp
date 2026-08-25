package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

func TestLivePrivateCloudResources(t *testing.T) {
	tokenFile := strings.TrimSpace(os.Getenv("GEMCP_TEST_PRIVATE_TOKEN_FILE"))
	if tokenFile == "" {
		t.Skip("GEMCP_TEST_PRIVATE_TOKEN_FILE is not set")
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read private Token file: %v", err)
	}
	token := strings.TrimSpace(string(contents))
	for index := range contents {
		contents[index] = 0
	}
	if token == "" {
		t.Fatal("private Token file is empty")
	}

	client := enttest.Open(t, dialect.SQLite, "file:provider-live?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := EncryptCredential(box, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant, err := client.Tenant.Create().SetName("Live validation").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ProviderAccount.Create().
		SetTenantID(tenant.ID).
		SetName("AutoDL Private Cloud").
		SetBaseURL(autodl.PrivateBaseURL).
		SetCredentialCiphertext(ciphertext).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := NewService(client, box, "live-test").QueryResources(ctx, tenant.ID, "")
	if err != nil {
		t.Fatalf("QueryResources() error = %v", err)
	}
	if snapshot.Provider.Backend != "private" || snapshot.Provider.Status != "active" {
		t.Fatalf("unexpected Provider state: %+v", snapshot.Provider)
	}
	if len(snapshot.SystemImages) == 0 || len(snapshot.GPUStock) == 0 {
		t.Fatalf("incomplete live snapshot: images=%d stock=%d", len(snapshot.SystemImages), len(snapshot.GPUStock))
	}
	t.Logf("live snapshot: private_images=%d system_images=%d gpu_types=%d deployments=%d active_containers=%d cached_containers=%d",
		len(snapshot.PrivateImages), len(snapshot.SystemImages), len(snapshot.GPUStock), len(snapshot.Deployments),
		len(snapshot.ActiveContainers), len(snapshot.CachedContainers))
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "credential_ciphertext") {
		t.Fatal("serialized live snapshot leaked the Provider credential")
	}
}
