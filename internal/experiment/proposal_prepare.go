package experiment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/cloudsshassignment"
	"github.com/XR-Lee/Gemcp/ent/cloudsshnode"
	"github.com/XR-Lee/Gemcp/ent/cloudsshprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/datasetbinding"
	"github.com/XR-Lee/Gemcp/ent/environment"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/project"
	entrepository "github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/ent/workspacedataset"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/sourcearchive"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/google/uuid"
)

const (
	defaultProposalSourceMaxBytes = int64(256 << 20)
	defaultProposalLifetime       = 2 * time.Hour
	defaultProposalNodeStaleAfter = time.Minute
	smokeRuntimeSeconds           = 300
	probeRuntimeSeconds           = 3600
	trainRuntimeDefaultSeconds    = 10800
	sshCloudAgentWarning          = "Cloud SSH is experimental. The control plane holds host login credentials and starts the command as a host process. There is no container isolation. Emergency Stop only kills the Gemcp-started process group."
)

var proposalPinnedImage = regexp.MustCompile(`^[^[:space:]@]+@sha256:[0-9a-f]{64}$`)
var proposalWorkspaceImage = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*(?:@sha256:[0-9a-f]{64})?$`)

const (
	proposalWorkspaceRecipePrefix = "trusted-workspace:"
	sshCloudRecipePrefix          = "ssh-cloud:"
)

type proposalWorkspace struct {
	nodeID    string
	nodeLabel string
	path      string
	datasets  []nodeprotocol.WorkspaceDataset
}

type proposalResolved struct {
	id             uuid.UUID
	project        *ent.Project
	repository     *ent.Repository
	environment    *ent.Environment
	profile        *ent.ResourceProfile
	image          string
	workspace      *proposalWorkspace
	ref            string
	commitSHA      string
	execution      executioncmd.Spec
	preset         string
	runtime        int
	reservation    int64
	expiresAt      time.Time
	checks         []ProposalCheck
	fromNodeID     string
	expectedMetric string
	cwd            string
	sshHost        string
	sshUser        string
	sshNodeID      string
	sshNodeLabel   string
	bindings       []datasetcatalog.View
}

type proposalPair struct {
	environment *ent.Environment
	profile     *ent.ResourceProfile
	score       int
}

func (s *Service) normalizeProposalConfig() {
	if s.proposalConfig.SourceMaxBytes <= 0 {
		s.proposalConfig.SourceMaxBytes = defaultProposalSourceMaxBytes
	}
	if s.proposalConfig.NodeStaleAfter <= 0 {
		s.proposalConfig.NodeStaleAfter = defaultProposalNodeStaleAfter
	}
	if s.proposalConfig.Lifetime <= 0 {
		s.proposalConfig.Lifetime = defaultProposalLifetime
	}
}

func (s *Service) Prepare(ctx context.Context, principal agentauth.Principal, input PrepareInput) (PrepareResult, error) {
	if !principal.HasScope("submit") {
		return PrepareResult{}, ErrForbidden
	}
	if s.refResolver == nil || s.archiver == nil || s.runtimeReader == nil {
		return PrepareResult{}, fmt.Errorf("prepared experiment service is unavailable")
	}
	executionSpec, err := executioncmd.Argv(input.Argv)
	if err != nil {
		return PrepareResult{}, &ValidationError{Message: err.Error()}
	}
	if _, err := s.ReportActivity(ctx, principal, ReportActivityInput{
		Phase: "preparing_proposal", RepositoryRemote: input.RepositoryRemote, Ref: input.Ref,
	}); err != nil {
		return PrepareResult{}, err
	}
	resolved, choices, err := s.resolveProposal(ctx, principal, input, executionSpec)
	if err != nil {
		_, _ = s.recordActivity(context.WithoutCancel(ctx), principal, "blocked", input.RepositoryRemote, input.Ref, "", "")
		return PrepareResult{}, err
	}
	if len(choices) > 0 {
		_, _ = s.recordActivity(context.WithoutCancel(ctx), principal, "blocked", input.RepositoryRemote, input.Ref, "", "")
		return PrepareResult{ChoiceRequired: choices}, nil
	}
	resolved.checks = s.proposalChecks(ctx, resolved)
	digest := proposalDigest(resolved)
	checkMaps := make([]map[string]any, 0, len(resolved.checks))
	for _, check := range resolved.checks {
		checkMaps = append(checkMaps, map[string]any{"id": check.ID, "status": check.Status, "summary": check.Summary, "detail": check.Detail})
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return PrepareResult{}, err
	}
	defer tx.Rollback()
	create := tx.ExperimentProposal.Create().
		SetPublicID(resolved.id).
		SetTenantID(principal.TenantID).
		SetProjectID(resolved.project.ID).
		SetAgentTokenID(principal.TokenID).
		SetEnvironmentID(resolved.environment.ID).
		SetResourceProfileID(resolved.profile.ID).
		SetRequestedRef(resolved.ref).
		SetCommitSha(resolved.commitSHA).
		SetExecutionMode("argv").
		SetArgv(resolved.execution.Argv).
		SetDisplayCommand(executioncmd.DisplayArgv(resolved.execution.Argv)).
		SetRuntimePreset(resolved.preset).
		SetMaxRuntimeSeconds(resolved.runtime).
		SetTimeoutExtensionSeconds(resolved.project.TimeoutExtensionSeconds).
		SetTerminationGraceSeconds(resolved.project.TerminationGraceSeconds).
		SetProjectSnapshot(proposalStoredProjectSnapshot(resolved)).
		SetRepositorySnapshot(repositorySnapshot(resolved.repository, resolved.project.PublicID.String())).
		SetEnvironmentSnapshot(proposalEnvironmentSnapshot(resolved)).
		SetResourceSnapshot(resourceSnapshot(resolved.profile)).
		SetChecks(checkMaps).
		SetReservedCostMilli(resolved.reservation).
		SetConfirmationDigest(digest).
		SetExpiresAt(resolved.expiresAt)
	if resolved.repository != nil {
		create.SetRepositoryID(resolved.repository.ID)
	}
	record, err := create.Save(ctx)
	if err != nil {
		return PrepareResult{}, err
	}
	auditMeta := map[string]any{
		"project_id": resolved.project.PublicID.String(), "commit_sha": resolved.commitSHA,
		"backend": resolved.profile.Backend, "reserved_cost_milli": resolved.reservation,
		"eligible": proposalChecksEligible(resolved.checks), "confirmation_digest": digest,
	}
	if resolved.repository != nil {
		auditMeta["repository_id"] = resolved.repository.PublicID.String()
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(principal.TenantID).
		SetActorType("agent_token").
		SetActorID(principal.TokenPublicID).
		SetAction("experiment.proposal_prepared").
		SetTargetType("experiment_proposal").
		SetTargetID(record.PublicID.String()).
		SetMetadata(auditMeta).Save(ctx); err != nil {
		return PrepareResult{}, err
	}
	activityMeta := map[string]any{
		"project_id": principal.ProjectPublicID, "phase": "awaiting_confirmation", "proposal_id": record.PublicID.String(),
		"ref": resolved.ref,
	}
	if resolved.repository != nil {
		activityMeta["repository_remote"] = resolved.repository.SSHURL
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(principal.TenantID).SetActorType("agent_token").SetActorID(principal.TokenPublicID).
		SetAction("agent.activity").SetTargetType("project").SetTargetID(principal.ProjectPublicID).
		SetMetadata(activityMeta).Save(ctx); err != nil {
		return PrepareResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PrepareResult{}, err
	}
	proposal := preparedProposal(resolved, digest, record.CreatedAt)
	return PrepareResult{Proposal: &proposal}, nil
}

func (s *Service) resolveProposal(ctx context.Context, principal agentauth.Principal, input PrepareInput, executionSpec executioncmd.Spec) (proposalResolved, []ProposalChoice, error) {
	var result proposalResolved
	projectRecord, err := s.client.Project.Query().Where(
		project.IDEQ(principal.ProjectID), project.TenantIDEQ(principal.TenantID), project.StatusEQ(project.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, nil, ErrProjectPaused
	}
	if err != nil {
		return result, nil, err
	}
	repositories, err := s.client.Repository.Query().Where(
		entrepository.ProjectIDEQ(projectRecord.ID), entrepository.StatusEQ(entrepository.StatusActive),
	).Order(ent.Asc(entrepository.FieldName)).All(ctx)
	if err != nil {
		return result, nil, err
	}
	if ensured, ensureErr := s.ensureSSHCloudForPrepare(ctx, principal, &input); ensureErr != nil {
		return result, nil, ensureErr
	} else if ensured.EnvironmentName != "" && strings.TrimSpace(input.Environment) == "" {
		input.Environment = ensured.EnvironmentName
		if strings.TrimSpace(input.ResourceProfile) == "" {
			input.ResourceProfile = ensured.ProfileName
		}
	}
	environments, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ(environment.StatusApproved),
	).Order(ent.Asc(environment.FieldName)).All(ctx)
	if err != nil {
		return result, nil, err
	}
	profiles, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Order(ent.Asc(resourceprofile.FieldName)).All(ctx)
	if err != nil {
		return result, nil, err
	}
	environmentRecord, profileRecord, choices, err := chooseProposalResources(environments, profiles, input.Environment, input.ResourceProfile)
	if err != nil || len(choices) > 0 {
		return result, choices, err
	}
	if err := validateProposalResource(profileRecord); err != nil {
		return result, nil, err
	}
	sshCloud := profileRecord.Backend == resourceprofile.BackendSSHCloud
	var repositoryRecord *ent.Repository
	if !sshCloud || strings.TrimSpace(input.Repository) != "" || strings.TrimSpace(input.RepositoryRemote) != "" {
		chosen, repoChoices, repoErr := chooseProposalRepository(repositories, input.Repository, input.RepositoryRemote)
		if repoErr != nil || len(repoChoices) > 0 {
			return result, repoChoices, repoErr
		}
		repositoryRecord = chosen
	}
	if !sshCloud && repositoryRecord == nil {
		return result, nil, ErrOptionNotFound
	}
	image, workspace, err := s.resolveProposalRuntime(ctx, projectRecord, environmentRecord, profileRecord, input.Image, false)
	if err != nil {
		return result, nil, err
	}
	preset, runtimeSeconds, err := resolveProposalRuntimeLimit(input.RuntimePreset, input.MaxRuntimeSeconds, projectRecord.MaxRuntimeSeconds)
	if err != nil {
		return result, nil, err
	}
	cwd, err := normalizeProposalCwd(input.Cwd)
	if err != nil {
		return result, nil, err
	}
	ref := strings.TrimSpace(input.Ref)
	commitSHA := sshcloud.HostCommit
	if repositoryRecord != nil {
		if ref == "" {
			ref = repositoryRecord.DefaultBranch
		}
		resolveCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		commitSHA, err = s.refResolver.ResolveRef(resolveCtx, repositoryRecord.ID, ref)
		cancel()
		if err != nil {
			return result, nil, fmt.Errorf("%w: resolve ref %q: %v", ErrCommitVerification, ref, err)
		}
		commitSHA = strings.ToLower(strings.TrimSpace(commitSHA))
		if !commitPattern.MatchString(commitSHA) {
			return result, nil, fmt.Errorf("%w: resolved ref did not return a full commit SHA", ErrCommitVerification)
		}
	} else {
		ref = sshcloud.HostRef
	}
	reservation := int64(0)
	if !unmeteredBackend(profileRecord.Backend) {
		billable, err := billableRuntimeSeconds(runtimeSeconds, projectRecord.TimeoutExtensionSeconds, projectRecord.TerminationGraceSeconds)
		if err != nil {
			return result, nil, err
		}
		reservation, err = reserveCost(profileRecord.PriceToMilli, profileRecord.GpuNum, billable)
		if err != nil {
			return result, nil, err
		}
	}
	fromNodeID, expectedMetric, err := s.resolveGraphOrigin(ctx, principal, input)
	if err != nil {
		return result, nil, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	resolved := proposalResolved{
		id: uuid.New(), project: projectRecord, repository: repositoryRecord, environment: environmentRecord, profile: profileRecord,
		image: image, workspace: workspace, cwd: cwd,
		ref: ref, commitSHA: commitSHA, execution: executionSpec, preset: preset, runtime: runtimeSeconds,
		reservation: reservation, expiresAt: now.Add(s.proposalConfig.Lifetime).Truncate(time.Microsecond),
		fromNodeID: fromNodeID, expectedMetric: expectedMetric,
	}
	if sshCloud {
		if err := s.attachSSHCloudTarget(ctx, projectRecord, environmentRecord, &resolved); err != nil {
			return result, nil, err
		}
	}
	if err := s.attachProposalBindings(ctx, &resolved); err != nil {
		return result, nil, err
	}
	return resolved, nil, nil
}

func resolveProposalRuntimeLimit(preset string, requested, projectMax int) (string, int, error) {
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == "" {
		preset = "smoke"
	}
	var ceiling, defaultRuntime int
	switch preset {
	case "smoke":
		ceiling, defaultRuntime = smokeRuntimeSeconds, smokeRuntimeSeconds
	case "probe":
		ceiling, defaultRuntime = probeRuntimeSeconds, probeRuntimeSeconds
	case "train":
		ceiling = projectMax
		defaultRuntime = trainRuntimeDefaultSeconds
		if defaultRuntime > projectMax {
			defaultRuntime = projectMax
		}
	default:
		return "", 0, &ValidationError{Message: "runtime_preset must be smoke, probe, or train"}
	}
	if ceiling > projectMax {
		ceiling = projectMax
	}
	runtimeSeconds := requested
	if runtimeSeconds == 0 {
		runtimeSeconds = defaultRuntime
	}
	if runtimeSeconds <= 0 || runtimeSeconds > ceiling {
		return "", 0, &ValidationError{Message: fmt.Sprintf("max_runtime_seconds must be between 1 and %d for the %s preset", ceiling, preset)}
	}
	return preset, runtimeSeconds, nil
}

func chooseProposalRepository(records []*ent.Repository, selector, remote string) (*ent.Repository, []ProposalChoice, error) {
	selector, remote = strings.TrimSpace(selector), strings.TrimSpace(remote)
	if selector != "" && remote != "" {
		return nil, nil, &ValidationError{Message: "repository and repository_remote cannot both be set"}
	}
	matches := make([]*ent.Repository, 0, len(records))
	for _, record := range records {
		match := selector == "" && remote == ""
		if selector != "" {
			match = strings.EqualFold(selector, record.Name) || strings.EqualFold(selector, record.PublicID.String())
		}
		if remote != "" {
			match = normalizedGitRemote(remote) != "" && normalizedGitRemote(remote) == normalizedGitRemote(record.SSHURL)
		}
		if match {
			matches = append(matches, record)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil, nil
	}
	if len(matches) == 0 {
		return nil, nil, ErrOptionNotFound
	}
	choices := make([]ProposalChoice, 0, len(matches))
	for _, record := range matches {
		choices = append(choices, ProposalChoice{Field: "repository", ID: record.PublicID.String(), Name: record.Name, Detail: record.SSHURL})
	}
	return nil, choices, nil
}

func chooseProposalResources(environments []*ent.Environment, profiles []*ent.ResourceProfile, environmentSelector, profileSelector string) (*ent.Environment, *ent.ResourceProfile, []ProposalChoice, error) {
	environmentSelector, profileSelector = strings.TrimSpace(environmentSelector), strings.TrimSpace(profileSelector)
	pairs := make([]proposalPair, 0)
	for _, environmentRecord := range environments {
		if environmentSelector != "" && !matchesProposalOption(environmentSelector, environmentRecord.PublicID.String(), environmentRecord.Name) {
			continue
		}
		for _, profileRecord := range profiles {
			if profileSelector != "" && !matchesProposalOption(profileSelector, profileRecord.PublicID.String(), profileRecord.Name) {
				continue
			}
			if string(environmentRecord.Backend) != string(profileRecord.Backend) {
				continue
			}
			if strings.HasPrefix(environmentRecord.RecipeRef, proposalWorkspaceRecipePrefix) && environmentRecord.Name != profileRecord.Name {
				continue
			}
			score := 0
			if environmentRecord.IsDefault {
				score++
			}
			if profileRecord.IsDefault {
				score++
			}
			pairs = append(pairs, proposalPair{environment: environmentRecord, profile: profileRecord, score: score})
		}
	}
	if len(pairs) == 0 {
		return nil, nil, nil, ErrOptionNotFound
	}
	maximum := pairs[0].score
	for _, pair := range pairs[1:] {
		if pair.score > maximum {
			maximum = pair.score
		}
	}
	best := make([]proposalPair, 0, len(pairs))
	for _, pair := range pairs {
		if pair.score == maximum {
			best = append(best, pair)
		}
	}
	if len(best) == 1 {
		return best[0].environment, best[0].profile, nil, nil
	}
	choices := []ProposalChoice{}
	seen := map[string]bool{}
	for _, pair := range best {
		environmentKey := "environment:" + pair.environment.PublicID.String()
		if !seen[environmentKey] {
			seen[environmentKey] = true
			choices = append(choices, ProposalChoice{Field: "environment", ID: pair.environment.PublicID.String(), Name: pair.environment.Name, Backend: string(pair.environment.Backend), Detail: pair.environment.ImageUUID})
		}
		profileKey := "resource_profile:" + pair.profile.PublicID.String()
		if !seen[profileKey] {
			seen[profileKey] = true
			choices = append(choices, ProposalChoice{Field: "resource_profile", ID: pair.profile.PublicID.String(), Name: pair.profile.Name, Backend: string(pair.profile.Backend), Detail: strings.Join(pair.profile.GpuNames, ", ")})
		}
	}
	sort.Slice(choices, func(left, right int) bool {
		if choices[left].Field == choices[right].Field {
			return choices[left].Name < choices[right].Name
		}
		return choices[left].Field < choices[right].Field
	})
	return nil, nil, choices, nil
}

func matchesProposalOption(selector, publicID, name string) bool {
	return strings.EqualFold(selector, publicID) || strings.EqualFold(selector, name)
}

func normalizedGitRemote(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSuffix(value, ".git")
	switch {
	case strings.HasPrefix(value, "git@github.com:"):
		return strings.TrimPrefix(value, "git@github.com:")
	case strings.HasPrefix(value, "ssh://git@github.com/"):
		return strings.TrimPrefix(value, "ssh://git@github.com/")
	case strings.HasPrefix(value, "https://github.com/"):
		return strings.TrimPrefix(value, "https://github.com/")
	default:
		return ""
	}
}

func (s *Service) ensureSSHCloudForPrepare(ctx context.Context, principal agentauth.Principal, input *PrepareInput) (sshcloud.EnsureResult, error) {
	var empty sshcloud.EnsureResult
	if !s.proposalConfig.SSHCloudEnabled || s.sshCloud == nil || input == nil {
		return empty, nil
	}
	sshSelector := looksLikeSSHCloudSelector(input.Environment) || looksLikeSSHCloudSelector(input.ResourceProfile)
	hasNodes, err := s.client.CloudSSHNode.Query().Where(
		cloudsshnode.TenantIDEQ(principal.TenantID),
		cloudsshnode.StatusIn(cloudsshnode.StatusPendingProbe, cloudsshnode.StatusActive),
	).Exist(ctx)
	if err != nil {
		return empty, err
	}
	selectedOther := (strings.TrimSpace(input.Environment) != "" && !looksLikeSSHCloudSelector(input.Environment)) ||
		(strings.TrimSpace(input.ResourceProfile) != "" && !looksLikeSSHCloudSelector(input.ResourceProfile))
	if selectedOther && !sshSelector {
		return empty, nil
	}
	if !sshSelector && !hasNodes {
		return empty, nil
	}
	input.Image = ""
	result, err := s.sshCloud.EnsureForProject(ctx, principal.TenantID, "agent:"+principal.TokenPublicID, principal.ProjectPublicID, sshcloud.HostImage)
	if err != nil {
		return empty, publicCloudSSHPrepareError(err)
	}
	return result, nil
}

func publicCloudSSHPrepareError(err error) error {
	if err == nil {
		return nil
	}
	var validation *sshcloud.ValidationError
	if errors.As(err, &validation) {
		return &ValidationError{Message: validation.Message}
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return err
	}
	if strings.Contains(message, "resolve Cloud SSH") || strings.Contains(message, "pull workload") ||
		strings.Contains(message, "still pulling") || strings.Contains(message, "runtime stays unlocked") {
		if len(message) > 1500 {
			message = strings.TrimSpace(message[:1500]) + "…"
		}
		return &ValidationError{Message: message}
	}
	return err
}

func looksLikeSSHCloudSelector(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(text, "ssh_cloud") || strings.Contains(text, "ssh-cloud")
}

func validateProposalResource(profileRecord *ent.ResourceProfile) error {
	if profileRecord.Backend == resourceprofile.BackendSSHCloud {
		if profileRecord.Region != "ssh_cloud" || profileRecord.PriceToMilli != 0 {
			return &ValidationError{Message: "Cloud SSH resource profile must use region ssh_cloud and a zero-CNY price"}
		}
		return nil
	}
	if len(profileRecord.GpuNames) == 0 || profileRecord.GpuNum <= 0 || profileRecord.CudaFrom > profileRecord.CudaTo ||
		profileRecord.CPUFrom > profileRecord.CPUTo || profileRecord.MemoryFromGB > profileRecord.MemoryToGB ||
		profileRecord.PriceFromMilli > profileRecord.PriceToMilli {
		return &ValidationError{Message: "resource profile has invalid or empty execution bounds"}
	}
	switch profileRecord.Backend {
	case resourceprofile.BackendAutodlPrivate:
		if profileRecord.Region != "private" || profileRecord.CudaFrom != profileRecord.CudaTo {
			return &ValidationError{Message: "Private Cloud resource profile must use region private and one CUDA version"}
		}
	case resourceprofile.BackendAutodlElastic:
		if strings.TrimSpace(profileRecord.Region) == "" || profileRecord.Region == "private" {
			return &ValidationError{Message: "Public Elastic resource profile must use a public region"}
		}
	}
	return nil
}

func (s *Service) resolveProposalRuntime(ctx context.Context, projectRecord *ent.Project, environmentRecord *ent.Environment, profileRecord *ent.ResourceProfile, requestedImage string, persisted bool) (string, *proposalWorkspace, error) {
	requestedImage = strings.TrimSpace(requestedImage)
	recipeRef := strings.TrimSpace(environmentRecord.RecipeRef)
	if !strings.HasPrefix(recipeRef, proposalWorkspaceRecipePrefix) {
		if environmentRecord.Backend == environment.BackendSSHCloud {
			return sshcloud.HostImage, nil, nil
		}
		if requestedImage != "" && (!persisted || requestedImage != environmentRecord.ImageUUID) {
			return "", nil, &ValidationError{Message: "image can be selected only for an Owner-approved trusted Self-hosted workspace"}
		}
		return environmentRecord.ImageUUID, nil, nil
	}
	if profileRecord.Backend != resourceprofile.BackendSelfHosted || environmentRecord.Backend != environment.BackendSelfHosted || profileRecord.Name != environmentRecord.Name {
		return "", nil, &ValidationError{Message: "trusted workspace Environment and Resource Profile are not a valid pair"}
	}
	nodePublicID, err := uuid.Parse(strings.TrimPrefix(recipeRef, proposalWorkspaceRecipePrefix))
	if err != nil {
		return "", nil, &ValidationError{Message: "trusted workspace binding is invalid"}
	}
	access, err := s.client.NodeProjectAccess.Query().Where(
		nodeprojectaccess.ProjectIDEQ(projectRecord.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		nodeprojectaccess.ExecutionPolicyEQ(nodeprojectaccess.ExecutionPolicyTrustedWorkspace), nodeprojectaccess.WorkspacePathNotNil(),
		nodeprojectaccess.HasNodeWith(selfhostednode.PublicIDEQ(nodePublicID), selfhostednode.TenantIDEQ(projectRecord.TenantID)),
	).WithNode().Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil, &ValidationError{Message: "trusted workspace authorization is no longer active"}
	}
	if err != nil {
		return "", nil, err
	}
	node, err := access.Edges.NodeOrErr()
	if err != nil || access.WorkspacePath == nil {
		return "", nil, &ValidationError{Message: "trusted workspace authorization is invalid"}
	}
	image := requestedImage
	if image == "" && environmentRecord.ImageUUID != "workspace:any-public-image" {
		image = environmentRecord.ImageUUID
	}
	if image == "" || len(image) > 512 || strings.Contains(image, "://") || !proposalWorkspaceImage.MatchString(image) {
		return "", nil, &ValidationError{Message: "image must be a public OCI image name, tag, or sha256 digest for trusted workspace execution"}
	}
	datasetRecords, err := s.client.WorkspaceDataset.Query().Where(
		workspacedataset.ProjectIDEQ(projectRecord.ID), workspacedataset.NodeIDEQ(node.ID), workspacedataset.StatusEQ(workspacedataset.StatusActive),
	).Order(ent.Asc(workspacedataset.FieldName)).All(ctx)
	if err != nil {
		return "", nil, err
	}
	return image, &proposalWorkspace{
		nodeID: node.PublicID.String(), nodeLabel: node.Label, path: *access.WorkspacePath,
		datasets: proposalWorkspaceDatasets(datasetRecords),
	}, nil
}

func (s *Service) proposalChecks(ctx context.Context, resolved proposalResolved) []ProposalCheck {
	checks := []ProposalCheck{}
	add := func(id, status, summary, detail string) {
		checks = append(checks, ProposalCheck{ID: id, Status: status, Summary: summary, Detail: detail})
	}
	status, err := s.runtimeReader.Status(ctx)
	if err != nil {
		add("runtime", ProposalCheckFail, "Runtime status query failed", proposalBounded(err.Error(), 240))
	} else {
		if !status.SchedulerEnabled {
			add("scheduler", ProposalCheckWarn, "Scheduler dispatch is disabled", "The Experiment will remain queued until GEMCP_SCHEDULER_ENABLED=true is set and Gemcp is restarted.")
		} else if !status.SchedulerHealthy {
			add("scheduler", ProposalCheckFail, "Scheduler heartbeat is stale", "Dispatch is enabled, but the scheduler worker heartbeat is not current. Restart Gemcp and verify the scheduler worker is running.")
		} else {
			add("scheduler", ProposalCheckPass, "Scheduler is healthy", fmt.Sprintf("Global concurrency is %d.", status.GlobalConcurrency))
		}
		if resolved.profile.Backend == resourceprofile.BackendSSHCloud {
			add("public_url", ProposalCheckPass, "Cloud SSH does not require inbound callbacks", "The control plane opens outbound SSH and observes the host process it starts.")
			if !s.proposalConfig.SSHCloudEnabled || !status.SSHCloudEnabled {
				add("ssh_cloud", ProposalCheckFail, "Cloud SSH execution is disabled", "")
			} else {
				add("ssh_cloud", ProposalCheckWarn, "Cloud SSH is experimental", sshCloudAgentWarning)
			}
		} else if !status.PublicURLConfigured {
			add("public_url", ProposalCheckFail, "Public callback URL is not configured", "Runner and Node callbacks require the configured HTTPS public URL.")
		} else if !unmeteredBackend(resolved.profile.Backend) && !status.PublicURLHTTPS {
			add("public_url", ProposalCheckFail, "AutoDL requires an HTTPS public URL", "Loopback HTTP is enough for Cloud SSH dispatch, but AutoDL Runner callbacks still need HTTPS.")
		} else {
			add("public_url", ProposalCheckPass, "Public callback URL is configured", "")
		}
		if !unmeteredBackend(resolved.profile.Backend) {
			if !status.WatchdogHealthy {
				add("watchdog", ProposalCheckFail, "Watchdog heartbeat is stale", "Paid AutoDL cleanup enforcement must be healthy.")
			} else {
				add("watchdog", ProposalCheckPass, "Watchdog is healthy", "")
			}
		} else if resolved.profile.Backend == resourceprofile.BackendSelfHosted && (!s.proposalConfig.SelfHostedEnabled || !status.SelfHostedEnabled) {
			add("self_hosted", ProposalCheckFail, "Self-hosted execution is disabled", "")
		} else if resolved.profile.Backend == resourceprofile.BackendSelfHosted {
			add("self_hosted", ProposalCheckPass, "Self-hosted execution is enabled", "")
		}
	}
	active, activeErr := s.client.Experiment.Query().Where(entexperiment.ProjectIDEQ(resolved.project.ID), entexperiment.StateIn("provisioning", "running", "cancelling", "collecting")).Count(ctx)
	if activeErr != nil {
		add("concurrency", ProposalCheckFail, "Project concurrency could not be checked", proposalBounded(activeErr.Error(), 240))
	} else if active >= resolved.project.MaxConcurrency {
		add("concurrency", ProposalCheckWarn, "Project concurrency is currently full", "The Experiment can remain queued until a slot is available.")
	} else {
		add("concurrency", ProposalCheckPass, "Project has an available concurrency slot", fmt.Sprintf("%d of %d slots are active.", active, resolved.project.MaxConcurrency))
	}
	if resolved.repository == nil || resolved.profile.Backend == resourceprofile.BackendSSHCloud {
		add("source_archive", ProposalCheckPass, "Cloud SSH does not upload a Git archive", "The Agent prepares the host working directory outside Gemcp.")
	} else {
		archiveCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		archive, archiveErr := s.archiver.ArchiveCommit(archiveCtx, resolved.repository.ID, resolved.commitSHA, s.proposalConfig.SourceMaxBytes)
		cancel()
		if archiveErr != nil {
			add("source_archive", ProposalCheckFail, "Commit archive could not be generated", proposalBounded(archiveErr.Error(), 320))
		} else {
			inspection, inspectErr := sourcearchive.Inspect(archive, s.proposalConfig.SourceMaxBytes)
			closeErr := archive.Close()
			if inspectErr != nil {
				add("source_archive", ProposalCheckFail, "Commit archive failed safety validation", proposalBounded(inspectErr.Error(), 320))
			} else {
				add("source_archive", ProposalCheckPass, "Commit archive is readable and safe", fmt.Sprintf("%d entries, %d compressed bytes, %d payload bytes.", inspection.Entries, inspection.CompressedBytes, inspection.PayloadBytes))
			}
			if closeErr != nil {
				add("source_cleanup", ProposalCheckWarn, "Temporary archive cleanup reported an error", proposalBounded(closeErr.Error(), 240))
			}
		}
	}
	s.proposalBudgetCheck(ctx, resolved, add)
	switch resolved.profile.Backend {
	case resourceprofile.BackendSelfHosted:
		s.proposalSelfHostedCheck(ctx, resolved, add)
	case resourceprofile.BackendSSHCloud:
		s.proposalSSHCloudCheck(ctx, resolved, add)
	default:
		s.proposalAutoDLCheck(ctx, resolved, add)
	}
	return checks
}

func (s *Service) proposalBudgetCheck(ctx context.Context, resolved proposalResolved, add func(string, string, string, string)) {
	if unmeteredBackend(resolved.profile.Backend) {
		add("budget", ProposalCheckPass, "This execution backend is unmetered", "Gemcp records a zero-CNY reservation.")
		return
	}
	if resolved.reservation > resolved.project.MaxExperimentMilli {
		add("budget", ProposalCheckFail, "Proposal exceeds the Project experiment cap", fmt.Sprintf("Reservation is %d milli-CNY; cap is %d.", resolved.reservation, resolved.project.MaxExperimentMilli))
		return
	}
	period, err := budgetPeriod(s.now().UTC(), resolved.project.Timezone)
	if err != nil {
		add("budget", ProposalCheckFail, "Project billing timezone is invalid", proposalBounded(err.Error(), 240))
		return
	}
	entries, err := s.client.BudgetEntry.Query().Where(budgetentry.ProjectIDEQ(resolved.project.ID), budgetentry.PeriodEQ(period)).All(ctx)
	if err != nil {
		add("budget", ProposalCheckFail, "Budget ledger could not be read", proposalBounded(err.Error(), 240))
		return
	}
	committed := int64(0)
	for _, entry := range entries {
		if (entry.AmountMilli > 0 && committed > math.MaxInt64-entry.AmountMilli) || (entry.AmountMilli < 0 && committed < math.MinInt64-entry.AmountMilli) {
			add("budget", ProposalCheckFail, "Budget ledger overflowed", "")
			return
		}
		committed += entry.AmountMilli
	}
	available := resolved.project.MonthlyBudgetMilli - committed
	if available < resolved.reservation {
		add("budget", ProposalCheckFail, "Project budget cannot reserve the proposal", fmt.Sprintf("Available is %d milli-CNY; reservation requires %d.", max64(available, 0), resolved.reservation))
		return
	}
	add("budget", ProposalCheckPass, "Project budget can reserve the proposal", fmt.Sprintf("Worst-case reservation is %d milli-CNY; %d remains available before reservation.", resolved.reservation, available))
}

func (s *Service) proposalAutoDLCheck(ctx context.Context, resolved proposalResolved, add func(string, string, string, string)) {
	if s.providerReader == nil {
		add("provider", ProposalCheckFail, "AutoDL Provider service is unavailable", "")
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	expectedBackend := "private"
	if resolved.profile.Backend == resourceprofile.BackendAutodlElastic {
		expectedBackend = "elastic"
	}
	snapshot, err := s.providerReader.QueryResources(queryCtx, resolved.project.TenantID, expectedBackend)
	cancel()
	if err != nil {
		add("provider", ProposalCheckFail, "AutoDL Provider query failed", proposalBounded(err.Error(), 320))
		return
	}
	if snapshot.Provider.Backend != expectedBackend {
		add("provider", ProposalCheckFail, "AutoDL Provider backend does not match the selected resource", fmt.Sprintf("Configured Provider is %s; selected resource requires %s.", snapshot.Provider.Backend, expectedBackend))
		return
	}
	add("provider", ProposalCheckPass, "AutoDL Provider is reachable", snapshot.Provider.Name)
	idle := 0
	details := []string{}
	for _, stock := range snapshot.GPUStock {
		if resolved.profile.Backend == resourceprofile.BackendAutodlElastic && stock.Region != resolved.profile.Region {
			continue
		}
		for _, accepted := range resolved.profile.GpuNames {
			if strings.EqualFold(strings.TrimSpace(stock.Name), strings.TrimSpace(accepted)) {
				idle += stock.Idle
				details = append(details, fmt.Sprintf("%s: %d idle", stock.Name, stock.Idle))
				break
			}
		}
	}
	if idle < resolved.profile.GpuNum {
		add("gpu_capacity", ProposalCheckFail, "Selected AutoDL GPU capacity is unavailable", strings.Join(details, ", "))
	} else if resolved.profile.Backend == resourceprofile.BackendAutodlElastic && resolved.profile.GpuNum > 1 {
		add("gpu_capacity", ProposalCheckWarn, "Public Elastic reports enough individual GPUs", strings.Join(details, ", ")+"; inventory does not guarantee that multiple GPUs are available on one machine.")
	} else {
		add("gpu_capacity", ProposalCheckPass, "Selected AutoDL GPU capacity is available", strings.Join(details, ", "))
	}
	imageFound := false
	for _, image := range append(append([]provider.Image{}, snapshot.PrivateImages...), snapshot.SystemImages...) {
		if image.UUID == resolved.environment.ImageUUID {
			imageFound = true
			break
		}
	}
	if imageFound {
		add("image", ProposalCheckPass, "Selected AutoDL image is visible", resolved.environment.ImageUUID)
	} else {
		add("image", ProposalCheckWarn, "Selected AutoDL image was not visible in discovery", "Provider create remains authoritative for public base images not returned by image-list endpoints.")
	}
	if len(resolved.bindings) == 0 {
		if resolved.preset == "smoke" {
			add("dataset_bindings", ProposalCheckWarn, "No AutoDL dataset binding is registered", "Identity smoke can proceed. Probe and train require a Project dataset binding under /root/autodl-fs/.")
			return
		}
		add("dataset_bindings", ProposalCheckFail, "No AutoDL dataset binding is registered", "Register a Project dataset binding before probe or train on Public Elastic or Private Cloud.")
		return
	}
	bindingDetails := make([]string, 0, len(resolved.bindings))
	for _, binding := range resolved.bindings {
		bindingDetails = append(bindingDetails, binding.EnvironmentVariable+"="+binding.CanonicalRoot)
	}
	add("dataset_bindings", ProposalCheckWarn, "Registered AutoDL dataset bindings will be injected", strings.Join(bindingDetails, ", ")+". AutoDL does not provide a mount sandbox; the Runner fails closed if a root or marker is missing.")
}

func (s *Service) proposalSelfHostedCheck(ctx context.Context, resolved proposalResolved, add func(string, string, string, string)) {
	if resolved.profile.GpuNum != 1 {
		add("resource_profile", ProposalCheckFail, "Self-hosted prepared Experiments require one GPU", "")
	}
	if resolved.workspace != nil && proposalWorkspaceImage.MatchString(resolved.image) {
		status := ProposalCheckPass
		summary := "Trusted workspace image is digest-pinned"
		detail := resolved.image
		if !proposalPinnedImage.MatchString(resolved.image) {
			status = ProposalCheckWarn
			summary = "Trusted workspace image uses a mutable name or tag"
			detail = "The Owner-approved permissive policy allows this reference. The Node will record the resolved digest after a successful run."
		}
		add("image", status, summary, detail)
		add("workspace", ProposalCheckWarn, "Trusted host workspace will be mounted read-write", resolved.workspace.path+" on "+resolved.workspace.nodeLabel+".")
		if len(resolved.workspace.datasets) > 0 {
			paths := make([]string, 0, len(resolved.workspace.datasets))
			for _, dataset := range resolved.workspace.datasets {
				paths = append(paths, dataset.EnvironmentVariable+"=/gemcp/workspace/"+dataset.RelativePath)
			}
			add("workspace_datasets", ProposalCheckWarn, "Registered workspace datasets will be exposed", strings.Join(paths, ", "))
		}
	} else if !proposalPinnedImage.MatchString(resolved.image) {
		add("image", ProposalCheckFail, "Self-hosted image is not digest-pinned", "Use a public Linux AMD64 image with an @sha256 digest.")
	} else {
		add("image", ProposalCheckPass, "Self-hosted image is digest-pinned", resolved.image)
	}
	nodes, err := s.client.SelfHostedNode.Query().Where(
		selfhostednode.TenantIDEQ(resolved.project.TenantID), selfhostednode.StatusEQ(selfhostednode.StatusActive),
		selfhostednode.ObservedStateEQ(selfhostednode.ObservedStateOnline), selfhostednode.LastSeenAtNotNil(),
		selfhostednode.LastSeenAtGT(s.now().UTC().Add(-s.proposalConfig.NodeStaleAfter)),
		selfhostednode.HasProjectAccessWith(nodeprojectaccess.ProjectIDEQ(resolved.project.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive)),
	).Order(ent.Asc(selfhostednode.FieldID)).All(ctx)
	if err != nil {
		add("node", ProposalCheckFail, "Eligible Nodes could not be queried", proposalBounded(err.Error(), 240))
		return
	}
	for _, node := range nodes {
		if resolved.workspace != nil && (node.PublicID.String() != resolved.workspace.nodeID || !proposalNodeSupportsWorkspace(node.Capabilities) ||
			len(resolved.workspace.datasets) > 0 && !proposalNodeSupportsDatasets(node.Capabilities)) {
			continue
		}
		if !proposalNodeSupportsArgv(node.Capabilities) || !proposalNodeMatchesGPU(node.Capabilities, resolved.profile.GpuNames) {
			continue
		}
		busy, err := s.client.NodeAssignment.Query().Where(
			nodeassignment.NodeIDEQ(node.ID), nodeassignment.StateIn(nodeassignment.StateStarting, nodeassignment.StateRunning, nodeassignment.StateStopping, nodeassignment.StateCollecting),
		).Exist(ctx)
		if err != nil {
			add("node", ProposalCheckFail, "Node Assignment state could not be queried", proposalBounded(err.Error(), 240))
			return
		}
		if !busy {
			add("node", ProposalCheckPass, "An argv-capable authorized Node is ready", fmt.Sprintf("%s (%s).", node.Label, node.PublicID.String()))
			return
		}
	}
	add("node", ProposalCheckFail, "No compatible authorized Node matches the profile", "Upgrade gemcp-node when trusted workspace capability is required, verify heartbeat and Project access, and confirm the GPU is idle.")
}

func (s *Service) proposalSSHCloudCheck(ctx context.Context, resolved proposalResolved, add func(string, string, string, string)) {
	add("isolation", ProposalCheckWarn, "Cloud SSH has no container isolation", "The command runs as a host process in the login environment. Gemcp does not lock an image, dataset, or conda prefix.")
	if resolved.sshHost != "" {
		add("host", ProposalCheckPass, "Command will start on the registered host", fmt.Sprintf("%s@%s cwd=%s", resolved.sshUser, resolved.sshHost, proposalWorkingDirectory(resolved)))
	}
	nodes, err := s.client.CloudSSHNode.Query().Where(
		cloudsshnode.TenantIDEQ(resolved.project.TenantID), cloudsshnode.StatusEQ(cloudsshnode.StatusActive),
		cloudsshnode.HasProjectAccessWith(cloudsshprojectaccess.ProjectIDEQ(resolved.project.ID), cloudsshprojectaccess.StatusEQ(cloudsshprojectaccess.StatusActive)),
	).Order(ent.Asc(cloudsshnode.FieldID)).All(ctx)
	if err != nil {
		add("node", ProposalCheckFail, "Cloud SSH nodes could not be queried", proposalBounded(err.Error(), 240))
		return
	}
	for _, node := range nodes {
		busy, busyErr := s.client.CloudSSHAssignment.Query().Where(
			cloudsshassignment.NodeIDEQ(node.ID),
			cloudsshassignment.StateIn(cloudsshassignment.StateStarting, cloudsshassignment.StateRunning, cloudsshassignment.StateStopping, cloudsshassignment.StateCollecting),
		).Exist(ctx)
		if busyErr != nil {
			add("node", ProposalCheckFail, "Cloud SSH Assignment state could not be queried", proposalBounded(busyErr.Error(), 240))
			return
		}
		if !busy {
			add("node", ProposalCheckPass, "An authorized Cloud SSH node is ready", fmt.Sprintf("%s (%s).", node.Label, node.PublicID.String()))
			return
		}
	}
	add("node", ProposalCheckFail, "No authorized Cloud SSH node is idle", "Register or probe a host and wait for the current Assignment to finish.")
}

func proposalNodeSupportsArgv(capabilities map[string]any) bool {
	switch values := capabilities["execution_modes"].(type) {
	case []any:
		for _, value := range values {
			if candidate, _ := value.(string); candidate == executioncmd.ModeArgv {
				return true
			}
		}
	case []string:
		for _, candidate := range values {
			if candidate == executioncmd.ModeArgv {
				return true
			}
		}
	}
	return false
}

func proposalNodeSupportsWorkspace(capabilities map[string]any) bool {
	switch values := capabilities["workspace_modes"].(type) {
	case []any:
		for _, value := range values {
			if value == "trusted_rw" {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if value == "trusted_rw" {
				return true
			}
		}
	}
	return false
}

func proposalNodeSupportsDatasets(capabilities map[string]any) bool {
	switch values := capabilities["dataset_modes"].(type) {
	case []any:
		for _, value := range values {
			if value == "workspace_env_v1" {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if value == "workspace_env_v1" {
				return true
			}
		}
	}
	return false
}

func proposalGPUValues(capabilities map[string]any) []any {
	switch values := capabilities["gpus"].(type) {
	case []any:
		return values
	case []map[string]any:
		result := make([]any, len(values))
		for index, value := range values {
			result[index] = value
		}
		return result
	default:
		return nil
	}
}

func proposalNodeMatchesGPU(capabilities map[string]any, accepted []string) bool {
	values := proposalGPUValues(capabilities)
	if len(values) != 1 {
		return false
	}
	gpu, ok := values[0].(map[string]any)
	if !ok {
		return false
	}
	name, _ := gpu["name"].(string)
	for _, candidate := range accepted {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func proposalDigest(resolved proposalResolved) string {
	material := struct {
		ProposalID       string            `json:"proposal_id"`
		Project          map[string]any    `json:"project"`
		Repository       map[string]any    `json:"repository"`
		Environment      map[string]any    `json:"environment"`
		Resource         map[string]any    `json:"resource"`
		RequestedRef     string            `json:"requested_ref"`
		CommitSHA        string            `json:"commit_sha"`
		Execution        executioncmd.Spec `json:"execution"`
		RuntimePreset    string            `json:"runtime_preset"`
		RuntimeSeconds   int               `json:"runtime_seconds"`
		ReservationMilli int64             `json:"reservation_milli"`
		FromNodeID       string            `json:"from_node_id,omitempty"`
		ExpectedMetric   string            `json:"expected_metric,omitempty"`
		ExpiresAt        time.Time         `json:"expires_at"`
		Host             string            `json:"host,omitempty"`
		User             string            `json:"user,omitempty"`
		WorkingDirectory string            `json:"working_directory,omitempty"`
		Isolation        string            `json:"isolation,omitempty"`
	}{
		ProposalID: resolved.id.String(), Project: proposalProjectSnapshot(resolved.project),
		Repository:  repositorySnapshot(resolved.repository, resolved.project.PublicID.String()),
		Environment: proposalEnvironmentSnapshot(resolved), Resource: resourceSnapshot(resolved.profile),
		RequestedRef: resolved.ref, CommitSHA: resolved.commitSHA, Execution: resolved.execution,
		RuntimePreset: resolved.preset, RuntimeSeconds: resolved.runtime, ReservationMilli: resolved.reservation,
		FromNodeID: resolved.fromNodeID, ExpectedMetric: resolved.expectedMetric,
		ExpiresAt: resolved.expiresAt.UTC().Truncate(time.Microsecond),
		Host:      resolved.sshHost, User: resolved.sshUser, WorkingDirectory: proposalWorkingDirectory(resolved),
		Isolation: proposalIsolation(resolved),
	}
	encoded, _ := json.Marshal(material)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest[:])
}

func proposalProjectSnapshot(record *ent.Project) map[string]any {
	return map[string]any{
		"id": record.PublicID.String(), "status": record.Status, "monthly_budget_milli": record.MonthlyBudgetMilli,
		"max_experiment_milli": record.MaxExperimentMilli, "max_concurrency": record.MaxConcurrency,
		"max_runtime_seconds": record.MaxRuntimeSeconds, "timeout_extension_seconds": record.TimeoutExtensionSeconds,
		"termination_grace_seconds": record.TerminationGraceSeconds, "timezone": record.Timezone,
	}
}

func preparedProposal(resolved proposalResolved, digest string, createdAt time.Time) PreparedProposal {
	repo := ProposalRepository{RequestedRef: resolved.ref, CommitSHA: resolved.commitSHA}
	if resolved.repository != nil {
		repo = ProposalRepository{
			ID: resolved.repository.PublicID.String(), Name: resolved.repository.Name, SSHURL: resolved.repository.SSHURL,
			HostKeyFingerprint: resolved.repository.HostKeyFingerprint, RequestedRef: resolved.ref,
			CommitSHA: resolved.commitSHA, DefaultBranch: resolved.repository.DefaultBranch,
		}
	}
	return PreparedProposal{
		ID: resolved.id.String(), ProjectID: resolved.project.PublicID.String(), Eligible: proposalChecksEligible(resolved.checks), RequiresConfirmation: true,
		Repository: repo,
		Execution:  ProposalExecution{Mode: executioncmd.ModeArgv, Argv: append([]string(nil), resolved.execution.Argv...), DisplayCommand: executioncmd.DisplayArgv(resolved.execution.Argv)},
		Resource: ProposalResource{
			EnvironmentID: resolved.environment.PublicID.String(), EnvironmentName: resolved.environment.Name,
			ResourceProfileID: resolved.profile.PublicID.String(), ResourceProfileName: resolved.profile.Name,
			Backend: string(resolved.profile.Backend), Image: resolved.image,
			GPUModels: append([]string(nil), resolved.profile.GpuNames...), GPUNum: resolved.profile.GpuNum,
			Region: resolved.profile.Region, CUDAFrom: resolved.profile.CudaFrom, CUDATo: resolved.profile.CudaTo,
			CPUFrom: resolved.profile.CPUFrom, CPUTo: resolved.profile.CPUTo,
			MemoryFromGB: resolved.profile.MemoryFromGB, MemoryToGB: resolved.profile.MemoryToGB,
			PriceFromMilli: resolved.profile.PriceFromMilli, PriceToMilli: resolved.profile.PriceToMilli,
			ReuseContainer: resolved.profile.ReuseContainer, Billable: !unmeteredBackend(resolved.profile.Backend),
			ExecutionPolicy: proposalExecutionPolicy(resolved), WorkspacePath: proposalWorkspacePath(resolved),
			NodeID: proposalWorkspaceNodeID(resolved), NodeLabel: proposalWorkspaceNodeLabel(resolved),
			Host: resolved.sshHost, User: resolved.sshUser, WorkingDirectory: proposalWorkingDirectory(resolved),
			Isolation:         proposalIsolation(resolved),
			ImageMutable:      resolved.workspace != nil && !proposalPinnedImage.MatchString(resolved.image),
			WorkspaceDatasets: proposalWorkspaceDatasetsCopy(resolved.workspace),
			DatasetBindings:   proposalDatasetBindingsCopy(resolved.bindings),
		},
		RuntimePreset: resolved.preset, MaxRuntimeSeconds: resolved.runtime,
		TimeoutExtensionSeconds: resolved.project.TimeoutExtensionSeconds, TerminationGraceSeconds: resolved.project.TerminationGraceSeconds,
		ReservedCostMilli: resolved.reservation, ReservedCostCNY: milliCNY(resolved.reservation), Checks: append([]ProposalCheck(nil), resolved.checks...),
		ConfirmationDigest: digest, FromNodeID: resolved.fromNodeID, ExpectedMetric: resolved.expectedMetric,
		ExpiresAt: resolved.expiresAt, CreatedAt: createdAt,
	}
}

func proposalEnvironmentSnapshot(resolved proposalResolved) map[string]any {
	snapshot := environmentSnapshot(resolved.environment)
	snapshot["image_uuid"] = resolved.image
	if resolved.cwd != "" {
		snapshot["working_directory"] = resolved.cwd
	}
	if resolved.sshHost != "" {
		snapshot["ssh_host"] = resolved.sshHost
		snapshot["ssh_user"] = resolved.sshUser
		snapshot["ssh_node_id"] = resolved.sshNodeID
		snapshot["ssh_node_label"] = resolved.sshNodeLabel
		snapshot["isolation"] = "none"
	}
	if resolved.workspace != nil {
		snapshot["execution_policy"] = "trusted_workspace"
		snapshot["workspace_path"] = resolved.workspace.path
		snapshot["workspace_node_id"] = resolved.workspace.nodeID
		snapshot["workspace_node_label"] = resolved.workspace.nodeLabel
		snapshot["workspace_datasets"] = proposalWorkspaceDatasetsCopy(resolved.workspace)
	}
	if resolved.profile != nil && (resolved.profile.Backend == resourceprofile.BackendAutodlElastic || resolved.profile.Backend == resourceprofile.BackendAutodlPrivate) {
		snapshot["dataset_bindings"] = datasetcatalog.Snapshot(resolved.bindings)
	}
	return snapshot
}

func (s *Service) attachProposalBindings(ctx context.Context, resolved *proposalResolved) error {
	bindings, err := queryActiveDatasetBindings(ctx, s.client.DatasetBinding.Query(), resolved.project.ID, string(resolved.profile.Backend))
	if err != nil {
		return err
	}
	resolved.bindings = bindings
	return nil
}

func queryActiveDatasetBindings(ctx context.Context, query *ent.DatasetBindingQuery, projectID int, backend string) ([]datasetcatalog.View, error) {
	if backend != string(resourceprofile.BackendAutodlElastic) && backend != string(resourceprofile.BackendAutodlPrivate) {
		return nil, nil
	}
	records, err := query.Where(
		datasetbinding.ProjectIDEQ(projectID),
		datasetbinding.BackendEQ(datasetbinding.Backend(backend)),
		datasetbinding.StatusEQ(datasetbinding.StatusActive),
	).Order(ent.Asc(datasetbinding.FieldName)).All(ctx)
	if err != nil {
		return nil, err
	}
	return datasetcatalog.ViewsFromRecords(records, ""), nil
}

func proposalDatasetBindingsCopy(views []datasetcatalog.View) []ProposalDatasetBinding {
	if len(views) == 0 {
		return nil
	}
	result := make([]ProposalDatasetBinding, 0, len(views))
	for _, view := range views {
		result = append(result, ProposalDatasetBinding{
			ID: view.ID, Name: view.Name, Backend: view.Backend, CanonicalRoot: view.CanonicalRoot,
			EnvironmentVariable: view.EnvironmentVariable, RequiredMarkers: append([]string(nil), view.RequiredMarkers...),
		})
	}
	return result
}

func normalizeProposalCwd(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\x00") {
		return "", &ValidationError{Message: "cwd must be an absolute remote path"}
	}
	return value, nil
}

func (s *Service) attachSSHCloudTarget(ctx context.Context, projectRecord *ent.Project, environmentRecord *ent.Environment, resolved *proposalResolved) error {
	recipe := strings.TrimSpace(environmentRecord.RecipeRef)
	if !strings.HasPrefix(recipe, sshCloudRecipePrefix) {
		return nil
	}
	id, err := uuid.Parse(strings.TrimPrefix(recipe, sshCloudRecipePrefix))
	if err != nil {
		return nil
	}
	node, err := s.client.CloudSSHNode.Query().Where(
		cloudsshnode.PublicIDEQ(id), cloudsshnode.TenantIDEQ(projectRecord.TenantID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	resolved.sshHost = node.SSHHost
	resolved.sshUser = node.SSHUser
	resolved.sshNodeID = node.PublicID.String()
	resolved.sshNodeLabel = node.Label
	return nil
}

func proposalWorkingDirectory(resolved proposalResolved) string {
	if resolved.cwd != "" {
		return resolved.cwd
	}
	if resolved.profile != nil && resolved.profile.Backend == resourceprofile.BackendSSHCloud {
		return "$HOME"
	}
	return ""
}

func proposalIsolation(resolved proposalResolved) string {
	if resolved.profile != nil && resolved.profile.Backend == resourceprofile.BackendSSHCloud {
		return "none"
	}
	return ""
}

func proposalWorkspaceDatasets(records []*ent.WorkspaceDataset) []nodeprotocol.WorkspaceDataset {
	result := make([]nodeprotocol.WorkspaceDataset, 0, len(records))
	for _, record := range records {
		result = append(result, nodeprotocol.WorkspaceDataset{
			Name: record.Name, RelativePath: record.RelativePath, EnvironmentVariable: record.EnvironmentVariable,
		})
	}
	return result
}

func proposalWorkspaceDatasetsCopy(workspace *proposalWorkspace) []nodeprotocol.WorkspaceDataset {
	if workspace == nil {
		return nil
	}
	return append([]nodeprotocol.WorkspaceDataset(nil), workspace.datasets...)
}

func proposalExecutionPolicy(resolved proposalResolved) string {
	if resolved.workspace != nil {
		return "trusted_workspace"
	}
	return "strict"
}

func proposalWorkspacePath(resolved proposalResolved) string {
	if resolved.workspace != nil {
		return resolved.workspace.path
	}
	return proposalWorkingDirectory(resolved)
}

func proposalWorkspaceNodeID(resolved proposalResolved) string {
	if resolved.workspace != nil {
		return resolved.workspace.nodeID
	}
	return resolved.sshNodeID
}

func proposalWorkspaceNodeLabel(resolved proposalResolved) string {
	if resolved.workspace != nil {
		return resolved.workspace.nodeLabel
	}
	return resolved.sshNodeLabel
}

func proposalChecksEligible(checks []ProposalCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if check.Status != ProposalCheckPass && check.Status != ProposalCheckWarn {
			return false
		}
	}
	return true
}

func milliCNY(value int64) string { return fmt.Sprintf("%d.%03d", value/1000, value%1000) }

func proposalBounded(value string, maximum int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum]) + "..."
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
