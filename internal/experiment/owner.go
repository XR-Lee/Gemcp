package experiment

import (
	"context"
	"errors"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/agenttoken"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
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
		result = append(result, makeAttemptView(record))
	}
	return result, nil
}

func (s *Service) OwnerSubmitPrepared(ctx context.Context, tenantID int, actorID, projectPublicID, proposalPublicID string, input OwnerSubmitPreparedInput) (SubmitPreparedResult, error) {
	if !input.Confirmed {
		return SubmitPreparedResult{}, ErrConfirmationRequired
	}
	projectID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return SubmitPreparedResult{}, ErrNotFound
	}
	projectRecord, err := s.client.Project.Query().Where(
		project.PublicIDEQ(projectID), project.TenantIDEQ(tenantID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return SubmitPreparedResult{}, ErrNotFound
	}
	if err != nil {
		return SubmitPreparedResult{}, err
	}
	proposalID, err := uuid.Parse(strings.TrimSpace(proposalPublicID))
	if err != nil {
		return SubmitPreparedResult{}, ErrProposalNotFound
	}
	record, err := s.client.ExperimentProposal.Query().Where(
		experimentproposal.PublicIDEQ(proposalID), experimentproposal.ProjectIDEQ(projectRecord.ID),
	).WithAgentToken().Only(ctx)
	if ent.IsNotFound(err) {
		return SubmitPreparedResult{}, ErrProposalNotFound
	}
	if err != nil {
		return SubmitPreparedResult{}, err
	}
	principal, err := s.preparedSubmitPrincipal(ctx, tenantID, actorID, projectRecord, record)
	if err != nil {
		return SubmitPreparedResult{}, err
	}
	result, err := s.SubmitPrepared(ctx, principal, SubmitPreparedInput{
		ProposalID: proposalPublicID, ConfirmationDigest: input.ConfirmationDigest,
	})
	if err != nil {
		return result, err
	}
	if _, auditErr := s.client.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(auditevent.ActorTypeUser).SetActorID(strings.TrimSpace(actorID)).
		SetAction("experiment.proposal_owner_confirmed").SetTargetType("experiment").
		SetTargetID(result.Experiment.ID).
		SetMetadata(map[string]any{
			"project_id": projectRecord.PublicID.String(), "proposal_id": record.PublicID.String(),
			"confirmation_digest": strings.ToLower(strings.TrimSpace(input.ConfirmationDigest)),
			"idempotent":          result.Idempotent,
		}).Save(ctx); auditErr != nil {
		return result, auditErr
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

func (s *Service) OwnerPrepare(ctx context.Context, tenantID int, actorID, projectPublicID string, input PrepareInput) (PrepareResult, error) {
	principal, err := s.ownerSubmitPrincipal(ctx, tenantID, projectPublicID, actorID)
	if err != nil {
		return PrepareResult{}, err
	}
	return s.Prepare(ctx, principal, input)
}

func (s *Service) ownerSubmitPrincipal(ctx context.Context, tenantID int, projectPublicID, actorID string) (agentauth.Principal, error) {
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return agentauth.Principal{}, err
	}
	principal.Scopes = []string{"read", "submit"}
	principal.UserPublicID = strings.TrimSpace(actorID)
	return principal, nil
}

func (s *Service) preparedSubmitPrincipal(ctx context.Context, tenantID int, actorID string, projectRecord *ent.Project, record *ent.ExperimentProposal) (agentauth.Principal, error) {
	principal := agentauth.Principal{
		TenantID: tenantID, ProjectID: projectRecord.ID, ProjectPublicID: projectRecord.PublicID.String(),
		UserPublicID: strings.TrimSpace(actorID), Scopes: []string{"submit", "read"},
	}
	if record.AgentTokenID == nil {
		return principal, nil
	}
	token, err := record.Edges.AgentTokenOrErr()
	if err != nil {
		token, err = s.client.AgentToken.Query().Where(agenttoken.IDEQ(*record.AgentTokenID)).Only(ctx)
		if err != nil {
			return agentauth.Principal{}, err
		}
	}
	principal.TokenID = token.ID
	principal.TokenPublicID = token.PublicID.String()
	return principal, nil
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
