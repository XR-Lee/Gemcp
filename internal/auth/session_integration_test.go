package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

func TestSessionLifecycle(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:auth?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	hash, _ := HashPassword("correct horse battery staple")
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	_, err = client.User.Create().SetTenantID(tenant.ID).SetEmail("owner@example.com").SetPasswordHash(hash).Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := NewService(client, box, time.Hour)
	credentials, err := service.Login(ctx, "OWNER@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	principal, record, err := service.Authenticate(ctx, credentials.SessionToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.Email != "owner@example.com" || principal.TenantPublicID != tenant.PublicID.String() {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if err := service.ValidateCSRF(record, credentials.CSRFToken); err != nil {
		t.Fatalf("ValidateCSRF() error = %v", err)
	}
	if err := service.Logout(ctx, record); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	_, _, err = service.Authenticate(ctx, credentials.SessionToken)
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("Authenticate() after logout error = %v", err)
	}
}

func TestLoginSkipsPasswordWhenEnabled(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:auth-skip?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	hash, _ := HashPassword("correct horse battery staple")
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	_, err = client.User.Create().SetTenantID(tenant.ID).SetEmail("owner@example.com").SetPasswordHash(hash).Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	required := NewService(client, box, time.Hour)
	if _, err := required.Login(ctx, "owner@example.com", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("required Login() error = %v", err)
	}

	skipped := NewService(client, box, time.Hour, WithSkipPassword(true))
	credentials, err := skipped.Login(ctx, "owner@example.com", "")
	if err != nil {
		t.Fatalf("skipped Login() error = %v", err)
	}
	if credentials.User.Email != "owner@example.com" {
		t.Fatalf("skipped principal: %+v", credentials.User)
	}
}
