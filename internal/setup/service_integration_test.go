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
