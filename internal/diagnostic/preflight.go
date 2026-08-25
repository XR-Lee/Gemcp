package diagnostic

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/internal/provider"
)

var activeExperimentStates = []string{"provisioning", "running", "cancelling", "collecting"}

func (s *Service) Preflight(ctx context.Context, tenantID int, projectID string, input PreflightInput) (Preflight, error) {
	resolved, err := s.resolve(ctx, tenantID, projectID, input)
	if err != nil {
		return Preflight{}, err
	}
	result := Preflight{
		RequiresConfirmation: true,
		GeneratedAt:          s.now().UTC(),
		Checks:               []Check{},
		Proposal:             proposalFor(resolved),
	}
	result.ConfirmationDigest = proposalDigest(resolved, result.Proposal)

	var sourceResult, backendResult Preflight
	sourceDone := make(chan struct{})
	backendDone := make(chan struct{})
	go func() {
		defer close(sourceDone)
		s.sourceCheck(ctx, resolved, &sourceResult)
	}()
	go func() {
		defer close(backendDone)
		if isAutoDLBackend(resolved.normalized.Backend) {
			s.autoDLChecks(ctx, resolved, &backendResult)
		} else {
			s.selfHostedChecks(ctx, resolved, &backendResult)
		}
	}()
	s.runtimeChecks(ctx, resolved, &result)
	s.concurrencyChecks(ctx, resolved, &result)
	s.budgetCheck(ctx, resolved, &result)
	<-sourceDone
	<-backendDone
	result.Checks = append(result.Checks, sourceResult.Checks...)
	result.Checks = append(result.Checks, backendResult.Checks...)
	result.Eligible = true
	for _, check := range result.Checks {
		if check.Status == CheckFail {
			result.Eligible = false
			break
		}
	}
	return result, nil
}

func (s *Service) runtimeChecks(ctx context.Context, resolved resolvedInput, result *Preflight) {
	if s.runtime == nil {
		addCheck(result, "runtime", CheckFail, "Runtime status is unavailable", "The diagnostic service cannot inspect scheduler health.")
		return
	}
	status, err := s.runtime.Status(ctx)
	if err != nil {
		addCheck(result, "runtime", CheckFail, "Runtime status query failed", bounded(err.Error(), 240))
		return
	}
	if !status.SchedulerEnabled {
		addCheck(result, "scheduler", CheckFail, "Scheduler dispatch is disabled", "Enable scheduler dispatch before running a backend diagnostic.")
	} else if !status.SchedulerHealthy {
		addCheck(result, "scheduler", CheckFail, "Scheduler heartbeat is stale", "Restore the scheduler before creating a diagnostic run.")
	} else {
		addCheck(result, "scheduler", CheckPass, "Scheduler is healthy", fmt.Sprintf("Global concurrency is %d.", status.GlobalConcurrency))
	}
	globalActive, countErr := s.client.Experiment.Query().Where(entexperiment.StateIn(activeExperimentStates...)).Count(ctx)
	if countErr != nil {
		addCheck(result, "global_concurrency", CheckFail, "Global concurrency could not be checked", bounded(countErr.Error(), 240))
	} else if globalActive >= status.GlobalConcurrency {
		addCheck(result, "global_concurrency", CheckWarn, "Global concurrency is currently full", "The diagnostic can remain queued until a global execution slot is available.")
	} else {
		addCheck(result, "global_concurrency", CheckPass, "A global execution slot is available", fmt.Sprintf("%d of %d slots are active.", globalActive, status.GlobalConcurrency))
	}
	if !status.PublicURLConfigured {
		addCheck(result, "public_url", CheckFail, "Public callback URL is not configured", "Runner and Node source callbacks require the configured HTTPS public URL.")
	} else {
		addCheck(result, "public_url", CheckPass, "Public callback URL is configured", "")
	}
	if isAutoDLBackend(resolved.normalized.Backend) {
		if !status.WatchdogHealthy {
			addCheck(result, "watchdog", CheckFail, "Watchdog heartbeat is stale", "AutoDL cleanup enforcement must be healthy before a paid diagnostic.")
		} else {
			addCheck(result, "watchdog", CheckPass, "Watchdog is healthy", "")
		}
	} else if !s.config.SelfHostedEnabled {
		addCheck(result, "self_hosted_feature", CheckFail, "Self-hosted execution is disabled", "Enable GEMCP_SELF_HOSTED_ENABLED before testing a Node.")
	} else {
		addCheck(result, "self_hosted_feature", CheckPass, "Self-hosted execution is enabled", "")
	}
}

func (s *Service) concurrencyChecks(ctx context.Context, resolved resolvedInput, result *Preflight) {
	projectActive, err := s.client.Experiment.Query().Where(
		entexperiment.ProjectIDEQ(resolved.project.ID), entexperiment.StateIn(activeExperimentStates...),
	).Count(ctx)
	if err != nil {
		addCheck(result, "project_concurrency", CheckFail, "Project concurrency could not be checked", bounded(err.Error(), 240))
		return
	}
	queued, err := s.client.Experiment.Query().Where(
		entexperiment.ProjectIDEQ(resolved.project.ID), entexperiment.StateEQ("queued"), entexperiment.DesiredStateEQ("running"),
	).Count(ctx)
	if err != nil {
		addCheck(result, "project_concurrency", CheckFail, "Project queue could not be checked", bounded(err.Error(), 240))
		return
	}
	if projectActive >= resolved.project.MaxConcurrency {
		addCheck(result, "project_concurrency", CheckWarn, "Project concurrency is currently full", "The diagnostic can be queued but will not dispatch until a slot is available.")
	} else {
		addCheck(result, "project_concurrency", CheckPass, "Project has an available concurrency slot", fmt.Sprintf("%d of %d slots are active; %d Experiments are already queued.", projectActive, resolved.project.MaxConcurrency, queued))
	}
}

func (s *Service) sourceCheck(ctx context.Context, resolved resolvedInput, result *Preflight) {
	if s.archiver == nil {
		addCheck(result, "source_archive", CheckFail, "Repository archiver is unavailable", "")
		return
	}
	archiveCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	archive, err := s.archiver.ArchiveCommit(archiveCtx, resolved.repository.ID, resolved.normalized.CommitSHA, s.config.SourceMaxBytes)
	cancel()
	if err != nil {
		addCheck(result, "source_archive", CheckFail, "Commit archive could not be generated", bounded(err.Error(), 320))
		return
	}
	inspection, inspectErr := inspectArchive(archive, s.config.SourceMaxBytes)
	closeErr := archive.Close()
	if inspectErr != nil {
		addCheck(result, "source_archive", CheckFail, "Commit archive failed safety validation", bounded(inspectErr.Error(), 320))
		return
	}
	if closeErr != nil {
		addCheck(result, "source_archive", CheckWarn, "Temporary archive cleanup reported an error", bounded(closeErr.Error(), 240))
	}
	addCheck(result, "source_archive", CheckPass, "Commit archive is readable and safe", fmt.Sprintf("%d entries, %d compressed bytes, %d payload bytes.", inspection.Entries, inspection.CompressedBytes, inspection.PayloadBytes))
}

func (s *Service) budgetCheck(ctx context.Context, resolved resolvedInput, result *Preflight) {
	if resolved.normalized.Backend == BackendSelfHosted {
		addCheck(result, "budget", CheckPass, "Self-hosted diagnostic is unmetered", "Gemcp records a zero-CNY reservation.")
		return
	}
	if resolved.reservation > resolved.project.MaxExperimentMilli {
		addCheck(result, "budget", CheckFail, "Diagnostic exceeds the Project experiment cap", fmt.Sprintf("Reservation is %d milli-CNY; cap is %d.", resolved.reservation, resolved.project.MaxExperimentMilli))
		return
	}
	period, err := currentPeriod(s.now().UTC(), resolved.project.Timezone)
	if err != nil {
		addCheck(result, "budget", CheckFail, "Project billing timezone is invalid", bounded(err.Error(), 240))
		return
	}
	entries, err := s.client.BudgetEntry.Query().Where(
		budgetentry.ProjectIDEQ(resolved.project.ID), budgetentry.PeriodEQ(period),
	).All(ctx)
	if err != nil {
		addCheck(result, "budget", CheckFail, "Budget ledger could not be read", bounded(err.Error(), 240))
		return
	}
	committed := int64(0)
	for _, entry := range entries {
		if (entry.AmountMilli > 0 && committed > math.MaxInt64-entry.AmountMilli) || (entry.AmountMilli < 0 && committed < math.MinInt64-entry.AmountMilli) {
			addCheck(result, "budget", CheckFail, "Budget ledger overflowed", "")
			return
		}
		committed += entry.AmountMilli
	}
	available := resolved.project.MonthlyBudgetMilli - committed
	if available < resolved.reservation {
		addCheck(result, "budget", CheckFail, "Project budget cannot reserve this diagnostic", fmt.Sprintf("Available is %d milli-CNY; reservation requires %d.", maxInt64(available, 0), resolved.reservation))
		return
	}
	addCheck(result, "budget", CheckPass, "Project budget can reserve the diagnostic", fmt.Sprintf("Worst-case reservation is %d milli-CNY; %d remains available before reservation.", resolved.reservation, available))
}

func (s *Service) autoDLChecks(ctx context.Context, resolved resolvedInput, result *Preflight) {
	if s.provider == nil {
		addCheck(result, "provider", CheckFail, "AutoDL Provider service is unavailable", "")
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	expectedBackend := "private"
	if resolved.normalized.Backend == BackendAutoDLElastic {
		expectedBackend = "elastic"
	}
	snapshot, err := s.provider.QueryResources(queryCtx, resolved.project.TenantID, expectedBackend)
	cancel()
	if err != nil {
		addCheck(result, "provider", CheckFail, "AutoDL Provider query failed", bounded(err.Error(), 320))
		return
	}
	if snapshot.Provider.Backend != expectedBackend {
		addCheck(result, "provider", CheckFail, "AutoDL Provider backend does not match the diagnostic", fmt.Sprintf("Configured Provider is %s; diagnostic requires %s.", snapshot.Provider.Backend, expectedBackend))
		return
	}
	addCheck(result, "provider", CheckPass, "AutoDL Provider credential and Developer API are reachable", snapshot.Provider.Name)
	idle := 0
	matched := []string{}
	for _, stock := range snapshot.GPUStock {
		if resolved.normalized.Backend == BackendAutoDLElastic && stock.Region != resolved.profile.Region {
			continue
		}
		matchesProfile := false
		for _, accepted := range resolved.profile.GpuNames {
			if strings.EqualFold(strings.TrimSpace(stock.Name), strings.TrimSpace(accepted)) {
				matchesProfile = true
				break
			}
		}
		if matchesProfile {
			idle += stock.Idle
			matched = append(matched, fmt.Sprintf("%s: %d idle", stock.Name, stock.Idle))
		}
	}
	if idle < resolved.profile.GpuNum {
		addCheck(result, "gpu_capacity", CheckFail, "Selected AutoDL GPU capacity is unavailable", strings.Join(matched, ", "))
	} else if resolved.normalized.Backend == BackendAutoDLElastic && resolved.profile.GpuNum > 1 {
		addCheck(result, "gpu_capacity", CheckWarn, "Public Elastic reports enough individual GPUs", strings.Join(matched, ", ")+"; inventory does not guarantee that multiple GPUs are available on one machine.")
	} else {
		addCheck(result, "gpu_capacity", CheckPass, "Selected AutoDL GPU capacity is available", strings.Join(matched, ", "))
	}
	imageFound := false
	for _, image := range append(append([]provider.Image{}, snapshot.PrivateImages...), snapshot.SystemImages...) {
		if image.UUID == resolved.environment.ImageUUID {
			imageFound = true
			break
		}
	}
	if imageFound {
		addCheck(result, "image", CheckPass, "Selected AutoDL image is visible to the Provider API", resolved.environment.ImageUUID)
	} else {
		detail := "The image may be a public base image that is not returned by the available image-list endpoints; Provider create remains authoritative."
		addCheck(result, "image", CheckWarn, "Selected AutoDL image was not visible in image discovery", detail)
	}
}

func (s *Service) selfHostedChecks(ctx context.Context, resolved resolvedInput, result *Preflight) {
	if resolved.profile.GpuNum != 1 {
		addCheck(result, "resource_profile", CheckFail, "Self-hosted diagnostics require exactly one GPU", "Create a single-GPU Self-hosted Resource Profile for this Node.")
	} else {
		addCheck(result, "resource_profile", CheckPass, "Self-hosted Resource Profile requests one GPU", "")
	}
	if !pinnedImagePattern.MatchString(resolved.environment.ImageUUID) {
		addCheck(result, "image", CheckFail, "Self-hosted image is not digest-pinned", "Use a public Linux AMD64 image with an @sha256 digest.")
	} else {
		addCheck(result, "image", CheckPass, "Self-hosted image is digest-pinned", resolved.environment.ImageUUID)
	}
	nodes, err := s.client.SelfHostedNode.Query().Where(
		selfhostednode.TenantIDEQ(resolved.project.TenantID),
		selfhostednode.StatusEQ(selfhostednode.StatusActive),
		selfhostednode.ObservedStateEQ(selfhostednode.ObservedStateOnline),
		selfhostednode.LastSeenAtNotNil(), selfhostednode.LastSeenAtGT(s.now().UTC().Add(-s.config.NodeStaleAfter)),
		selfhostednode.HasProjectAccessWith(
			nodeprojectaccess.ProjectIDEQ(resolved.project.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		),
	).Order(ent.Asc(selfhostednode.FieldID)).All(ctx)
	if err != nil {
		addCheck(result, "node", CheckFail, "Eligible Nodes could not be queried", bounded(err.Error(), 240))
		return
	}
	for _, node := range nodes {
		busy, err := s.client.NodeAssignment.Query().Where(
			nodeassignment.NodeIDEQ(node.ID),
			nodeassignment.StateIn(nodeassignment.StateStarting, nodeassignment.StateRunning, nodeassignment.StateStopping, nodeassignment.StateCollecting),
		).Exist(ctx)
		if err != nil {
			addCheck(result, "node", CheckFail, "Node Assignment state could not be queried", bounded(err.Error(), 240))
			return
		}
		if busy {
			continue
		}
		gpuName, ok := matchingNodeGPU(node.Capabilities, resolved.profile)
		if !ok {
			continue
		}
		addCheck(result, "node", CheckPass, "An online authorized Node is ready", fmt.Sprintf("%s (%s), GPU %s.", node.Label, node.PublicID.String(), gpuName))
		return
	}
	addCheck(result, "node", CheckFail, "No online authorized Node matches the selected profile", "Check Node heartbeat, Project authorization, external GPU occupancy, active Assignments, and exact GPU model names.")
}

func matchingNodeGPU(capabilities map[string]any, profile *ent.ResourceProfile) (string, bool) {
	values, ok := capabilities["gpus"].([]any)
	if !ok || len(values) != 1 {
		return "", false
	}
	gpu, ok := values[0].(map[string]any)
	if !ok {
		return "", false
	}
	name, _ := gpu["name"].(string)
	if name == "" {
		return "", false
	}
	for _, accepted := range profile.GpuNames {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(accepted)) {
			return name, true
		}
	}
	return "", false
}

func diagnosticReservation(profile *ent.ResourceProfile, runtimeSeconds, graceSeconds int) (int64, error) {
	if profile.Backend == resourceprofile.BackendSelfHosted {
		return 0, nil
	}
	billableSeconds := int64(runtimeSeconds + graceSeconds + 600 + 30)
	if profile.PriceToMilli <= 0 || profile.GpuNum < 1 || billableSeconds < 1 {
		return 0, fmt.Errorf("invalid diagnostic cost inputs")
	}
	if profile.PriceToMilli > math.MaxInt64/int64(profile.GpuNum) {
		return 0, fmt.Errorf("diagnostic reservation overflow")
	}
	product := profile.PriceToMilli * int64(profile.GpuNum)
	if product > math.MaxInt64/billableSeconds {
		return 0, fmt.Errorf("diagnostic reservation overflow")
	}
	product *= billableSeconds
	return (product + 3599) / 3600, nil
}

func currentPeriod(now time.Time, timezone string) (string, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", err
	}
	return now.In(location).Format("2006-01"), nil
}

func addCheck(result *Preflight, id, status, summary, detail string) {
	result.Checks = append(result.Checks, Check{ID: id, Status: status, Summary: summary, Detail: detail})
}

func bounded(value string, maximum int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum]) + "..."
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
