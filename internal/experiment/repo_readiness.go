package experiment

import (
	"context"
	"sort"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/datasetbinding"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/projectworkload"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/sourcearchive"
	"github.com/XR-Lee/Gemcp/internal/workload"
	"github.com/google/uuid"
)

type defaultBranchDetector interface {
	DetectDefaultBranch(context.Context, int) (string, string, error)
}

func (s *Service) OwnerRepositoryReadiness(ctx context.Context, tenantID int, projectPublicID, repositoryPublicID string) (RepositoryReadiness, error) {
	var result RepositoryReadiness
	projectID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return result, ErrNotFound
	}
	repositoryID, err := uuid.Parse(strings.TrimSpace(repositoryPublicID))
	if err != nil {
		return result, ErrNotFound
	}
	projectRecord, err := s.client.Project.Query().Where(
		project.PublicIDEQ(projectID), project.TenantIDEQ(tenantID), project.StatusEQ(project.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	record, err := s.client.Repository.Query().Where(
		repository.PublicIDEQ(repositoryID), repository.ProjectIDEQ(projectRecord.ID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	result = RepositoryReadiness{
		ID:                       record.PublicID.String(),
		ProjectID:                projectRecord.PublicID.String(),
		Name:                     record.Name,
		SSHURL:                   record.SSHURL,
		Status:                   string(record.Status),
		Access:                   gitrepository.AccessOf(record),
		DefaultBranch:            record.DefaultBranch,
		ObservationWritesAllowed: gitrepository.ObservationWritesAllowed(string(record.Status)),
		PendingNote:              gitrepository.PendingNote(string(record.Status)),
		Manifest:                 ManifestReadiness{},
		Defaults:                 ProjectDefaultsReadiness{},
	}
	if result.Access == gitrepository.AccessSSHDeploy {
		result.DeployPublicKey = record.DeployPublicKey
		result.DeployKeySettingsURL = gitrepository.DeployKeySettingsURL(record.SSHURL)
	}
	if gitrepository.HasHTTPSToken(record) {
		result.HTTPSTokenConfigured = true
	}
	if result.Access != gitrepository.AccessPublicHTTPS {
		result.HTTPSTokenSettingsURL = gitrepository.FineGrainedTokenSettingsURL
	}
	if saved, listErr := s.client.ProjectWorkload.Query().Where(
		projectworkload.ProjectIDEQ(projectRecord.ID),
	).Order(ent.Asc(projectworkload.FieldName)).All(ctx); listErr != nil {
		return result, listErr
	} else {
		result.ProjectWorkloads = make([]string, 0, len(saved))
		for _, item := range saved {
			result.ProjectWorkloads = append(result.ProjectWorkloads, item.Name)
		}
	}
	if record.Status != repository.StatusActive {
		if gitrepository.HasHTTPSToken(record) {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "https_token_verify_required",
				Title:  "Verify the GitHub HTTPS token",
				Detail: "Gemcp stored a write-only HTTPS token for this repository. Verify access to activate access=https_token. A fine-grained token with Contents: Read on this repository is enough.",
				Href:   result.HTTPSTokenSettingsURL,
			})
		} else {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "deploy_key_required",
				Title:  "Add a read-only Deploy Key",
				Detail: "Install this Gemcp public key on the GitHub repository as a read-only Deploy Key, then verify access. If Deploy Keys are disabled, configure a GitHub fine-grained token with Contents: Read and verify with https_token.",
				Href:   result.DeployKeySettingsURL,
			})
		}
		s.appendProjectDefaultBlockers(ctx, projectRecord, &result)
		result.Ready = len(result.Blockers) == 0
		return result, nil
	}
	s.inspectRepositorySource(ctx, record, &result)
	s.appendProjectDefaultBlockers(ctx, projectRecord, &result)
	result.Ready = len(result.Blockers) == 0
	return result, nil
}

func (s *Service) inspectRepositorySource(ctx context.Context, record *ent.Repository, result *RepositoryReadiness) {
	branch := strings.TrimSpace(record.DefaultBranch)
	if detector, ok := detectorFrom(s); ok {
		detected, sha, err := detector.DetectDefaultBranch(ctx, record.ID)
		if err != nil {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "repository_inspect_failed",
				Title:  "Repository access check failed",
				Detail: "Gemcp could not read HEAD after verification. Confirm the Deploy Key or HTTPS token still has Contents: Read on this repository and verify again.",
			})
			return
		}
		if detected != "" {
			result.DetectedDefaultBranch = detected
			if detected != record.DefaultBranch {
				updated, updateErr := record.Update().SetDefaultBranch(detected).Save(ctx)
				if updateErr == nil {
					record = updated
					result.DefaultBranch = detected
					branch = detected
				}
			} else {
				branch = detected
			}
		}
		if isFullCommitSHA(sha) {
			result.CommitSHA = sha
		}
	}
	if result.CommitSHA == "" {
		if s.refResolver == nil {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "repository_inspect_failed",
				Title:  "Repository access check failed",
				Detail: "Gemcp could not resolve the default branch to a full commit SHA.",
			})
			return
		}
		if branch == "" {
			branch = "main"
		}
		sha, err := s.refResolver.ResolveRef(ctx, record.ID, branch)
		if err != nil || !isFullCommitSHA(sha) {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "repository_inspect_failed",
				Title:  "Repository access check failed",
				Detail: "Gemcp could not resolve the default branch to a full commit SHA.",
			})
			return
		}
		result.CommitSHA = sha
	}
	if s.archiver == nil {
		return
	}
	maxBytes := s.proposalConfig.SourceMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultProposalSourceMaxBytes
	}
	archive, err := s.archiver.ArchiveCommit(ctx, record.ID, result.CommitSHA, maxBytes)
	if err != nil {
		result.Blockers = append(result.Blockers, ReadinessBlocker{
			Kind:   "repository_inspect_failed",
			Title:  "Repository source archive failed",
			Detail: "Gemcp verified the default branch but could not read the commit tree.",
		})
		return
	}
	defer archive.Close()
	raw, err := sourcearchive.ReadRootFile(archive, "gemcp.yaml", s.proposalConfig.SourceMaxBytes)
	if err != nil {
		result.Manifest.Error = err.Error()
		return
	}
	manifest, err := workload.Parse(raw)
	if err != nil {
		result.Manifest.Error = err.Error()
		return
	}
	names := make([]string, 0, len(manifest.Workloads))
	for name := range manifest.Workloads {
		names = append(names, name)
	}
	sort.Strings(names)
	result.Manifest.Present = true
	result.Manifest.Workloads = names
}

func (s *Service) appendProjectDefaultBlockers(ctx context.Context, projectRecord *ent.Project, result *RepositoryReadiness) {
	environments, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ(environment.StatusApproved),
	).Order(ent.Desc(environment.FieldIsDefault), ent.Asc(environment.FieldName)).All(ctx)
	if err != nil {
		return
	}
	profiles, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Order(ent.Desc(resourceprofile.FieldIsDefault), ent.Asc(resourceprofile.FieldName)).All(ctx)
	if err != nil {
		return
	}
	bindings, err := s.client.DatasetBinding.Query().Where(
		datasetbinding.ProjectIDEQ(projectRecord.ID), datasetbinding.StatusEQ(datasetbinding.StatusActive),
	).Order(ent.Asc(datasetbinding.FieldName)).All(ctx)
	if err != nil {
		return
	}
	environmentRecord, profileRecord, _, chooseErr := chooseProposalResources(environments, profiles, "", "")
	if chooseErr != nil {
		if len(environments) == 0 {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "environment_required",
				Title:  "Register a default Environment",
				Detail: "Prepare needs one approved Environment that matches a Resource Profile backend.",
			})
		}
		if len(profiles) == 0 {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "resource_profile_required",
				Title:  "Register a default Resource Profile",
				Detail: "Prepare needs one active Resource Profile that matches the Environment backend.",
			})
		}
		if len(environments) > 0 && len(profiles) > 0 {
			result.Blockers = append(result.Blockers, ReadinessBlocker{
				Kind:   "incompatible_defaults",
				Title:  "Environment and Resource Profile do not match",
				Detail: "Approved Environments and active Resource Profiles use different backends, so prepare cannot pick a default pair.",
			})
		}
	} else if environmentRecord != nil && profileRecord != nil {
		result.Defaults.Environment = &ReadinessDefault{
			ID: environmentRecord.PublicID.String(), Name: environmentRecord.Name, Backend: string(environmentRecord.Backend),
		}
		result.Defaults.ResourceProfile = &ReadinessDefault{
			ID: profileRecord.PublicID.String(), Name: profileRecord.Name, Backend: string(profileRecord.Backend),
		}
	}
	if len(bindings) == 0 {
		result.Blockers = append(result.Blockers, ReadinessBlocker{
			Kind:   "dataset_binding_required",
			Title:  "Register a Dataset Binding",
			Detail: "Probe and train need a Project Dataset Binding. Smoke can still use a one-shot argv.",
		})
		return
	}
	result.Defaults.DatasetBinding = &ReadinessDefault{
		ID: bindings[0].PublicID.String(), Name: bindings[0].Name, Backend: string(bindings[0].Backend),
	}
}

func detectorFrom(s *Service) (defaultBranchDetector, bool) {
	if detector, ok := any(s.refResolver).(defaultBranchDetector); ok {
		return detector, true
	}
	if detector, ok := any(s.archiver).(defaultBranchDetector); ok {
		return detector, true
	}
	return nil, false
}

func isFullCommitSHA(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return false
		}
	}
	return true
}
