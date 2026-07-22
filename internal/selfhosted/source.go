package selfhosted

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/google/uuid"
)

const maxSourceDownloads = 3

func (s *Service) Source(ctx context.Context, nodeID int, assignmentPublicID string) (repository.Archive, error) {
	if !s.config.Enabled {
		return nil, ErrDisabled
	}
	id, err := uuid.Parse(strings.TrimSpace(assignmentPublicID))
	if err != nil {
		return nil, ErrAssignment
	}
	record, err := s.client.NodeAssignment.Query().Where(
		nodeassignment.PublicIDEQ(id), nodeassignment.NodeIDEQ(nodeID),
		nodeassignment.StateIn(nodeassignment.StateStarting, nodeassignment.StateRunning),
	).WithAttempt().WithExperiment().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrAssignment
	}
	if err != nil {
		return nil, fmt.Errorf("load source Assignment: %w", err)
	}
	attemptRecord, err := record.Edges.AttemptOrErr()
	if err != nil {
		return nil, ErrAssignment
	}
	experimentRecord, err := record.Edges.ExperimentOrErr()
	if err != nil {
		return nil, ErrAssignment
	}
	updated, err := s.client.Attempt.Update().Where(
		attempt.IDEQ(attemptRecord.ID), attempt.SourceDownloadsLT(maxSourceDownloads),
	).AddSourceDownloads(1).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve Self-hosted source download: %w", err)
	}
	if updated != 1 {
		return nil, ErrSourceLimit
	}
	release := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.client.Attempt.Update().Where(attempt.IDEQ(attemptRecord.ID), attempt.SourceDownloadsGT(0)).AddSourceDownloads(-1).Save(releaseCtx)
	}
	if s.archiver == nil {
		release()
		return nil, fmt.Errorf("Self-hosted source archiver is unavailable")
	}
	archive, err := s.archiver.ArchiveCommit(ctx, experimentRecord.RepositoryID, experimentRecord.CommitSha, s.config.SourceMaxBytes)
	if err != nil {
		release()
		return nil, fmt.Errorf("archive Self-hosted experiment source: %w", err)
	}
	return archive, nil
}
