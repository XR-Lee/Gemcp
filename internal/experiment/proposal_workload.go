package experiment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/projectworkload"
	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/XR-Lee/Gemcp/internal/sourcearchive"
	"github.com/XR-Lee/Gemcp/internal/workload"
)

var errProjectWorkloadNotFound = errors.New("project workload not found")

type namedWorkload struct {
	Name             string
	Spec             executioncmd.Spec
	RuntimePreset    string
	Dataset          string
	Parameters       map[string]string
	WorkingDirectory string
}

func (s *Service) resolveNamedWorkload(ctx context.Context, project *ent.Project, repository *ent.Repository, commitSHA string, input PrepareInput) (namedWorkload, error) {
	var result namedWorkload
	name := strings.TrimSpace(input.Workload)
	if name == "" {
		return result, nil
	}
	if !workload.ValidName(name) {
		return result, &ValidationError{Message: fmt.Sprintf("workload name %q is invalid", name)}
	}
	var missingRepo error
	if repository != nil && s.archiver != nil {
		resolved, err := s.resolveRepoWorkload(ctx, repository, commitSHA, name, input.Parameters)
		if err == nil {
			return resolved, nil
		}
		if !isMissingRepoWorkload(err) {
			return result, err
		}
		missingRepo = err
	}
	resolved, err := s.resolveStoredProjectWorkload(ctx, project.ID, name, input.Parameters)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, errProjectWorkloadNotFound) {
		return result, err
	}
	if missingRepo != nil {
		return result, missingRepo
	}
	if repository == nil {
		return result, &ValidationError{Message: "named workloads require gemcp.yaml at the verified commit root or a saved Project workload"}
	}
	return result, &ValidationError{Message: fmt.Sprintf("named workload %q was not found in gemcp.yaml or Project workloads", name)}
}

func (s *Service) resolveRepoWorkload(ctx context.Context, repository *ent.Repository, commitSHA, name string, parameters map[string]string) (namedWorkload, error) {
	var result namedWorkload
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
	return resolveManifestWorkload(raw, name, parameters)
}

func (s *Service) resolveStoredProjectWorkload(ctx context.Context, projectID int, name string, parameters map[string]string) (namedWorkload, error) {
	var result namedWorkload
	record, err := s.client.ProjectWorkload.Query().Where(
		projectworkload.ProjectIDEQ(projectID), projectworkload.NameEQ(name),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, errProjectWorkloadNotFound
	}
	if err != nil {
		return result, err
	}
	return resolveManifestWorkload([]byte(record.ManifestYaml), name, parameters)
}

func resolveManifestWorkload(raw []byte, name string, parameters map[string]string) (namedWorkload, error) {
	var result namedWorkload
	manifest, err := workload.Parse(raw)
	if err != nil {
		return result, &ValidationError{Message: err.Error()}
	}
	resolved, err := workload.Resolve(manifest, name, parameters)
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

func isMissingRepoWorkload(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "gemcp.yaml was not found") ||
		strings.Contains(message, "named workload requires gemcp.yaml") ||
		strings.Contains(message, "has no workload")
}
