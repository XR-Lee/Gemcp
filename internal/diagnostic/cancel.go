package diagnostic

import (
	"context"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/diagnosticrun"
	entproject "github.com/XR-Lee/Gemcp/ent/project"
	"github.com/google/uuid"
)

func (s *Service) Cancel(ctx context.Context, tenantID int, actorID, projectID, runID string) (RunView, error) {
	var result RunView
	projectPublicID, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return result, ErrNotFound
	}
	runPublicID, err := uuid.Parse(strings.TrimSpace(runID))
	if err != nil {
		return result, ErrNotFound
	}
	record, err := s.client.DiagnosticRun.Query().Where(
		diagnosticrun.PublicIDEQ(runPublicID),
		diagnosticrun.HasProjectWith(entproject.PublicIDEQ(projectPublicID), entproject.TenantIDEQ(tenantID)),
	).WithExperiment().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return result, err
	}
	if _, err := s.experiments.OwnerCancel(ctx, tenantID, actorID, projectPublicID.String(), experimentRecord.PublicID.String()); err != nil {
		return result, err
	}
	return s.Get(ctx, tenantID, projectPublicID.String(), runPublicID.String())
}
