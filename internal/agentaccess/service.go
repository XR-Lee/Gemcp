package agentaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/agenttoken"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
)

const (
	maxActiveTokensPerProject = 50
	maxListedTokens           = 200
)

var (
	ErrNotFound             = errors.New("Agent token or project not found")
	ErrProjectArchived      = errors.New("project is archived")
	ErrPublicURLUnavailable = errors.New("MCP public URL is unavailable")
	ErrActiveTokenLimit     = errors.New("active Agent token limit reached")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

type IssueInput struct {
	Label         string   `json:"label"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays *int     `json:"expires_in_days"`
	NeverExpires  bool     `json:"never_expires"`
}

type View struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	Label      string     `json:"label"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type MCPServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

type ListResult struct {
	Tokens         []View     `json:"tokens"`
	MCPURL         string     `json:"mcp_url,omitempty"`
	ConfigTemplate *MCPConfig `json:"config_template,omitempty"`
	ConfigFileName string     `json:"config_file_name"`
	Truncated      bool       `json:"truncated,omitempty"`
}

type IssueResult struct {
	Token          View      `json:"token"`
	AgentToken     string    `json:"agent_token"`
	MCPURL         string    `json:"mcp_url"`
	MCPConfig      MCPConfig `json:"mcp_config"`
	ConfigFileName string    `json:"config_file_name"`
}

type Service struct {
	client *ent.Client
	box    *secrets.Box
	mcpURL string
	now    func() time.Time
}

func NewService(client *ent.Client, box *secrets.Box, publicURL string) *Service {
	return &Service{client: client, box: box, mcpURL: canonicalMCPURL(publicURL), now: time.Now}
}

func (s *Service) List(ctx context.Context, tenantID int, projectPublicID string) (ListResult, error) {
	var result ListResult
	projectRecord, err := s.findProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return result, err
	}
	records, err := s.client.AgentToken.Query().
		Where(agenttoken.ProjectIDEQ(projectRecord.ID)).
		Order(ent.Desc(agenttoken.FieldCreatedAt)).
		Limit(maxListedTokens + 1).
		All(ctx)
	if err != nil {
		return result, fmt.Errorf("list Agent tokens: %w", err)
	}
	now := s.now().UTC()
	if len(records) > maxListedTokens {
		result.Truncated = true
		records = records[:maxListedTokens]
	}
	result.Tokens = make([]View, 0, len(records))
	for _, record := range records {
		result.Tokens = append(result.Tokens, makeView(record, projectRecord.PublicID.String(), now))
	}
	result.MCPURL = s.mcpURL
	result.ConfigFileName = configFileName(projectRecord.Slug)
	if s.mcpURL != "" {
		config := makeMCPConfig(projectRecord.Slug, s.mcpURL, "${GEMCP_AGENT_TOKEN}")
		result.ConfigTemplate = &config
	}
	return result, nil
}

func (s *Service) Issue(ctx context.Context, tenantID int, actorID, projectPublicID string, input IssueInput) (IssueResult, error) {
	var result IssueResult
	if s == nil || s.client == nil || s.box == nil {
		return result, fmt.Errorf("Agent access service is not initialized")
	}
	if s.mcpURL == "" {
		return result, ErrPublicURLUnavailable
	}
	label, scopes, expiresInDays, err := validateIssueInput(input)
	if err != nil {
		return result, err
	}
	rawToken, prefix, err := secrets.RandomToken("gmc", 32)
	if err != nil {
		return result, fmt.Errorf("generate Agent token: %w", err)
	}
	now := s.now().UTC()

	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin Agent token transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	projectRecord, err := findProject(ctx, tx.Client(), tenantID, projectPublicID)
	if err != nil {
		return result, err
	}
	if projectRecord.Status == project.StatusArchived {
		return result, ErrProjectArchived
	}
	activeCount, err := tx.AgentToken.Query().Where(
		agenttoken.ProjectIDEQ(projectRecord.ID),
		agenttoken.StatusEQ(agenttoken.StatusActive),
		agenttoken.Or(agenttoken.ExpiresAtIsNil(), agenttoken.ExpiresAtGT(now)),
	).Count(ctx)
	if err != nil {
		return result, fmt.Errorf("count active Agent tokens: %w", err)
	}
	if activeCount >= maxActiveTokensPerProject {
		return result, ErrActiveTokenLimit
	}

	create := tx.AgentToken.Create().
		SetProjectID(projectRecord.ID).
		SetLabel(label).
		SetPrefix(prefix).
		SetTokenHash(s.box.Digest("agent-token", rawToken)).
		SetScopes(scopes)
	var expiresAt *time.Time
	if expiresInDays != nil {
		value := now.Add(time.Duration(*expiresInDays) * 24 * time.Hour)
		expiresAt = &value
		create.SetExpiresAt(value)
	}
	record, err := create.Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create Agent token: %w", err)
	}
	metadata := map[string]any{"project_id": projectRecord.PublicID.String(), "label": label, "prefix": prefix, "scopes": scopes}
	if expiresAt != nil {
		metadata["expires_at"] = expiresAt.Format(time.RFC3339)
	} else {
		metadata["never_expires"] = true
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType("user").
		SetActorID(strings.TrimSpace(actorID)).
		SetAction("agent_token.issued").
		SetTargetType("agent_token").
		SetTargetID(record.PublicID.String()).
		SetMetadata(metadata).
		Save(ctx); err != nil {
		return result, fmt.Errorf("write Agent token audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit Agent token transaction: %w", err)
	}

	result = IssueResult{
		Token:          makeView(record, projectRecord.PublicID.String(), now),
		AgentToken:     rawToken,
		MCPURL:         s.mcpURL,
		MCPConfig:      makeMCPConfig(projectRecord.Slug, s.mcpURL, rawToken),
		ConfigFileName: configFileName(projectRecord.Slug),
	}
	return result, nil
}

func (s *Service) Revoke(ctx context.Context, tenantID int, actorID, projectPublicID, tokenPublicID string) (View, error) {
	var view View
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return view, fmt.Errorf("begin Agent token transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	projectRecord, err := findProject(ctx, tx.Client(), tenantID, projectPublicID)
	if err != nil {
		return view, err
	}
	tokenID, err := uuid.Parse(strings.TrimSpace(tokenPublicID))
	if err != nil {
		return view, ErrNotFound
	}
	record, err := tx.AgentToken.Query().Where(
		agenttoken.PublicIDEQ(tokenID),
		agenttoken.ProjectIDEQ(projectRecord.ID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return view, ErrNotFound
	}
	if err != nil {
		return view, fmt.Errorf("find Agent token: %w", err)
	}
	if record.Status != agenttoken.StatusRevoked {
		record, err = record.Update().SetStatus(agenttoken.StatusRevoked).Save(ctx)
		if err != nil {
			return view, fmt.Errorf("revoke Agent token: %w", err)
		}
		if _, err := tx.AuditEvent.Create().
			SetTenantID(tenantID).
			SetActorType("user").
			SetActorID(strings.TrimSpace(actorID)).
			SetAction("agent_token.revoked").
			SetTargetType("agent_token").
			SetTargetID(record.PublicID.String()).
			SetMetadata(map[string]any{"project_id": projectRecord.PublicID.String(), "label": record.Label, "prefix": record.Prefix}).
			Save(ctx); err != nil {
			return view, fmt.Errorf("write Agent token audit event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return view, fmt.Errorf("commit Agent token transaction: %w", err)
	}
	return makeView(record, projectRecord.PublicID.String(), s.now().UTC()), nil
}

func (s *Service) findProject(ctx context.Context, tenantID int, value string) (*ent.Project, error) {
	return findProject(ctx, s.client, tenantID, value)
}

func findProject(ctx context.Context, client *ent.Client, tenantID int, value string) (*ent.Project, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, ErrNotFound
	}
	record, err := client.Project.Query().Where(project.PublicIDEQ(publicID), project.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	return record, nil
}

func validateIssueInput(input IssueInput) (string, []string, *int, error) {
	label := strings.TrimSpace(input.Label)
	if label == "" || len(label) > 120 {
		return "", nil, nil, invalid("label is required and must not exceed 120 characters")
	}
	if input.NeverExpires && input.ExpiresInDays != nil {
		return "", nil, nil, invalid("never_expires and expires_in_days cannot both be set")
	}
	expiresInDays := input.ExpiresInDays
	if !input.NeverExpires && expiresInDays == nil {
		defaultDays := 90
		expiresInDays = &defaultDays
	}
	if expiresInDays != nil && (*expiresInDays < 1 || *expiresInDays > 3650) {
		return "", nil, nil, invalid("expires_in_days must be between 1 and 3650")
	}

	requested := input.Scopes
	if requested == nil {
		requested = []string{"read", "submit", "cancel"}
	}
	if len(requested) == 0 {
		return "", nil, nil, invalid("at least one scope is required")
	}
	seen := make(map[string]bool, len(requested))
	for _, value := range requested {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "read" && value != "submit" && value != "cancel" {
			return "", nil, nil, invalid("scopes may contain only read, submit, and cancel")
		}
		if seen[value] {
			return "", nil, nil, invalid("scopes must not contain duplicates")
		}
		seen[value] = true
	}
	scopes := make([]string, 0, len(seen))
	for _, value := range []string{"read", "submit", "cancel"} {
		if seen[value] {
			scopes = append(scopes, value)
		}
	}
	return label, scopes, expiresInDays, nil
}

func makeView(record *ent.AgentToken, projectID string, now time.Time) View {
	status := string(record.Status)
	if record.Status == agenttoken.StatusActive && record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
		status = "expired"
	}
	return View{
		ID: record.PublicID.String(), ProjectID: projectID, Label: record.Label, Prefix: record.Prefix,
		Scopes: append([]string(nil), record.Scopes...), Status: status, ExpiresAt: record.ExpiresAt,
		LastUsedAt: record.LastUsedAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func makeMCPConfig(projectSlug, mcpURL, token string) MCPConfig {
	return MCPConfig{MCPServers: map[string]MCPServerConfig{
		serverName(projectSlug): {
			Type: "http", URL: mcpURL,
			Headers: map[string]string{"Authorization": "Bearer " + token},
		},
	}}
}

func serverName(projectSlug string) string {
	projectSlug = strings.Trim(strings.ToLower(strings.TrimSpace(projectSlug)), "-")
	if projectSlug == "" {
		return "gemcp"
	}
	return "gemcp-" + projectSlug
}

func configFileName(projectSlug string) string {
	return serverName(projectSlug) + "-mcp.json"
}

func canonicalMCPURL(publicURL string) string {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(publicURL), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return ""
	}
	return strings.TrimRight(parsed.String(), "/") + "/mcp"
}
