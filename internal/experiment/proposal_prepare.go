package experiment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/environment"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/project"
	entrepository "github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/sourcearchive"
	"github.com/google/uuid"
)

const (
	defaultProposalSourceMaxBytes = int64(256 << 20)
	defaultProposalLifetime       = 30 * time.Minute
	defaultProposalNodeStaleAfter = time.Minute
	smokeRuntimeSeconds           = 300
)

var proposalPinnedImage = regexp.MustCompile(`^[^[:space:]@]+@sha256:[0-9a-f]{64}$`)

type proposalResolved struct {
	id          uuid.UUID
	project     *ent.Project
	repository  *ent.Repository
	environment *ent.Environment
	profile     *ent.ResourceProfile
	ref         string
	commitSHA   string
	execution   executioncmd.Spec
	preset      string
	runtime     int
	reservation int64
	expiresAt   time.Time
	checks      []ProposalCheck
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
	resolved, choices, err := s.resolveProposal(ctx, principal, input, executionSpec)
	if err != nil {
		return PrepareResult{}, err
	}
	if len(choices) > 0 {
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
	record, err := tx.ExperimentProposal.Create().
		SetPublicID(resolved.id).
		SetTenantID(principal.TenantID).
		SetProjectID(resolved.project.ID).
		SetAgentTokenID(principal.TokenID).
		SetRepositoryID(resolved.repository.ID).
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
		SetProjectSnapshot(proposalProjectSnapshot(resolved.project)).
		SetRepositorySnapshot(repositorySnapshot(resolved.repository, resolved.project.PublicID.String())).
		SetEnvironmentSnapshot(environmentSnapshot(resolved.environment)).
		SetResourceSnapshot(resourceSnapshot(resolved.profile)).
		SetChecks(checkMaps).
		SetReservedCostMilli(resolved.reservation).
		SetConfirmationDigest(digest).
		SetExpiresAt(resolved.expiresAt).
		Save(ctx)
	if err != nil {
		return PrepareResult{}, err
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(principal.TenantID).
		SetActorType("agent_token").
		SetActorID(principal.TokenPublicID).
		SetAction("experiment.proposal_prepared").
		SetTargetType("experiment_proposal").
		SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": resolved.project.PublicID.String(), "repository_id": resolved.repository.PublicID.String(),
			"commit_sha": resolved.commitSHA, "backend": resolved.profile.Backend, "reserved_cost_milli": resolved.reservation,
			"eligible": proposalChecksEligible(resolved.checks), "confirmation_digest": digest,
		}).Save(ctx); err != nil {
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
	repositoryRecord, choices, err := chooseProposalRepository(repositories, input.Repository, input.RepositoryRemote)
	if err != nil || len(choices) > 0 {
		return result, choices, err
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
	preset := strings.ToLower(strings.TrimSpace(input.RuntimePreset))
	if preset == "" {
		preset = "smoke"
	}
	if preset != "smoke" {
		return result, nil, &ValidationError{Message: "runtime_preset must be smoke in the prepared experiment phase-one interface"}
	}
	runtimeSeconds := input.MaxRuntimeSeconds
	if runtimeSeconds == 0 {
		runtimeSeconds = smokeRuntimeSeconds
		if projectRecord.MaxRuntimeSeconds < runtimeSeconds {
			runtimeSeconds = projectRecord.MaxRuntimeSeconds
		}
	}
	if runtimeSeconds <= 0 || runtimeSeconds > smokeRuntimeSeconds || runtimeSeconds > projectRecord.MaxRuntimeSeconds {
		return result, nil, &ValidationError{Message: fmt.Sprintf("max_runtime_seconds must be between 1 and %d for the smoke preset", minInt(smokeRuntimeSeconds, projectRecord.MaxRuntimeSeconds))}
	}
	ref := strings.TrimSpace(input.Ref)
	if ref == "" {
		ref = repositoryRecord.DefaultBranch
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	commitSHA, err := s.refResolver.ResolveRef(resolveCtx, repositoryRecord.ID, ref)
	cancel()
	if err != nil {
		return result, nil, fmt.Errorf("%w: resolve ref %q: %v", ErrCommitVerification, ref, err)
	}
	commitSHA = strings.ToLower(strings.TrimSpace(commitSHA))
	if !commitPattern.MatchString(commitSHA) {
		return result, nil, fmt.Errorf("%w: resolved ref did not return a full commit SHA", ErrCommitVerification)
	}
	reservation := int64(0)
	if profileRecord.Backend != resourceprofile.BackendSelfHosted {
		billable, err := billableRuntimeSeconds(runtimeSeconds, projectRecord.TimeoutExtensionSeconds, projectRecord.TerminationGraceSeconds)
		if err != nil {
			return result, nil, err
		}
		reservation, err = reserveCost(profileRecord.PriceToMilli, profileRecord.GpuNum, billable)
		if err != nil {
			return result, nil, err
		}
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	return proposalResolved{
		id: uuid.New(), project: projectRecord, repository: repositoryRecord, environment: environmentRecord, profile: profileRecord,
		ref: ref, commitSHA: commitSHA, execution: executionSpec, preset: preset, runtime: runtimeSeconds,
		reservation: reservation, expiresAt: now.Add(s.proposalConfig.Lifetime).Truncate(time.Microsecond),
	}, nil, nil
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

func validateProposalResource(profileRecord *ent.ResourceProfile) error {
	if len(profileRecord.GpuNames) == 0 || profileRecord.GpuNum <= 0 || profileRecord.CudaFrom > profileRecord.CudaTo ||
		profileRecord.CPUFrom > profileRecord.CPUTo || profileRecord.MemoryFromGB > profileRecord.MemoryToGB ||
		profileRecord.PriceFromMilli > profileRecord.PriceToMilli {
		return &ValidationError{Message: "resource profile has invalid or empty execution bounds"}
	}
	return nil
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
		if !status.SchedulerEnabled || !status.SchedulerHealthy {
			add("scheduler", ProposalCheckFail, "Scheduler is not ready", "Dispatch must be enabled and the scheduler heartbeat must be current.")
		} else {
			add("scheduler", ProposalCheckPass, "Scheduler is healthy", fmt.Sprintf("Global concurrency is %d.", status.GlobalConcurrency))
		}
		if !status.PublicURLConfigured {
			add("public_url", ProposalCheckFail, "Public callback URL is not configured", "Runner and Node callbacks require the configured HTTPS public URL.")
		} else {
			add("public_url", ProposalCheckPass, "Public callback URL is configured", "")
		}
		if resolved.profile.Backend == resourceprofile.BackendAutodlPrivate {
			if !status.WatchdogHealthy {
				add("watchdog", ProposalCheckFail, "Watchdog heartbeat is stale", "Paid AutoDL cleanup enforcement must be healthy.")
			} else {
				add("watchdog", ProposalCheckPass, "Watchdog is healthy", "")
			}
		} else if !s.proposalConfig.SelfHostedEnabled || !status.SelfHostedEnabled {
			add("self_hosted", ProposalCheckFail, "Self-hosted execution is disabled", "")
		} else {
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
	s.proposalBudgetCheck(ctx, resolved, add)
	if resolved.profile.Backend == resourceprofile.BackendAutodlPrivate {
		s.proposalAutoDLCheck(ctx, resolved, add)
	} else {
		s.proposalSelfHostedCheck(ctx, resolved, add)
	}
	return checks
}

func (s *Service) proposalBudgetCheck(ctx context.Context, resolved proposalResolved, add func(string, string, string, string)) {
	if resolved.profile.Backend == resourceprofile.BackendSelfHosted {
		add("budget", ProposalCheckPass, "Self-hosted execution is unmetered", "Gemcp records a zero-CNY reservation.")
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
	snapshot, err := s.providerReader.QueryResources(queryCtx, resolved.project.TenantID)
	cancel()
	if err != nil {
		add("provider", ProposalCheckFail, "AutoDL Provider query failed", proposalBounded(err.Error(), 320))
		return
	}
	add("provider", ProposalCheckPass, "AutoDL Provider is reachable", snapshot.Provider.Name)
	idle := 0
	details := []string{}
	for _, stock := range snapshot.GPUStock {
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
}

func (s *Service) proposalSelfHostedCheck(ctx context.Context, resolved proposalResolved, add func(string, string, string, string)) {
	if resolved.profile.GpuNum != 1 {
		add("resource_profile", ProposalCheckFail, "Self-hosted prepared Experiments require one GPU", "")
	}
	if !proposalPinnedImage.MatchString(resolved.environment.ImageUUID) {
		add("image", ProposalCheckFail, "Self-hosted image is not digest-pinned", "Use a public Linux AMD64 image with an @sha256 digest.")
	} else {
		add("image", ProposalCheckPass, "Self-hosted image is digest-pinned", resolved.environment.ImageUUID)
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
	add("node", ProposalCheckFail, "No argv-capable authorized Node matches the profile", "Upgrade gemcp-node, verify heartbeat and Project access, and confirm the GPU is idle.")
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
		ExpiresAt        time.Time         `json:"expires_at"`
	}{
		ProposalID: resolved.id.String(), Project: proposalProjectSnapshot(resolved.project),
		Repository:  repositorySnapshot(resolved.repository, resolved.project.PublicID.String()),
		Environment: environmentSnapshot(resolved.environment), Resource: resourceSnapshot(resolved.profile),
		RequestedRef: resolved.ref, CommitSHA: resolved.commitSHA, Execution: resolved.execution,
		RuntimePreset: resolved.preset, RuntimeSeconds: resolved.runtime, ReservationMilli: resolved.reservation, ExpiresAt: resolved.expiresAt.UTC().Truncate(time.Microsecond),
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
	return PreparedProposal{
		ID: resolved.id.String(), ProjectID: resolved.project.PublicID.String(), Eligible: proposalChecksEligible(resolved.checks), RequiresConfirmation: true,
		Repository: ProposalRepository{
			ID: resolved.repository.PublicID.String(), Name: resolved.repository.Name, SSHURL: resolved.repository.SSHURL,
			HostKeyFingerprint: resolved.repository.HostKeyFingerprint, RequestedRef: resolved.ref,
			CommitSHA: resolved.commitSHA, DefaultBranch: resolved.repository.DefaultBranch,
		},
		Execution: ProposalExecution{Mode: executioncmd.ModeArgv, Argv: append([]string(nil), resolved.execution.Argv...), DisplayCommand: executioncmd.DisplayArgv(resolved.execution.Argv)},
		Resource: ProposalResource{
			EnvironmentID: resolved.environment.PublicID.String(), EnvironmentName: resolved.environment.Name,
			ResourceProfileID: resolved.profile.PublicID.String(), ResourceProfileName: resolved.profile.Name,
			Backend: string(resolved.profile.Backend), Image: resolved.environment.ImageUUID,
			GPUModels: append([]string(nil), resolved.profile.GpuNames...), GPUNum: resolved.profile.GpuNum,
			Region: resolved.profile.Region, CUDAFrom: resolved.profile.CudaFrom, CUDATo: resolved.profile.CudaTo,
			CPUFrom: resolved.profile.CPUFrom, CPUTo: resolved.profile.CPUTo,
			MemoryFromGB: resolved.profile.MemoryFromGB, MemoryToGB: resolved.profile.MemoryToGB,
			PriceFromMilli: resolved.profile.PriceFromMilli, PriceToMilli: resolved.profile.PriceToMilli,
			ReuseContainer: resolved.profile.ReuseContainer, Billable: resolved.profile.Backend != resourceprofile.BackendSelfHosted,
		},
		RuntimePreset: resolved.preset, MaxRuntimeSeconds: resolved.runtime,
		TimeoutExtensionSeconds: resolved.project.TimeoutExtensionSeconds, TerminationGraceSeconds: resolved.project.TerminationGraceSeconds,
		ReservedCostMilli: resolved.reservation, ReservedCostCNY: milliCNY(resolved.reservation), Checks: append([]ProposalCheck(nil), resolved.checks...),
		ConfirmationDigest: digest, ExpiresAt: resolved.expiresAt, CreatedAt: createdAt,
	}
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
