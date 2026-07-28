package agentauth

import (
	"context"
	"crypto/hmac"
	"errors"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/agenttoken"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

var ErrInvalidToken = errors.New("invalid Agent token")

type Principal struct {
	TenantID        int
	TenantPublicID  string
	ProjectID       int
	ProjectPublicID string
	TokenID         int
	TokenPublicID   string
	TokenLabel      string
	TokenPrefix     string
	Scopes          []string
	ExpiresAt       *time.Time
}

func (p Principal) HasScope(scope string) bool {
	for _, candidate := range p.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

type Service struct {
	client *ent.Client
	box    *secrets.Box
	now    func() time.Time
}

func NewService(client *ent.Client, box *secrets.Box) *Service {
	return &Service{client: client, box: box, now: time.Now}
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Principal, error) {
	var principal Principal
	raw = strings.TrimSpace(raw)
	const prefixLength = len("gmc_") + 7
	if len(raw) < prefixLength+1+22 || !strings.HasPrefix(raw, "gmc_") || raw[prefixLength] != '_' {
		return principal, ErrInvalidToken
	}
	prefix := raw[:prefixLength]
	candidates, err := s.client.AgentToken.Query().
		Where(agenttoken.PrefixEQ(prefix), agenttoken.StatusEQ("active")).
		WithProject(func(query *ent.ProjectQuery) { query.WithTenant() }).
		All(ctx)
	if err != nil {
		return principal, err
	}
	digest := s.box.Digest("agent-token", raw)
	var matched *ent.AgentToken
	for _, candidate := range candidates {
		if hmac.Equal(candidate.TokenHash, digest) {
			matched = candidate
			break
		}
	}
	if matched == nil || matched.Edges.Project == nil || matched.Edges.Project.Edges.Tenant == nil {
		return principal, ErrInvalidToken
	}
	now := s.now().UTC()
	if matched.ExpiresAt != nil && !matched.ExpiresAt.After(now) {
		return principal, ErrInvalidToken
	}
	project := matched.Edges.Project
	if project.Status == "archived" {
		return principal, ErrInvalidToken
	}
	tenant := project.Edges.Tenant
	if tenant.Status != "active" {
		return principal, ErrInvalidToken
	}
	if err := matched.Update().SetLastUsedAt(now).Exec(ctx); err != nil {
		return principal, err
	}
	return Principal{
		TenantID:        tenant.ID,
		TenantPublicID:  tenant.PublicID.String(),
		ProjectID:       project.ID,
		ProjectPublicID: project.PublicID.String(),
		TokenID:         matched.ID,
		TokenPublicID:   matched.PublicID.String(),
		TokenLabel:      matched.Label,
		TokenPrefix:     matched.Prefix,
		Scopes:          append([]string(nil), matched.Scopes...),
		ExpiresAt:       matched.ExpiresAt,
	}, nil
}
