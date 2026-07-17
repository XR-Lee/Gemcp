package agentaccess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

type fixture struct {
	client  *ent.Client
	box     *secrets.Box
	tenant  *ent.Tenant
	project *ent.Project
	service *Service
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:agent-access?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
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
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	service := NewService(client, box, "https://gemcp.example.com/")
	service.now = func() time.Time { return now }
	return &fixture{client: client, box: box, tenant: tenant, project: project, service: service, now: now}
}

func TestIssueListAuthenticateAndRevoke(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	days := 30
	issued, err := f.service.Issue(ctx, f.tenant.ID, "owner-id", f.project.PublicID.String(), IssueInput{
		Label: "training-agent", Scopes: []string{"cancel", "read", "submit"}, ExpiresInDays: &days,
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !strings.HasPrefix(issued.AgentToken, issued.Token.Prefix+"_") || issued.Token.Status != "active" {
		t.Fatalf("issued token = %+v", issued)
	}
	if issued.Token.ExpiresAt == nil || !issued.Token.ExpiresAt.Equal(f.now.Add(30*24*time.Hour)) {
		t.Fatalf("expires_at = %v", issued.Token.ExpiresAt)
	}
	server, ok := issued.MCPConfig.MCPServers["gemcp-research"]
	if !ok || server.Type != "http" || server.URL != "https://gemcp.example.com/mcp" || server.Headers["Authorization"] != "Bearer "+issued.AgentToken {
		t.Fatalf("MCP config = %+v", issued.MCPConfig)
	}
	if issued.ConfigFileName != "gemcp-research-mcp.json" {
		t.Fatalf("config filename = %q", issued.ConfigFileName)
	}

	stored, err := f.client.AgentToken.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.TokenHash) == issued.AgentToken || len(stored.TokenHash) != 32 {
		t.Fatal("Agent token was not stored as a fixed-length digest")
	}
	principal, err := agentauth.NewService(f.client, f.box).Authenticate(ctx, issued.AgentToken)
	if err != nil || principal.ProjectPublicID != f.project.PublicID.String() || !principal.HasScope("submit") {
		t.Fatalf("Authenticate() principal=%+v err=%v", principal, err)
	}

	listed, err := f.service.List(ctx, f.tenant.ID, f.project.PublicID.String())
	if err != nil || len(listed.Tokens) != 1 || listed.MCPURL != "https://gemcp.example.com/mcp" || listed.ConfigTemplate == nil {
		t.Fatalf("List() result=%+v err=%v", listed, err)
	}
	template := listed.ConfigTemplate.MCPServers["gemcp-research"]
	if template.Headers["Authorization"] != "Bearer ${GEMCP_AGENT_TOKEN}" {
		t.Fatalf("template = %+v", template)
	}
	encoded, _ := json.Marshal(listed)
	if strings.Contains(string(encoded), issued.AgentToken) {
		t.Fatal("list response contains the plaintext Agent token")
	}

	revoked, err := f.service.Revoke(ctx, f.tenant.ID, "owner-id", f.project.PublicID.String(), issued.Token.ID)
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("Revoke() view=%+v err=%v", revoked, err)
	}
	if _, err := agentauth.NewService(f.client, f.box).Authenticate(ctx, issued.AgentToken); !errors.Is(err, agentauth.ErrInvalidToken) {
		t.Fatalf("revoked Authenticate() error = %v", err)
	}
	if _, err := f.service.Revoke(ctx, f.tenant.ID, "owner-id", f.project.PublicID.String(), issued.Token.ID); err != nil {
		t.Fatalf("idempotent Revoke() error = %v", err)
	}
	audits, err := f.client.AuditEvent.Query().All(ctx)
	if err != nil || len(audits) != 2 || audits[0].Action != "agent_token.issued" || audits[1].Action != "agent_token.revoked" {
		t.Fatalf("audit events = %+v, err=%v", audits, err)
	}
	for _, audit := range audits {
		metadata, _ := json.Marshal(audit.Metadata)
		if strings.Contains(string(metadata), issued.AgentToken) {
			t.Fatal("audit metadata contains the plaintext Agent token")
		}
	}
}

func TestValidationIsolationAndEffectiveExpiry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	invalidInputs := []IssueInput{
		{Label: ""},
		{Label: "agent", Scopes: []string{}},
		{Label: "agent", Scopes: []string{"admin"}},
		{Label: "agent", Scopes: []string{"read", "read"}},
		{Label: "agent", NeverExpires: true, ExpiresInDays: intPointer(30)},
		{Label: "agent", ExpiresInDays: intPointer(0)},
	}
	for _, input := range invalidInputs {
		if _, err := f.service.Issue(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), input); err == nil {
			t.Fatalf("Issue(%+v) succeeded", input)
		}
	}

	otherTenant, _ := f.client.Tenant.Create().SetName("Other").Save(ctx)
	if _, err := f.service.List(ctx, otherTenant.ID, f.project.PublicID.String()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant List() error = %v", err)
	}
	unconfigured := NewService(f.client, f.box, "http://gemcp.example.com")
	if _, err := unconfigured.Issue(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), IssueInput{Label: "agent"}); !errors.Is(err, ErrPublicURLUnavailable) {
		t.Fatalf("unconfigured Issue() error = %v", err)
	}

	raw, prefix, _ := secrets.RandomToken("gmc", 32)
	_, err := f.client.AgentToken.Create().
		SetProjectID(f.project.ID).
		SetLabel("expired").
		SetPrefix(prefix).
		SetTokenHash(f.box.Digest("agent-token", raw)).
		SetExpiresAt(f.now.Add(-time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := f.service.List(ctx, f.tenant.ID, f.project.PublicID.String())
	if err != nil || len(listed.Tokens) != 1 || listed.Tokens[0].Status != "expired" {
		t.Fatalf("expired List() result=%+v err=%v", listed, err)
	}

	defaulted, err := f.service.Issue(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), IssueInput{Label: "default-policy"})
	if err != nil || defaulted.Token.ExpiresAt == nil || !defaulted.Token.ExpiresAt.Equal(f.now.Add(90*24*time.Hour)) || strings.Join(defaulted.Token.Scopes, ",") != "read,submit,cancel" {
		t.Fatalf("default Issue() result=%+v err=%v", defaulted, err)
	}
	issued, err := f.service.Issue(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), IssueInput{Label: "long-lived", NeverExpires: true})
	if err != nil || issued.Token.ExpiresAt != nil {
		t.Fatalf("never-expiring Issue() result=%+v err=%v", issued, err)
	}
	if err := f.project.Update().SetStatus("archived").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.List(ctx, f.tenant.ID, f.project.PublicID.String()); err != nil {
		t.Fatalf("archived List() error = %v", err)
	}
	if _, err := f.service.Issue(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), IssueInput{Label: "blocked"}); !errors.Is(err, ErrProjectArchived) {
		t.Fatalf("archived Issue() error = %v", err)
	}
	if revoked, err := f.service.Revoke(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), issued.Token.ID); err != nil || revoked.Status != "revoked" {
		t.Fatalf("archived Revoke() result=%+v err=%v", revoked, err)
	}
}

func TestActiveLimitAndBoundedList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for index := 0; index < maxActiveTokensPerProject; index++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("active-%d", index)))
		_, err := f.client.AgentToken.Create().
			SetProjectID(f.project.ID).
			SetLabel(fmt.Sprintf("active-%d", index)).
			SetPrefix(fmt.Sprintf("gmc_%07d", index)).
			SetTokenHash(digest[:]).
			Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Issue(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), IssueInput{Label: "over-limit"}); !errors.Is(err, ErrActiveTokenLimit) {
		t.Fatalf("Issue() at active limit error = %v", err)
	}
	for index := 0; index <= maxListedTokens-maxActiveTokensPerProject; index++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("revoked-%d", index)))
		_, err := f.client.AgentToken.Create().
			SetProjectID(f.project.ID).
			SetLabel(fmt.Sprintf("revoked-%d", index)).
			SetPrefix(fmt.Sprintf("gmc_r%06d", index)).
			SetTokenHash(digest[:]).
			SetStatus("revoked").
			Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	listed, err := f.service.List(ctx, f.tenant.ID, f.project.PublicID.String())
	if err != nil || len(listed.Tokens) != maxListedTokens || !listed.Truncated {
		t.Fatalf("bounded List() count=%d truncated=%t err=%v", len(listed.Tokens), listed.Truncated, err)
	}
}

func intPointer(value int) *int { return &value }
