package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

func TestInitializeCreatesEncryptedSingleOrganization(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:setup?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	service := NewService(client, box)

	result, err := service.Initialize(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !strings.HasPrefix(result.AgentToken, result.AgentTokenPrefix+"_") {
		t.Fatalf("unexpected Agent token prefix: %+v", result)
	}
	if initialized, err := service.Initialized(context.Background()); err != nil || !initialized {
		t.Fatalf("Initialized() = %v, %v", initialized, err)
	}
	provider, err := client.ProviderAccount.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query provider: %v", err)
	}
	if strings.Contains(provider.CredentialCiphertext, "provider-token") {
		t.Fatal("provider token was stored as plaintext")
	}
	if provider.Backend != "elastic" {
		t.Fatalf("provider backend = %q, want elastic", provider.Backend)
	}
	environment, err := client.Environment.Query().Only(context.Background())
	if err != nil || environment.Backend != "autodl_elastic" {
		t.Fatalf("environment backend = %q, %v", environment.Backend, err)
	}
	profile, err := client.ResourceProfile.Query().Only(context.Background())
	if err != nil || profile.Backend != "autodl_elastic" {
		t.Fatalf("resource profile backend = %q, %v", profile.Backend, err)
	}
	plaintext, err := box.Decrypt(provider.CredentialCiphertext, providerTokenAAD)
	if err != nil || string(plaintext) != "provider-token" {
		t.Fatalf("decrypt provider token = %q, %v", plaintext, err)
	}
	storedToken, err := client.AgentToken.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query Agent token: %v", err)
	}
	if string(storedToken.TokenHash) == result.AgentToken || len(storedToken.TokenHash) != 32 {
		t.Fatal("Agent token was not stored as a fixed-length digest")
	}
	if count, _ := client.AuditEvent.Query().Count(context.Background()); count != 1 {
		t.Fatalf("audit count = %d, want 1", count)
	}

	_, err = service.Initialize(context.Background(), validInput())
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second Initialize() error = %v", err)
	}
}

func TestInitializePrivateCloudSetsPrivateBackends(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:setup-private?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	service := NewService(client, box)

	input := validInput()
	input.Provider.Name = "AutoDL Private Cloud"
	input.Provider.BaseURL = "https://private.autodl.com"
	input.Provider.Backend = "private"
	input.ResourceProfile.Region = "private"
	input.ResourceProfile.GPUNames = []string{"NVIDIA GeForce RTX 3090"}
	input.ResourceProfile.CUDATo = input.ResourceProfile.CUDAFrom

	if _, err := service.Initialize(context.Background(), input); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	provider, err := client.ProviderAccount.Query().Only(context.Background())
	if err != nil || provider.Backend != "private" {
		t.Fatalf("provider backend = %q, %v", provider.Backend, err)
	}
	environment, err := client.Environment.Query().Only(context.Background())
	if err != nil || environment.Backend != "autodl_private" {
		t.Fatalf("environment backend = %q, %v", environment.Backend, err)
	}
	profile, err := client.ResourceProfile.Query().Only(context.Background())
	if err != nil || profile.Backend != "autodl_private" {
		t.Fatalf("resource profile backend = %q, %v", profile.Backend, err)
	}
}

func TestInitializeCanSkipAutoDL(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:setup-no-provider?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	service := NewService(client, box)

	input := validInput()
	input.SkipProvider = true
	input.Provider = ProviderInput{}
	input.Environment = EnvironmentInput{}
	input.ResourceProfile = ResourceProfileInput{}
	result, err := service.Initialize(context.Background(), input)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if result.ProviderID != "" || result.EnvironmentID != "" || result.ResourceProfileID != "" {
		t.Fatalf("unexpected AutoDL records in result: %+v", result)
	}
	if count, _ := client.ProviderAccount.Query().Count(context.Background()); count != 0 {
		t.Fatalf("provider count = %d, want 0", count)
	}
	if count, _ := client.Environment.Query().Count(context.Background()); count != 0 {
		t.Fatalf("environment count = %d, want 0", count)
	}
	if count, _ := client.ResourceProfile.Query().Count(context.Background()); count != 0 {
		t.Fatalf("resource profile count = %d, want 0", count)
	}
	if count, _ := client.Project.Query().Count(context.Background()); count != 1 {
		t.Fatalf("project count = %d, want 1", count)
	}
	if count, _ := client.AgentToken.Query().Count(context.Background()); count != 1 {
		t.Fatalf("Agent token count = %d, want 1", count)
	}
}
