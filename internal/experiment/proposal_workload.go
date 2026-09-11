package experiment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/XR-Lee/Gemcp/internal/sourcearchive"
	"github.com/XR-Lee/Gemcp/internal/workload"
)

type namedWorkload struct {
	Name             string
	Spec             executioncmd.Spec
	RuntimePreset    string
	Dataset          string
	Parameters       map[string]string
	WorkingDirectory string
}

func (s *Service) resolveNamedWorkload(ctx context.Context, repository *ent.Repository, commitSHA string, input PrepareInput) (namedWorkload, error) {
	var result namedWorkload
	name := strings.TrimSpace(input.Workload)
	if name == "" {
		return result, nil
	}
	if repository == nil {
		return result, &ValidationError{Message: "named workloads require a registered GitHub repository"}
	}
	if s.archiver == nil {
		return result, fmt.Errorf("prepared experiment service is unavailable")
	}
	archiveCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	archive, err := s.archiver.ArchiveCommit(archiveCtx, repository.ID, commitSHA, s.proposalConfig.SourceMaxBytes)
	cancel()
	if err != nil {
		return result, fmt.Errorf("%w: read gemcp.yaml: %v", ErrCommitVerification, err)
	}
	defer archive.Close()
	raw, err := sourcearchive.ReadRootFile(archive, "gemcp.yaml", s.proposalConfig.SourceMaxBytes)
	if err != nil {
		return result, &ValidationError{Message: "named workload requires gemcp.yaml at the verified commit root: " + err.Error()}
	}
	manifest, err := workload.Parse(raw)
	if err != nil {
		return result, &ValidationError{Message: err.Error()}
	}
	resolved, err := workload.Resolve(manifest, name, input.Parameters)
	if err != nil {
		return result, &ValidationError{Message: err.Error()}
	}
	spec, err := executioncmd.Argv(resolved.Argv)
	if err != nil {
		return result, &ValidationError{Message: err.Error()}
	}
	return namedWorkload{
		Name:             resolved.Name,
		Spec:             spec,
		RuntimePreset:    resolved.RuntimePreset,
		Dataset:          resolved.Dataset,
		Parameters:       resolved.Parameters,
		WorkingDirectory: resolved.WorkingDirectory,
	}, nil
}
