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
	token := readLiveToken(t, "GEMCP_TEST_PRIVATE_TOKEN_FILE")

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

func TestLivePublicElasticResources(t *testing.T) {
	token := readLiveToken(t, "GEMCP_TEST_ELASTIC_TOKEN_FILE")
	region := strings.TrimSpace(os.Getenv("GEMCP_TEST_ELASTIC_REGION"))
	if region == "" {
		region = "westDC2"
	}

	client := enttest.Open(t, dialect.SQLite, "file:provider-live-elastic?mode=memory&cache=shared&_fk=1")
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tenant, err := client.Tenant.Create().SetName("Live Public Elastic validation").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Live Public Elastic").SetSlug("live-public-elastic").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ResourceProfile.Create().
		SetProjectID(project.ID).SetName("live-elastic").SetBackend("autodl_elastic").SetRegion(region).
		SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).
		SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
		SetPriceFromMilli(1).SetPriceToMilli(20000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ProviderAccount.Create().
		SetTenantID(tenant.ID).
		SetName("AutoDL Public Elastic").
		SetBackend("elastic").
		SetBaseURL(autodl.DefaultBaseURL).
		SetCredentialCiphertext(ciphertext).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := NewService(client, box, "live-test").QueryResources(ctx, tenant.ID, "elastic")
	if err != nil {
		t.Fatalf("QueryResources() error = %v", err)
	}
	if snapshot.Provider.Backend != "elastic" || snapshot.Provider.Status != "active" {
		t.Fatalf("unexpected Provider state: %+v", snapshot.Provider)
	}
	if snapshot.Wallet == nil || len(snapshot.PrivateImages) == 0 {
		t.Fatalf("incomplete Public Elastic snapshot: wallet=%+v private_images=%d", snapshot.Wallet, len(snapshot.PrivateImages))
	}
	foundRegion := false
	for _, stock := range snapshot.GPUStock {
		if stock.Region == region {
			foundRegion = true
			break
		}
	}
	if !foundRegion {
		t.Fatalf("Public Elastic snapshot has no GPU inventory for %q: %+v", region, snapshot.GPUStock)
	}
	if snapshot.Deployments == nil || snapshot.ActiveContainers == nil || snapshot.CachedContainers == nil {
		t.Fatalf("deployment/container collections were not queried: %+v", snapshot)
	}
	t.Logf("live snapshot: region=%s wallet_assets=%d private_images=%d gpu_types=%d deployments=%d active_containers=%d cached_containers=%d",
		region, snapshot.Wallet.Assets, len(snapshot.PrivateImages), len(snapshot.GPUStock), len(snapshot.Deployments),
		len(snapshot.ActiveContainers), len(snapshot.CachedContainers))
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "credential_ciphertext") {
		t.Fatal("serialized live snapshot leaked the Provider credential")
	}
}

func readLiveToken(t *testing.T, environmentVariable string) string {
	t.Helper()
	tokenFile := strings.TrimSpace(os.Getenv(environmentVariable))
	if tokenFile == "" {
		t.Skip(environmentVariable + " is not set")
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read Token file: %v", err)
	}
	token := strings.TrimSpace(string(contents))
	for index := range contents {
		contents[index] = 0
	}
	if token == "" {
		t.Fatal("Token file is empty")
	}
	return token
}
