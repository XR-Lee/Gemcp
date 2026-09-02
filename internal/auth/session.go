package auth

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/session"
	"github.com/XR-Lee/Gemcp/ent/user"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

var ErrInvalidCredentials = errors.New("invalid email or password")
var ErrInvalidSession = errors.New("invalid or expired session")
var ErrInvalidCSRF = errors.New("invalid CSRF token")

type Service struct {
	client       *ent.Client
	box          *secrets.Box
	ttl          time.Duration
	now          func() time.Time
	skipPassword bool
}

type ServiceOption func(*Service)

func WithSkipPassword(skip bool) ServiceOption {
	return func(service *Service) {
		if service != nil {
			service.skipPassword = skip
		}
	}
}

type SessionCredentials struct {
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
	User         Principal
}

type Principal struct {
	UserID         int    `json:"-"`
	UserPublicID   string `json:"user_id"`
	TenantID       int    `json:"-"`
	TenantPublicID string `json:"tenant_id"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	SessionID      int    `json:"-"`
}

func NewService(client *ent.Client, box *secrets.Box, ttl time.Duration, options ...ServiceOption) *Service {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	service := &Service{client: client, box: box, ttl: ttl, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) SkipPassword() bool {
	return s != nil && s.skipPassword
}

func (s *Service) Login(ctx context.Context, email, password string) (SessionCredentials, error) {
	var credentials SessionCredentials
	if s == nil || s.client == nil || s.box == nil {
		return credentials, fmt.Errorf("auth service is not initialized")
	}
	account, err := s.client.User.Query().
		Where(user.EmailEQ(strings.ToLower(strings.TrimSpace(email))), user.StatusEQ(user.StatusActive)).
		WithTenant().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return credentials, ErrInvalidCredentials
		}
		return credentials, fmt.Errorf("query owner: %w", err)
	}
	if !s.skipPassword && !VerifyPassword(account.PasswordHash, password) {
		return credentials, ErrInvalidCredentials
	}

	sessionToken, _, err := secrets.RandomToken("gms", 32)
	if err != nil {
		return credentials, err
	}
	csrfToken, _, err := secrets.RandomToken("csrf", 32)
	if err != nil {
		return credentials, err
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)
	record, err := s.client.Session.Create().
		SetUserID(account.ID).
		SetTokenHash(s.box.Digest("web-session", sessionToken)).
		SetCsrfHash(s.box.Digest("csrf", csrfToken)).
		SetExpiresAt(expiresAt).
		SetLastUsedAt(now).
		Save(ctx)
	if err != nil {
		return credentials, fmt.Errorf("create session: %w", err)
	}
	tenant, err := account.Edges.TenantOrErr()
	if err != nil {
		return credentials, fmt.Errorf("load owner tenant: %w", err)
	}
	return SessionCredentials{
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    expiresAt,
		User: Principal{
			UserID:         account.ID,
			UserPublicID:   account.PublicID.String(),
			TenantID:       tenant.ID,
			TenantPublicID: tenant.PublicID.String(),
			Email:          account.Email,
			Role:           string(account.Role),
			SessionID:      record.ID,
		},
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, *ent.Session, error) {
	var principal Principal
	if s == nil || s.client == nil || s.box == nil || strings.TrimSpace(token) == "" {
		return principal, nil, ErrInvalidSession
	}
	record, err := s.client.Session.Query().
		Where(session.TokenHashEQ(s.box.Digest("web-session", token)), session.RevokedAtIsNil()).
		WithUser(func(query *ent.UserQuery) { query.WithTenant() }).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return principal, nil, ErrInvalidSession
		}
		return principal, nil, fmt.Errorf("query session: %w", err)
	}
	now := s.now().UTC()
	if !record.ExpiresAt.After(now) {
		return principal, nil, ErrInvalidSession
	}
	account, err := record.Edges.UserOrErr()
	if err != nil || account.Status != user.StatusActive {
		return principal, nil, ErrInvalidSession
	}
	tenant, err := account.Edges.TenantOrErr()
	if err != nil {
		return principal, nil, ErrInvalidSession
	}
	if now.Sub(record.LastUsedAt) >= 5*time.Minute {
		if _, updateErr := record.Update().SetLastUsedAt(now).Save(ctx); updateErr != nil {
			return principal, nil, fmt.Errorf("touch session: %w", updateErr)
		}
	}
	return Principal{
		UserID:         account.ID,
		UserPublicID:   account.PublicID.String(),
		TenantID:       tenant.ID,
		TenantPublicID: tenant.PublicID.String(),
		Email:          account.Email,
		Role:           string(account.Role),
		SessionID:      record.ID,
	}, record, nil
}

func (s *Service) ValidateCSRF(record *ent.Session, provided string) error {
	if s == nil || s.box == nil || record == nil || strings.TrimSpace(provided) == "" {
		return ErrInvalidCSRF
	}
	if !hmac.Equal(record.CsrfHash, s.box.Digest("csrf", provided)) {
		return ErrInvalidCSRF
	}
	return nil
}

func (s *Service) Logout(ctx context.Context, record *ent.Session) error {
	if record == nil || record.RevokedAt != nil {
		return nil
	}
	if _, err := record.Update().SetRevokedAt(s.now().UTC()).Save(ctx); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
