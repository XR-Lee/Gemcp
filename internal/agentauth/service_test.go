package agentauth

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/setup"
	_ "github.com/mattn/go-sqlite3"
)

func TestAuthenticateAndRevokeAgentToken(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:agentauth?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	result, err := setup.NewService(client, box).Initialize(context.Background(), setup.Input{
		OrganizationName: "Test",
		Owner:            setup.OwnerInput{Email: "owner@example.com", Password: "correct horse battery staple"},
		Provider:         setup.ProviderInput{Name: "AutoDL", BaseURL: "https://api.autodl.com", Token: "provider-token"},
		Project: setup.ProjectInput{
			Name: "Project", Slug: "project", MonthlyBudgetMilli: 100000, MaxExperimentMilli: 20000,
			MaxConcurrency: 1, MaxRuntimeSeconds: 3600, TimeoutExtensionSeconds: 3600, TerminationGraceSeconds: 60,
		},
		Environment: setup.EnvironmentInput{Name: "default", ImageUUID: "image-uuid"},
		ResourceProfile: setup.ResourceProfileInput{
			Name: "default", Region: "west", GPUNames: []string{"RTX 4090"}, GPUNum: 1,
			CUDAFrom: 118, CUDATo: 128, CPUFrom: 1, CPUTo: 128, MemoryFromGB: 1, MemoryToGB: 512,
			PriceFromMilli: 10, PriceToMilli: 3000,
		},
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	service := NewService(client, box)
	principal, err := service.Authenticate(context.Background(), result.AgentToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.ProjectPublicID != result.ProjectID || !principal.HasScope("submit") {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if _, err := service.Authenticate(context.Background(), "gmc_wrong-token-value"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong token error = %v", err)
	}
	token, _ := client.AgentToken.Get(context.Background(), principal.TokenID)
	if err := token.Update().SetStatus("revoked").Exec(context.Background()); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	if _, err := service.Authenticate(context.Background(), result.AgentToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked token error = %v", err)
	}
}
