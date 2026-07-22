package experiment

import (
	"context"
	"errors"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
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

func (s *Service) OwnerCancel(ctx context.Context, tenantID int, actorID, projectPublicID, experimentPublicID string) (View, error) {
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	publicID, err := uuid.Parse(strings.TrimSpace(experimentPublicID))
	if err != nil {
		return View{}, ErrNotFound
	}
	for attempt := 0; attempt < 3; attempt++ {
		view, cancelErr := s.cancelOnce(ctx, principal, publicID, "user", strings.TrimSpace(actorID))
		if cancelErr == nil {
			return view, nil
		}
		if !isRetryableTransaction(cancelErr) && !errors.Is(cancelErr, errStateChanged) {
			return View{}, cancelErr
		}
		err = cancelErr
	}
	return View{}, err
}

func (s *Service) OwnerAttempts(ctx context.Context, tenantID int, projectPublicID, experimentPublicID string) ([]AttemptView, error) {
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return nil, err
	}
	experimentRecord, err := s.getRecord(ctx, principal.ProjectID, experimentPublicID)
	if err != nil {
		return nil, err
	}
	records, err := s.client.Attempt.Query().Where(attempt.ExperimentIDEQ(experimentRecord.ID)).
		Order(ent.Asc(attempt.FieldNumber)).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]AttemptView, 0, len(records))
	for _, record := range records {
		result = append(result, AttemptView{
			ID: record.PublicID.String(), Number: record.Number, State: record.State,
			ProviderResourceID: record.ProviderResourceID, RetryReason: record.RetryReason,
			FailureCode: record.FailureCode, FailureReason: record.FailureReason,
			StartedAt: record.StartedAt, FinishedAt: record.FinishedAt,
			EstimatedCostMilli: record.EstimatedCostMilli, ExitCode: record.ExitCode,
			LogTail: record.LogTail, Metrics: record.Metrics,
			CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		})
	}
	return result, nil
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
