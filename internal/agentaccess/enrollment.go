package agentaccess

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/agentenrollment"
	"github.com/XR-Lee/Gemcp/ent/agenttoken"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
)

const (
	maxActiveEnrollmentsPerProject = 20
	maxListedEnrollments           = 50
	defaultSetupExpiresInMinutes   = 30
	minSetupExpiresInMinutes       = 5
	maxSetupExpiresInMinutes       = 24 * 60
)

var (
	ErrEnrollmentInvalid = errors.New("Agent setup link is invalid, expired, or already completed")
	ErrEnrollmentLimit   = errors.New("active Agent setup link limit reached")
	ErrEnrollmentState   = errors.New("Agent setup link cannot be changed in its current state")
)

var piDirectTools = []string{
	"get_usage_guide",
	"list_repository_registrations",
	"register_repository",
	"verify_repository",
	"list_workspace_datasets",
	"register_workspace_dataset",
	"remove_workspace_dataset",
	"get_research_workspace",
	"update_research_workspace",
	"get_next_actions",
	"close_run",
	"report_agent_activity",
	"prepare_experiment",
	"submit_prepared_experiment",
	"get_project_options",
	"get_project_cost",
	"submit_experiment",
	"get_experiment",
	"list_experiments",
	"cancel_experiment",
	"list_artifacts",
}

type EnrollmentIssueInput struct {
	Label                 string   `json:"label"`
	Scopes                []string `json:"scopes"`
	ExpiresInDays         *int     `json:"expires_in_days"`
	NeverExpires          bool     `json:"never_expires"`
	SetupExpiresInMinutes *int     `json:"setup_expires_in_minutes"`
}

type EnrollmentView struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"project_id"`
	Label              string     `json:"label"`
	Scopes             []string   `json:"scopes"`
	Status             string     `json:"status"`
	ExpiresAt          time.Time  `json:"expires_at"`
	TokenExpiresInDays *int       `json:"token_expires_in_days,omitempty"`
	ClaimedAt          *time.Time `json:"claimed_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	AgentTokenID       string     `json:"agent_token_id,omitempty"`
	AgentTokenPrefix   string     `json:"agent_token_prefix,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type EnrollmentIssueResult struct {
	Enrollment   EnrollmentView `json:"enrollment"`
	SetupURL     string         `json:"setup_url"`
	InstallerURL string         `json:"installer_url"`
}

type PiMCPServerConfig struct {
	URL             string   `json:"url"`
	Auth            string   `json:"auth"`
	BearerToken     string   `json:"bearerToken"`
	Lifecycle       string   `json:"lifecycle"`
	ExposeResources bool     `json:"exposeResources"`
	DirectTools     []string `json:"directTools"`
}

type EnrollmentClaimResult struct {
	EnrollmentID string            `json:"enrollment_id"`
	ProjectID    string            `json:"project_id"`
	ProjectName  string            `json:"project_name"`
	ServerName   string            `json:"server_name"`
	Scopes       []string          `json:"scopes"`
	AgentToken   string            `json:"agent_token"`
	MCPURL       string            `json:"mcp_url"`
	GuideURL     string            `json:"guide_url"`
	PiConfig     PiMCPServerConfig `json:"pi_config"`
	ExpiresAt    time.Time         `json:"setup_expires_at"`
}

type EnrollmentCompleteInput struct {
	Client    string   `json:"client"`
	ToolCount int      `json:"tool_count"`
	Checks    []string `json:"checks"`
}

type EnrollmentCompleteResult struct {
	Enrollment EnrollmentView `json:"enrollment"`
	Token      View           `json:"token"`
}

func (s *Service) listEnrollments(ctx context.Context, projectRecord *ent.Project, now time.Time) ([]EnrollmentView, bool, error) {
	records, err := s.client.AgentEnrollment.Query().
		Where(agentenrollment.ProjectIDEQ(projectRecord.ID)).
		WithAgentToken().
		Order(ent.Desc(agentenrollment.FieldCreatedAt)).
		Limit(maxListedEnrollments + 1).
		All(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("list Agent setup links: %w", err)
	}
	truncated := len(records) > maxListedEnrollments
	if truncated {
		records = records[:maxListedEnrollments]
	}
	views := make([]EnrollmentView, 0, len(records))
	for _, record := range records {
		views = append(views, makeEnrollmentView(record, projectRecord.PublicID.String(), now))
	}
	return views, truncated, nil
}

func (s *Service) IssueEnrollment(ctx context.Context, tenantID int, actorID, projectPublicID string, input EnrollmentIssueInput) (EnrollmentIssueResult, error) {
	var result EnrollmentIssueResult
	if s == nil || s.client == nil || s.box == nil {
		return result, fmt.Errorf("Agent access service is not initialized")
	}
	if s.publicURL == "" || s.mcpURL == "" {
		return result, ErrPublicURLUnavailable
	}
	label, scopes, tokenExpiresInDays, err := validateIssueInput(IssueInput{
		Label: input.Label, Scopes: input.Scopes, ExpiresInDays: input.ExpiresInDays, NeverExpires: input.NeverExpires,
	})
	if err != nil {
		return result, err
	}
	if !containsScope(scopes, "read") {
		return result, invalid("Pi setup link scopes must include read")
	}
	setupMinutes := defaultSetupExpiresInMinutes
	if input.SetupExpiresInMinutes != nil {
		setupMinutes = *input.SetupExpiresInMinutes
	}
	if setupMinutes < minSetupExpiresInMinutes || setupMinutes > maxSetupExpiresInMinutes {
		return result, invalid("setup_expires_in_minutes must be between 5 and 1440")
	}
	rawCode, _, err := secrets.RandomToken("gme", 32)
	if err != nil {
		return result, fmt.Errorf("generate Agent setup code: %w", err)
	}
	now := s.now().UTC()
	setupExpiresAt := now.Add(time.Duration(setupMinutes) * time.Minute)

	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin Agent setup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	projectRecord, err := findProject(ctx, tx.Client(), tenantID, projectPublicID)
	if err != nil {
		return result, err
	}
	if projectRecord.Status == project.StatusArchived {
		return result, ErrProjectArchived
	}
	activeTokens, err := countActiveTokens(ctx, tx.Client(), projectRecord.ID, now)
	if err != nil {
		return result, err
	}
	if activeTokens >= maxActiveTokensPerProject {
		return result, ErrActiveTokenLimit
	}
	activeEnrollments, err := tx.AgentEnrollment.Query().Where(
		agentenrollment.ProjectIDEQ(projectRecord.ID),
		agentenrollment.StatusIn(agentenrollment.StatusPending, agentenrollment.StatusClaimed),
		agentenrollment.ExpiresAtGT(now),
	).Count(ctx)
	if err != nil {
		return result, fmt.Errorf("count active Agent setup links: %w", err)
	}
	if activeEnrollments >= maxActiveEnrollmentsPerProject {
		return result, ErrEnrollmentLimit
	}

	create := tx.AgentEnrollment.Create().
		SetProjectID(projectRecord.ID).
		SetLabel(label).
		SetCodeHash(s.box.Digest("agent-enrollment", rawCode)).
		SetScopes(scopes).
		SetExpiresAt(setupExpiresAt).
		SetNillableTokenExpiresInDays(tokenExpiresInDays)
	record, err := create.Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create Agent setup link: %w", err)
	}
	metadata := map[string]any{
		"project_id": projectRecord.PublicID.String(), "label": label, "scopes": scopes,
		"setup_expires_at": setupExpiresAt.Format(time.RFC3339),
	}
	if tokenExpiresInDays == nil {
		metadata["token_never_expires"] = true
	} else {
		metadata["token_expires_in_days"] = *tokenExpiresInDays
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType("user").
		SetActorID(strings.TrimSpace(actorID)).
		SetAction("agent_enrollment.issued").
		SetTargetType("agent_enrollment").
		SetTargetID(record.PublicID.String()).
		SetMetadata(metadata).
		Save(ctx); err != nil {
		return result, fmt.Errorf("write Agent setup audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit Agent setup transaction: %w", err)
	}

	fragment := url.Values{"code": []string{rawCode}}.Encode()
	result = EnrollmentIssueResult{
		Enrollment:   makeEnrollmentView(record, projectRecord.PublicID.String(), now),
		SetupURL:     s.publicURL + "/agent/setup#" + fragment,
		InstallerURL: s.publicURL + "/agent/setup/install.mjs",
	}
	return result, nil
}

func (s *Service) ClaimEnrollment(ctx context.Context, code string) (EnrollmentClaimResult, error) {
	var result EnrollmentClaimResult
	if s == nil || s.client == nil || s.box == nil || s.publicURL == "" || s.mcpURL == "" {
		return result, ErrEnrollmentInvalid
	}
	code = strings.TrimSpace(code)
	if len(code) < 32 || len(code) > 160 || !strings.HasPrefix(code, "gme_") {
		return result, ErrEnrollmentInvalid
	}
	now := s.now().UTC()
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin Agent setup claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	record, err := tx.AgentEnrollment.Query().Where(
		agentenrollment.CodeHashEQ(s.box.Digest("agent-enrollment", code)),
	).WithProject(func(query *ent.ProjectQuery) { query.WithTenant() }).WithAgentToken().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrEnrollmentInvalid
	}
	if err != nil {
		return result, fmt.Errorf("find Agent setup link: %w", err)
	}
	projectRecord, err := record.Edges.ProjectOrErr()
	if err != nil || projectRecord.Edges.Tenant == nil || projectRecord.Edges.Tenant.Status != "active" ||
		projectRecord.Status == project.StatusArchived || !record.ExpiresAt.After(now) {
		return result, ErrEnrollmentInvalid
	}

	rawToken, prefix := deriveEnrollmentAgentToken(s.box, code)
	var tokenRecord *ent.AgentToken
	switch record.Status {
	case agentenrollment.StatusPending:
		activeTokens, err := countActiveTokens(ctx, tx.Client(), projectRecord.ID, now)
		if err != nil {
			return result, err
		}
		if activeTokens >= maxActiveTokensPerProject {
			return result, ErrActiveTokenLimit
		}
		tokenRecord, err = tx.AgentToken.Create().
			SetProjectID(projectRecord.ID).
			SetLabel(record.Label).
			SetPrefix(prefix).
			SetTokenHash(s.box.Digest("agent-token", rawToken)).
			SetScopes([]string{"read"}).
			SetExpiresAt(record.ExpiresAt).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("create claimed Agent token: %w", err)
		}
		record, err = record.Update().
			SetStatus(agentenrollment.StatusClaimed).
			SetClaimedAt(now).
			SetAgentToken(tokenRecord).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("mark Agent setup claimed: %w", err)
		}
		if err := writeClaimAudits(ctx, tx.Client(), projectRecord, record, tokenRecord, now); err != nil {
			return result, err
		}
	case agentenrollment.StatusClaimed:
		tokenRecord, err = record.Edges.AgentTokenOrErr()
		if err != nil || tokenRecord.Status != agenttoken.StatusActive || (tokenRecord.ExpiresAt != nil && !tokenRecord.ExpiresAt.After(now)) ||
			!hmac.Equal(tokenRecord.TokenHash, s.box.Digest("agent-token", rawToken)) {
			return result, ErrEnrollmentInvalid
		}
	default:
		return result, ErrEnrollmentInvalid
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit Agent setup claim: %w", err)
	}

	result = EnrollmentClaimResult{
		EnrollmentID: record.PublicID.String(), ProjectID: projectRecord.PublicID.String(), ProjectName: projectRecord.Name,
		ServerName: serverName(projectRecord.Slug), Scopes: append([]string(nil), record.Scopes...), AgentToken: rawToken, MCPURL: s.mcpURL,
		GuideURL: s.publicURL + "/docs/agent-mcp.md", ExpiresAt: record.ExpiresAt,
		PiConfig: PiMCPServerConfig{
			URL: s.mcpURL, Auth: "bearer", BearerToken: rawToken, Lifecycle: "lazy", ExposeResources: true,
			DirectTools: append([]string(nil), piDirectTools...),
		},
	}
	return result, nil
}

func (s *Service) CompleteEnrollment(ctx context.Context, code string, input EnrollmentCompleteInput) (EnrollmentCompleteResult, error) {
	var result EnrollmentCompleteResult
	verification, err := validateEnrollmentVerification(input)
	if err != nil {
		return result, err
	}
	code = strings.TrimSpace(code)
	if len(code) < 32 || len(code) > 160 || !strings.HasPrefix(code, "gme_") {
		return result, ErrEnrollmentInvalid
	}
	now := s.now().UTC()
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin Agent setup completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	record, err := tx.AgentEnrollment.Query().Where(
		agentenrollment.CodeHashEQ(s.box.Digest("agent-enrollment", code)),
	).WithProject(func(query *ent.ProjectQuery) { query.WithTenant() }).WithAgentToken().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrEnrollmentInvalid
	}
	if err != nil {
		return result, fmt.Errorf("find claimed Agent setup: %w", err)
	}
	projectRecord, err := record.Edges.ProjectOrErr()
	if err != nil || projectRecord.Edges.Tenant == nil || projectRecord.Edges.Tenant.Status != "active" || projectRecord.Status == project.StatusArchived {
		return result, ErrEnrollmentInvalid
	}
	tokenRecord, err := record.Edges.AgentTokenOrErr()
	if err != nil {
		return result, ErrEnrollmentInvalid
	}
	if record.Status == agentenrollment.StatusCompleted {
		if err := tx.Commit(); err != nil {
			return result, fmt.Errorf("commit repeated Agent setup completion: %w", err)
		}
		view := makeEnrollmentView(record, projectRecord.PublicID.String(), now)
		view.AgentTokenID = tokenRecord.PublicID.String()
		view.AgentTokenPrefix = tokenRecord.Prefix
		return EnrollmentCompleteResult{
			Enrollment: view,
			Token:      makeView(tokenRecord, projectRecord.PublicID.String(), now),
		}, nil
	}
	if record.Status != agentenrollment.StatusClaimed || !record.ExpiresAt.After(now) || tokenRecord.Status != agenttoken.StatusActive {
		return result, ErrEnrollmentInvalid
	}

	tokenUpdate := tokenRecord.Update().SetScopes(record.Scopes)
	if record.TokenExpiresInDays == nil {
		tokenUpdate.ClearExpiresAt()
	} else {
		tokenUpdate.SetExpiresAt(now.Add(time.Duration(*record.TokenExpiresInDays) * 24 * time.Hour))
	}
	tokenRecord, err = tokenUpdate.Save(ctx)
	if err != nil {
		return result, fmt.Errorf("activate installed Agent token: %w", err)
	}
	record, err = record.Update().
		SetStatus(agentenrollment.StatusCompleted).
		SetCompletedAt(now).
		SetVerification(verification).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("complete Agent setup link: %w", err)
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(projectRecord.TenantID).
		SetActorType("agent_enrollment").
		SetActorID(record.PublicID.String()).
		SetAction("agent_enrollment.completed").
		SetTargetType("agent_enrollment").
		SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectRecord.PublicID.String(), "agent_token_id": tokenRecord.PublicID.String(),
			"scopes": tokenRecord.Scopes, "client": verification["client"],
			"tool_count": verification["tool_count"], "checks": verification["checks"],
		}).Save(ctx); err != nil {
		return result, fmt.Errorf("write Agent setup completion audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit Agent setup completion: %w", err)
	}
	enrollmentView := makeEnrollmentView(record, projectRecord.PublicID.String(), now)
	enrollmentView.AgentTokenID = tokenRecord.PublicID.String()
	enrollmentView.AgentTokenPrefix = tokenRecord.Prefix
	result = EnrollmentCompleteResult{
		Enrollment: enrollmentView,
		Token:      makeView(tokenRecord, projectRecord.PublicID.String(), now),
	}
	return result, nil
}

func (s *Service) RevokeEnrollment(ctx context.Context, tenantID int, actorID, projectPublicID, enrollmentPublicID string) (EnrollmentView, error) {
	var view EnrollmentView
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return view, fmt.Errorf("begin Agent setup revocation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	projectRecord, err := findProject(ctx, tx.Client(), tenantID, projectPublicID)
	if err != nil {
		return view, err
	}
	publicID, err := uuid.Parse(strings.TrimSpace(enrollmentPublicID))
	if err != nil {
		return view, ErrNotFound
	}
	record, err := tx.AgentEnrollment.Query().Where(
		agentenrollment.PublicIDEQ(publicID), agentenrollment.ProjectIDEQ(projectRecord.ID),
	).WithAgentToken().Only(ctx)
	if ent.IsNotFound(err) {
		return view, ErrNotFound
	}
	if err != nil {
		return view, fmt.Errorf("find Agent setup link: %w", err)
	}
	if record.Status == agentenrollment.StatusCompleted {
		return view, ErrEnrollmentState
	}
	tokenRecord, _ := record.Edges.AgentTokenOrErr()
	if record.Status != agentenrollment.StatusRevoked {
		if tokenRecord != nil && tokenRecord.Status != agenttoken.StatusRevoked {
			if _, err := tokenRecord.Update().SetStatus(agenttoken.StatusRevoked).Save(ctx); err != nil {
				return view, fmt.Errorf("revoke claimed Agent token: %w", err)
			}
			if _, err := tx.AuditEvent.Create().
				SetTenantID(tenantID).
				SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
				SetAction("agent_token.revoked").SetTargetType("agent_token").SetTargetID(tokenRecord.PublicID.String()).
				SetMetadata(map[string]any{
					"project_id": projectRecord.PublicID.String(), "label": tokenRecord.Label, "prefix": tokenRecord.Prefix,
					"reason": "agent_enrollment_revoked", "agent_enrollment_id": record.PublicID.String(),
				}).Save(ctx); err != nil {
				return view, fmt.Errorf("write claimed Agent token revocation audit: %w", err)
			}
		}
		record, err = record.Update().SetStatus(agentenrollment.StatusRevoked).Save(ctx)
		if err != nil {
			return view, fmt.Errorf("revoke Agent setup link: %w", err)
		}
		if _, err := tx.AuditEvent.Create().
			SetTenantID(tenantID).
			SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
			SetAction("agent_enrollment.revoked").SetTargetType("agent_enrollment").SetTargetID(record.PublicID.String()).
			SetMetadata(map[string]any{"project_id": projectRecord.PublicID.String(), "label": record.Label}).
			Save(ctx); err != nil {
			return view, fmt.Errorf("write Agent setup revocation audit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return view, fmt.Errorf("commit Agent setup revocation: %w", err)
	}
	view = makeEnrollmentView(record, projectRecord.PublicID.String(), s.now().UTC())
	if tokenRecord != nil {
		view.AgentTokenID = tokenRecord.PublicID.String()
		view.AgentTokenPrefix = tokenRecord.Prefix
	}
	return view, nil
}

func countActiveTokens(ctx context.Context, client *ent.Client, projectID int, now time.Time) (int, error) {
	count, err := client.AgentToken.Query().Where(
		agenttoken.ProjectIDEQ(projectID), agenttoken.StatusEQ(agenttoken.StatusActive),
		agenttoken.Or(agenttoken.ExpiresAtIsNil(), agenttoken.ExpiresAtGT(now)),
	).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active Agent tokens: %w", err)
	}
	return count, nil
}

func writeClaimAudits(ctx context.Context, client *ent.Client, projectRecord *ent.Project, enrollment *ent.AgentEnrollment, tokenRecord *ent.AgentToken, now time.Time) error {
	metadata := map[string]any{
		"project_id": projectRecord.PublicID.String(), "label": tokenRecord.Label, "prefix": tokenRecord.Prefix,
		"scopes": tokenRecord.Scopes, "setup_expires_at": enrollment.ExpiresAt.Format(time.RFC3339),
	}
	if _, err := client.AuditEvent.Create().
		SetTenantID(projectRecord.TenantID).SetActorType("agent_enrollment").SetActorID(enrollment.PublicID.String()).
		SetAction("agent_token.issued").SetTargetType("agent_token").SetTargetID(tokenRecord.PublicID.String()).
		SetMetadata(metadata).Save(ctx); err != nil {
		return fmt.Errorf("write claimed Agent token audit: %w", err)
	}
	if _, err := client.AuditEvent.Create().
		SetTenantID(projectRecord.TenantID).SetActorType("agent_enrollment").SetActorID(enrollment.PublicID.String()).
		SetAction("agent_enrollment.claimed").SetTargetType("agent_enrollment").SetTargetID(enrollment.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectRecord.PublicID.String(), "agent_token_id": tokenRecord.PublicID.String(), "claimed_at": now.Format(time.RFC3339),
		}).Save(ctx); err != nil {
		return fmt.Errorf("write Agent setup claim audit: %w", err)
	}
	return nil
}

func validateEnrollmentVerification(input EnrollmentCompleteInput) (map[string]any, error) {
	client := strings.TrimSpace(input.Client)
	if client == "" || len(client) > 80 {
		return nil, invalid("client is required and must not exceed 80 characters")
	}
	if input.ToolCount < len(piDirectTools) || input.ToolCount > 100 {
		return nil, invalid("tool_count must include all Gemcp tools and must not exceed 100")
	}
	allowed := map[string]bool{"tools": true, "guide": true, "options": true, "cost": true}
	seen := make(map[string]bool, len(input.Checks))
	for _, check := range input.Checks {
		check = strings.ToLower(strings.TrimSpace(check))
		if !allowed[check] || seen[check] {
			return nil, invalid("checks must contain tools, guide, options, and cost exactly once")
		}
		seen[check] = true
	}
	for check := range allowed {
		if !seen[check] {
			return nil, invalid("checks must contain tools, guide, options, and cost exactly once")
		}
	}
	checks := []string{"tools", "guide", "options", "cost"}
	return map[string]any{"client": client, "tool_count": input.ToolCount, "checks": checks}, nil
}

func makeEnrollmentView(record *ent.AgentEnrollment, projectID string, now time.Time) EnrollmentView {
	status := string(record.Status)
	if (record.Status == agentenrollment.StatusPending || record.Status == agentenrollment.StatusClaimed) && !record.ExpiresAt.After(now) {
		status = "expired"
	}
	view := EnrollmentView{
		ID: record.PublicID.String(), ProjectID: projectID, Label: record.Label,
		Scopes: append([]string(nil), record.Scopes...), Status: status, ExpiresAt: record.ExpiresAt,
		TokenExpiresInDays: record.TokenExpiresInDays, ClaimedAt: record.ClaimedAt, CompletedAt: record.CompletedAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
	if tokenRecord, err := record.Edges.AgentTokenOrErr(); err == nil {
		view.AgentTokenID = tokenRecord.PublicID.String()
		view.AgentTokenPrefix = tokenRecord.Prefix
	}
	return view
}

func deriveEnrollmentAgentToken(box *secrets.Box, code string) (string, string) {
	material := box.Digest("agent-enrollment-agent-token", code)
	prefix := "gmc_" + base64.RawURLEncoding.EncodeToString(material[:5])
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(material), prefix
}

func containsScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}
