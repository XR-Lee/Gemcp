package experiment

import (
	"context"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/google/uuid"
)

func (s *Service) OwnerList(ctx context.Context, tenantID int, projectPublicID string, input ListInput) (ListResult, error) {
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return ListResult{}, err
	}
	return s.List(ctx, principal, input)
}

func (s *Service) OwnerGet(ctx context.Context, tenantID int, projectPublicID, experimentPublicID string) (View, error) {
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, principal, experimentPublicID)
}

func (s *Service) OwnerCost(ctx context.Context, tenantID int, projectPublicID string) (CostView, error) {
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return CostView{}, err
	}
	return s.Cost(ctx, principal)
}

func (s *Service) ownerPrincipal(ctx context.Context, tenantID int, value string) (agentauth.Principal, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return agentauth.Principal{}, ErrNotFound
	}
	record, err := s.client.Project.Query().Where(
		project.PublicIDEQ(publicID), project.TenantIDEQ(tenantID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return agentauth.Principal{}, ErrNotFound
	}
	if err != nil {
		return agentauth.Principal{}, err
	}
	return agentauth.Principal{
		TenantID: tenantID, ProjectID: record.ID, ProjectPublicID: record.PublicID.String(), Scopes: []string{"read"},
	}, nil
}
