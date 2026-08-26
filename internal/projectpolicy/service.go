package projectpolicy

import (
	"context"
	"errors"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("project not found")

type UpdateInput struct {
	MonthlyBudgetMilli      *int64 `json:"monthly_budget_milli"`
	MaxExperimentMilli      *int64 `json:"max_experiment_milli"`
	MaxConcurrency          *int   `json:"max_concurrency"`
	MaxRuntimeSeconds       *int   `json:"max_runtime_seconds"`
	TimeoutExtensionSeconds *int   `json:"timeout_extension_seconds"`
	TerminationGraceSeconds *int   `json:"termination_grace_seconds"`
}

type Service struct{ client *ent.Client }

func NewService(client *ent.Client) *Service { return &Service{client: client} }

func (s *Service) Update(ctx context.Context, tenantID int, actorID, projectPublicID string, input UpdateInput) (*ent.Project, error) {
	if input.MonthlyBudgetMilli == nil && input.MaxExperimentMilli == nil && input.MaxConcurrency == nil &&
		input.MaxRuntimeSeconds == nil && input.TimeoutExtensionSeconds == nil && input.TerminationGraceSeconds == nil {
		return nil, invalid("project policy update must include at least one limit")
	}
	publicID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	record, err := tx.Project.Query().Where(
		project.PublicIDEQ(publicID), project.TenantIDEQ(tenantID), project.StatusEQ(project.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	current := Limits{
		MonthlyBudgetMilli: record.MonthlyBudgetMilli, MaxExperimentMilli: record.MaxExperimentMilli,
		MaxConcurrency: record.MaxConcurrency, MaxRuntimeSeconds: record.MaxRuntimeSeconds,
		TimeoutExtensionSeconds: record.TimeoutExtensionSeconds, TerminationGraceSeconds: record.TerminationGraceSeconds,
	}
	next := current
	if input.MonthlyBudgetMilli != nil {
		next.MonthlyBudgetMilli = *input.MonthlyBudgetMilli
	}
	if input.MaxExperimentMilli != nil {
		next.MaxExperimentMilli = *input.MaxExperimentMilli
	}
	if input.MaxConcurrency != nil {
		next.MaxConcurrency = *input.MaxConcurrency
	}
	if input.MaxRuntimeSeconds != nil {
		next.MaxRuntimeSeconds = *input.MaxRuntimeSeconds
	}
	if input.TimeoutExtensionSeconds != nil {
		next.TimeoutExtensionSeconds = *input.TimeoutExtensionSeconds
	}
	if input.TerminationGraceSeconds != nil {
		next.TerminationGraceSeconds = *input.TerminationGraceSeconds
	}
	if err := ValidateUpdate(current, next); err != nil {
		return nil, err
	}
	updated, err := record.Update().
		SetMonthlyBudgetMilli(next.MonthlyBudgetMilli).
		SetMaxExperimentMilli(next.MaxExperimentMilli).
		SetMaxConcurrency(next.MaxConcurrency).
		SetMaxRuntimeSeconds(next.MaxRuntimeSeconds).
		SetTimeoutExtensionSeconds(next.TimeoutExtensionSeconds).
		SetTerminationGraceSeconds(next.TerminationGraceSeconds).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType(auditevent.ActorTypeUser).
		SetActorID(strings.TrimSpace(actorID)).
		SetAction("project.policy_updated").
		SetTargetType("project").
		SetTargetID(updated.PublicID.String()).
		SetMetadata(map[string]any{
			"monthly_budget_milli": next.MonthlyBudgetMilli, "max_experiment_milli": next.MaxExperimentMilli,
			"max_concurrency": next.MaxConcurrency, "max_runtime_seconds": next.MaxRuntimeSeconds,
			"timeout_extension_seconds": next.TimeoutExtensionSeconds, "termination_grace_seconds": next.TerminationGraceSeconds,
		}).Save(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}
