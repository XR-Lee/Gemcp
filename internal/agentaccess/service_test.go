package agentaccess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	result := &fixture{client: client, box: box, tenant: tenant, project: project, service: service, now: now}
	service.now = func() time.Time { return result.now }
	return result
}

func TestIssueListAuthenticateAndRevoke(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	days := 30
	issued, err := f.service.Issue(ctx, f.tenant.ID, "owner-id", f.project.PublicID.String(), IssueInput{
		Label: "training-agent", Scopes: []string{"configure", "cancel", "read", "submit"}, ExpiresInDays: &days,
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
	if err != nil || principal.ProjectPublicID != f.project.PublicID.String() || !principal.HasScope("submit") || !principal.HasScope("configure") || strings.Join(principal.Scopes, ",") != "read,submit,cancel,configure" {
		t.Fatalf("Authenticate() principal=%+v err=%v", principal, err)
	}
	updated, err := f.service.UpdateScopes(ctx, f.tenant.ID, "owner-id", f.project.PublicID.String(), issued.Token.ID, UpdateScopesInput{Scopes: []string{"configure", "read"}})
	if err != nil || strings.Join(updated.Scopes, ",") != "read,configure" {
		t.Fatalf("UpdateScopes() = %+v, %v", updated, err)
	}
	principal, err = agentauth.NewService(f.client, f.box).Authenticate(ctx, issued.AgentToken)
	if err != nil || !principal.HasScope("configure") || principal.HasScope("submit") {
		t.Fatalf("updated principal=%+v err=%v", principal, err)
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
	if err != nil || len(audits) != 3 || audits[0].Action != "agent_token.issued" || audits[1].Action != "agent_token.scopes_updated" || audits[2].Action != "agent_token.revoked" {
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

func TestEnrollmentDefaultSetupValidityIsFourHours(t *testing.T) {
	f := newFixture(t)
	issued, err := f.service.IssueEnrollment(
		context.Background(),
		f.tenant.ID,
		"owner-id",
		f.project.PublicID.String(),
		EnrollmentIssueInput{Label: "default-validity", Scopes: []string{"read"}},
	)
	if err != nil {
		t.Fatalf("IssueEnrollment() error = %v", err)
	}
	want := f.now.Add(4 * time.Hour)
	if !issued.Enrollment.ExpiresAt.Equal(want) {
		t.Fatalf("setup expires_at = %v, want %v", issued.Enrollment.ExpiresAt, want)
	}
}

func TestPiEnrollmentClaimCompleteAndSecretHygiene(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	setupMinutes := 15
	tokenDays := 30
	if _, err := f.service.IssueEnrollment(ctx, f.tenant.ID, "owner-id", f.project.PublicID.String(), EnrollmentIssueInput{
		Label: "write-only", Scopes: []string{"submit"}, ExpiresInDays: &tokenDays, SetupExpiresInMinutes: &setupMinutes,
	}); err == nil {
		t.Fatal("IssueEnrollment() accepted scopes without read")
	}
	issued, err := f.service.IssueEnrollment(ctx, f.tenant.ID, "owner-id", f.project.PublicID.String(), EnrollmentIssueInput{
		Label: "pi-experiment-agent", Scopes: []string{"read", "submit", "cancel"},
		ExpiresInDays: &tokenDays, SetupExpiresInMinutes: &setupMinutes,
	})
	if err != nil {
		t.Fatalf("IssueEnrollment() error = %v", err)
	}
	parsed, err := url.Parse(issued.SetupURL)
	if err != nil {
		t.Fatal(err)
	}
	code := parsed.Fragment
	values, err := url.ParseQuery(code)
	if err != nil {
		t.Fatal(err)
	}
	code = values.Get("code")
	if !strings.HasPrefix(code, "gme_") || parsed.RawQuery != "" || parsed.Path != "/agent/setup" {
		t.Fatalf("setup URL = %q", issued.SetupURL)
	}
	if issued.InstallerURL != "https://gemcp.example.com/agent/setup/install.mjs" || issued.Enrollment.Status != "pending" {
		t.Fatalf("issue result = %+v", issued)
	}
	if count, _ := f.client.AgentToken.Query().Count(ctx); count != 0 {
		t.Fatalf("Agent token count before claim = %d", count)
	}
	listed, err := f.service.List(ctx, f.tenant.ID, f.project.PublicID.String())
	if err != nil || len(listed.Enrollments) != 1 || listed.Enrollments[0].Status != "pending" {
		t.Fatalf("List() enrollments=%+v err=%v", listed.Enrollments, err)
	}
	listedJSON, _ := json.Marshal(listed)
	if strings.Contains(string(listedJSON), code) {
		t.Fatal("list response contains setup code")
	}

	claimed, err := f.service.ClaimEnrollment(ctx, code)
	if err != nil {
		t.Fatalf("ClaimEnrollment() error = %v", err)
	}
	if claimed.ProjectID != f.project.PublicID.String() || claimed.ServerName != "gemcp-research" || strings.Join(claimed.Scopes, ",") != "read,submit,cancel" || claimed.PiConfig.Auth != "bearer" || len(claimed.PiConfig.DirectTools) != 34 {
		t.Fatalf("claim result = %+v", claimed)
	}
	if claimed.PiConfig.BearerToken != claimed.AgentToken || claimed.GuideURL != "https://gemcp.example.com/docs/agent-mcp.md" {
		t.Fatalf("claim config = %+v", claimed)
	}
	claimedList, err := f.service.List(ctx, f.tenant.ID, f.project.PublicID.String())
	if err != nil {
		t.Fatal(err)
	}
	claimedListJSON, _ := json.Marshal(claimedList)
	if strings.Contains(string(claimedListJSON), code) || strings.Contains(string(claimedListJSON), claimed.AgentToken) {
		t.Fatal("claimed enrollment list contains a setup or Agent secret")
	}
	tokenRecord, err := f.client.AgentToken.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tokenRecord.ExpiresAt == nil || !tokenRecord.ExpiresAt.Equal(f.now.Add(15*time.Minute)) {
		t.Fatalf("provisional token expires_at = %v", tokenRecord.ExpiresAt)
	}
	provisionalPrincipal, err := agentauth.NewService(f.client, f.box).Authenticate(ctx, claimed.AgentToken)
	if err != nil {
		t.Fatalf("provisional token authentication error = %v", err)
	}
	if len(provisionalPrincipal.Scopes) != 1 || provisionalPrincipal.Scopes[0] != "read" {
		t.Fatalf("provisional token scopes = %v", provisionalPrincipal.Scopes)
	}
	retried, err := f.service.ClaimEnrollment(ctx, code)
	if err != nil || retried.AgentToken != claimed.AgentToken {
		t.Fatalf("idempotent claim result=%+v err=%v", retried, err)
	}
	if _, err := f.service.CompleteEnrollment(ctx, code, EnrollmentCompleteInput{
		Client: "pi-mcp-adapter/2.10.0", ToolCount: 7, Checks: []string{"tools", "guide", "options"},
	}); err == nil {
		t.Fatal("CompleteEnrollment() accepted incomplete verification")
	}

	completed, err := f.service.CompleteEnrollment(ctx, code, EnrollmentCompleteInput{
		Client: "pi-mcp-adapter/2.10.0", ToolCount: 34, Checks: []string{"tools", "guide", "options", "cost"},
	})
	if err != nil {
		t.Fatalf("CompleteEnrollment() error = %v", err)
	}
	if completed.Enrollment.Status != "completed" || completed.Enrollment.AgentTokenPrefix != completed.Token.Prefix || strings.Join(completed.Token.Scopes, ",") != "read,submit,cancel" || completed.Token.ExpiresAt == nil || !completed.Token.ExpiresAt.Equal(f.now.Add(30*24*time.Hour)) {
		t.Fatalf("complete result = %+v", completed)
	}
	retriedCompletion, err := f.service.CompleteEnrollment(ctx, code, EnrollmentCompleteInput{
		Client: "pi-mcp-adapter/2.10.0", ToolCount: 34, Checks: []string{"tools", "guide", "options", "cost"},
	})
	if err != nil || retriedCompletion.Enrollment.ID != completed.Enrollment.ID || retriedCompletion.Enrollment.AgentTokenPrefix != completed.Token.Prefix {
		t.Fatalf("idempotent completion result=%+v err=%v", retriedCompletion, err)
	}
	storedEnrollment, err := f.client.AgentEnrollment.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if storedEnrollment.Status != "completed" || storedEnrollment.Verification["client"] != "pi-mcp-adapter/2.10.0" {
		t.Fatalf("stored enrollment = %+v", storedEnrollment)
	}
	if _, err := f.service.ClaimEnrollment(ctx, code); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("claim after complete error = %v", err)
	}

	audits, err := f.client.AuditEvent.Query().All(ctx)
	if err != nil || len(audits) != 4 {
		t.Fatalf("audit events=%+v err=%v", audits, err)
	}
	for _, audit := range audits {
		encoded, _ := json.Marshal(audit.Metadata)
		if strings.Contains(string(encoded), code) || strings.Contains(string(encoded), claimed.AgentToken) {
			t.Fatal("audit metadata contains an enrollment or Agent secret")
		}
	}
}

func TestPiEnrollmentExpiryAndRevocation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	minutes := 5
	issued, err := f.service.IssueEnrollment(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), EnrollmentIssueInput{
		Label: "revocable", SetupExpiresInMinutes: &minutes,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(issued.SetupURL)
	values, _ := url.ParseQuery(parsed.Fragment)
	code := values.Get("code")
	claimed, err := f.service.ClaimEnrollment(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := f.service.RevokeEnrollment(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), issued.Enrollment.ID)
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("RevokeEnrollment() result=%+v err=%v", revoked, err)
	}
	if _, err := agentauth.NewService(f.client, f.box).Authenticate(ctx, claimed.AgentToken); !errors.Is(err, agentauth.ErrInvalidToken) {
		t.Fatalf("authentication after enrollment revocation error = %v", err)
	}
	if _, err := f.service.ClaimEnrollment(ctx, code); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("claim after revocation error = %v", err)
	}
	audits, err := f.client.AuditEvent.Query().All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actions := make(map[string]bool, len(audits))
	for _, audit := range audits {
		actions[audit.Action] = true
	}
	if !actions["agent_token.revoked"] || !actions["agent_enrollment.revoked"] {
		t.Fatalf("revocation audit actions = %v", actions)
	}

	expiring, err := f.service.IssueEnrollment(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), EnrollmentIssueInput{
		Label: "expiring", SetupExpiresInMinutes: &minutes,
	})
	if err != nil {
		t.Fatal(err)
	}
	expiringURL, _ := url.Parse(expiring.SetupURL)
	expiringValues, _ := url.ParseQuery(expiringURL.Fragment)
	f.now = f.now.Add(6 * time.Minute)
	if _, err := f.service.ClaimEnrollment(ctx, expiringValues.Get("code")); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("expired claim error = %v", err)
	}
	listed, err := f.service.List(ctx, f.tenant.ID, f.project.PublicID.String())
	if err != nil || listed.Enrollments[0].Status != "expired" {
		t.Fatalf("expired enrollment list=%+v err=%v", listed.Enrollments, err)
	}
}

func TestLoopbackHTTPPublicURLAllowsMCPExport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	service := NewService(f.client, f.box, "http://127.0.0.1:18080/")
	service.now = f.service.now
	listed, err := service.List(ctx, f.tenant.ID, f.project.PublicID.String())
	if err != nil || listed.MCPURL != "http://127.0.0.1:18080/mcp" || listed.ConfigTemplate == nil {
		t.Fatalf("List() result=%+v err=%v", listed, err)
	}
	issued, err := service.Issue(ctx, f.tenant.ID, "owner", f.project.PublicID.String(), IssueInput{Label: "local-loopback"})
	if err != nil || issued.MCPURL != "http://127.0.0.1:18080/mcp" {
		t.Fatalf("Issue() result=%+v err=%v", issued, err)
	}
	if canonicalPublicURL("http://localhost:18080") != "http://localhost:18080" {
		t.Fatalf("localhost loopback = %q", canonicalPublicURL("http://localhost:18080"))
	}
	if canonicalPublicURL("https://gemcp.example.com") != "https://gemcp.example.com" {
		t.Fatalf("https origin = %q", canonicalPublicURL("https://gemcp.example.com"))
	}
	if canonicalPublicURL("http://gemcp.example.com") != "" {
		t.Fatal("accepted a non-loopback HTTP origin")
	}
}

func intPointer(value int) *int { return &value }
