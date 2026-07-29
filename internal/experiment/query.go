package experiment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	entattempt "github.com/XR-Lee/Gemcp/ent/attempt"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/environment"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/google/uuid"
)

var validStates = map[string]struct{}{
	"queued": {}, "provisioning": {}, "running": {}, "cancelling": {}, "collecting": {},
	"succeeded": {}, "failed": {}, "cancelled": {}, "timed_out": {}, "budget_stopped": {}, "provider_error": {},
}

var terminalStates = map[string]struct{}{
	"succeeded": {}, "failed": {}, "cancelled": {}, "timed_out": {}, "budget_stopped": {}, "provider_error": {},
}

func (s *Service) Get(ctx context.Context, principal agentauth.Principal, experimentID string) (View, error) {
	var view View
	if !principal.HasScope("read") {
		return view, ErrForbidden
	}
	record, err := s.getRecord(ctx, principal.ProjectID, experimentID)
	if err != nil {
		return view, err
	}
	view = makeView(record)
	if err := s.enrichRunnerStatus(ctx, record, &view); err != nil {
		return View{}, err
	}
	if err := s.enrichExecutionObservation(ctx, record, &view); err != nil {
		return View{}, err
	}
	return view, nil
}

func (s *Service) enrichRunnerStatus(ctx context.Context, experimentRecord *ent.Experiment, view *View) error {
	attemptRecord, err := s.client.Attempt.Query().Where(entattempt.ExperimentIDEQ(experimentRecord.ID)).
		Order(ent.Desc(entattempt.FieldNumber)).First(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	attemptID := attemptRecord.PublicID.String()
	sourceDownloads := attemptRecord.SourceDownloads
	view.RunnerAttemptID = &attemptID
	view.RunnerSourceDownloads = &sourceDownloads

	events, err := s.client.AuditEvent.Query().Where(
		auditevent.TenantIDEQ(experimentRecord.TenantID),
		auditevent.ActionEQ("runner.bootstrap_stage"),
		auditevent.TargetTypeEQ("experiment"),
		auditevent.TargetIDEQ(experimentRecord.PublicID.String()),
	).Order(ent.Desc(auditevent.FieldCreatedAt), ent.Desc(auditevent.FieldID)).Limit(64).All(ctx)
	if err != nil {
		return err
	}
	for _, event := range events {
		if value, _ := event.Metadata["attempt_id"].(string); value != attemptID {
			continue
		}
		stage, ok := event.Metadata["stage"].(string)
		if !ok || stage == "" {
			continue
		}
		view.RunnerStage = &stage
		stageUpdatedAt := event.CreatedAt
		view.RunnerStageUpdatedAt = &stageUpdatedAt
		if errorType, ok := event.Metadata["error_type"].(string); ok && errorType != "" {
			view.RunnerErrorType = &errorType
		}
		break
	}
	return nil
}

func (s *Service) List(ctx context.Context, principal agentauth.Principal, input ListInput) (ListResult, error) {
	var result ListResult
	if !principal.HasScope("read") {
		return result, ErrForbidden
	}
	limit := input.Limit
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return result, &ValidationError{Message: "limit must be between 1 and 100"}
	}
	states := make([]string, 0, len(input.States))
	for _, value := range input.States {
		state := strings.TrimSpace(value)
		if _, ok := validStates[state]; !ok {
			return result, &ValidationError{Message: fmt.Sprintf("unsupported experiment state %q", state)}
		}
		states = append(states, state)
	}
	query := s.client.Experiment.Query().Where(entexperiment.ProjectIDEQ(principal.ProjectID))
	if len(states) > 0 {
		query.Where(entexperiment.StateIn(states...))
	}
	records, err := query.Order(ent.Desc(entexperiment.FieldCreatedAt)).Limit(limit).All(ctx)
	if err != nil {
		return result, err
	}
	result.Experiments = make([]View, 0, len(records))
	for _, record := range records {
		result.Experiments = append(result.Experiments, makeView(record))
	}
	return result, nil
}

func (s *Service) Cancel(ctx context.Context, principal agentauth.Principal, experimentID string) (View, error) {
	var view View
	if !principal.HasScope("cancel") {
		return view, ErrForbidden
	}
	publicID, err := uuid.Parse(strings.TrimSpace(experimentID))
	if err != nil {
		return view, ErrNotFound
	}
	for attempt := 0; attempt < 3; attempt++ {
		view, err = s.cancelOnce(ctx, principal, publicID, "agent_token", principal.TokenPublicID)
		if err == nil {
			return view, nil
		}
		if !isRetryableTransaction(err) && !errors.Is(err, errStateChanged) {
			return View{}, err
		}
	}
	return View{}, err
}

var errStateChanged = errors.New("experiment state changed concurrently")

func (s *Service) cancelOnce(ctx context.Context, principal agentauth.Principal, publicID uuid.UUID, actorType auditevent.ActorType, actorID string) (View, error) {
	var view View
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return view, err
	}
	defer tx.Rollback()
	record, err := tx.Experiment.Query().Where(
		entexperiment.PublicIDEQ(publicID), entexperiment.ProjectIDEQ(principal.ProjectID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return view, ErrNotFound
	}
	if err != nil {
		return view, err
	}
	if _, terminal := terminalStates[record.State]; terminal {
		return makeView(record), nil
	}
	if record.DesiredState == "cancelled" {
		now := s.now().UTC()
		if _, err := tx.ProviderResource.Update().Where(
			providerresource.ExperimentIDEQ(record.ID), providerresource.OwnedEQ(true),
			providerresource.StateIn(
				providerresource.StateCreating, providerresource.StateActive, providerresource.StateStopping,
				providerresource.StateStopped, providerresource.StateDeleting,
			), providerresource.StopRequestedAtIsNil(),
		).SetStopRequestedAt(now).SetStopReason("cancelled").Save(ctx); err != nil {
			return view, err
		}
		if err := tx.Commit(); err != nil {
			return view, err
		}
		return makeView(record), nil
	}
	now := s.now().UTC()
	if record.State == "queued" {
		updated, err := tx.Experiment.Update().Where(
			entexperiment.IDEQ(record.ID), entexperiment.StateEQ("queued"),
		).SetState("cancelled").SetDesiredState("cancelled").SetCancelRequestedAt(now).SetFinishedAt(now).SetBudgetFinalizedAt(now).Save(ctx)
		if err != nil {
			return view, err
		}
		if updated != 1 {
			return view, errStateChanged
		}
		reservation, err := tx.BudgetEntry.Query().Where(
			budgetentry.ExperimentIDEQ(record.ID), budgetentry.KindEQ("reservation"),
		).Order(ent.Desc(budgetentry.FieldID)).First(ctx)
		if err != nil {
			return view, err
		}
		if _, err := tx.BudgetEntry.Create().
			SetTenantID(principal.TenantID).
			SetProjectID(principal.ProjectID).
			SetExperimentID(record.ID).
			SetPeriod(reservation.Period).
			SetKind("release").
			SetAmountMilli(-record.ReservedCostMilli).
			SetDescription("queued experiment cancellation release").
			Save(ctx); err != nil {
			return view, err
		}
	} else {
		updated, err := tx.Experiment.Update().Where(
			entexperiment.IDEQ(record.ID), entexperiment.StateEQ(record.State),
		).SetDesiredState("cancelled").SetState("cancelling").SetCancelRequestedAt(now).Save(ctx)
		if err != nil {
			return view, err
		}
		if updated != 1 {
			return view, errStateChanged
		}
		if _, err := tx.ProviderResource.Update().Where(
			providerresource.ExperimentIDEQ(record.ID), providerresource.OwnedEQ(true),
			providerresource.StateIn(
				providerresource.StateCreating, providerresource.StateActive, providerresource.StateStopping,
				providerresource.StateStopped, providerresource.StateDeleting,
			), providerresource.StopRequestedAtIsNil(),
		).SetStopRequestedAt(now).SetStopReason("cancelled").Save(ctx); err != nil {
			return view, err
		}
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(principal.TenantID).
		SetActorType(actorType).
		SetActorID(actorID).
		SetAction("experiment.cancel_requested").
		SetTargetType("experiment").
		SetTargetID(publicID.String()).
		SetMetadata(map[string]any{"previous_state": record.State}).
		Save(ctx); err != nil {
		return view, err
	}
	if err := tx.Commit(); err != nil {
		return view, err
	}
	updatedRecord, err := s.getRecord(ctx, principal.ProjectID, publicID.String())
	if err != nil {
		return view, err
	}
	return makeView(updatedRecord), nil
}

func (s *Service) Options(ctx context.Context, principal agentauth.Principal) (ProjectOptions, error) {
	var result ProjectOptions
	if !principal.HasScope("read") {
		return result, ErrForbidden
	}
	projectRecord, err := s.client.Project.Get(ctx, principal.ProjectID)
	if ent.IsNotFound(err) || (err == nil && projectRecord.Status == "archived") {
		return result, ErrProjectPaused
	}
	if err != nil {
		return result, err
	}
	repositories, err := s.client.Repository.Query().Where(
		repository.ProjectIDEQ(principal.ProjectID), repository.StatusEQ("active"),
	).Order(ent.Asc(repository.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	environments, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(principal.ProjectID), environment.StatusEQ("approved"),
	).Order(ent.Desc(environment.FieldIsDefault), ent.Asc(environment.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	profiles, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(principal.ProjectID), resourceprofile.StatusEQ("active"),
	).Order(ent.Desc(resourceprofile.FieldIsDefault), ent.Asc(resourceprofile.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	result.Project = ProjectPolicy{
		ID: projectRecord.PublicID.String(), Name: projectRecord.Name, MonthlyBudgetMilli: projectRecord.MonthlyBudgetMilli,
		MaxExperimentMilli: projectRecord.MaxExperimentMilli, MaxConcurrency: projectRecord.MaxConcurrency,
		MaxRuntimeSeconds: projectRecord.MaxRuntimeSeconds, TimeoutExtensionSeconds: projectRecord.TimeoutExtensionSeconds,
		TerminationGraceSeconds: projectRecord.TerminationGraceSeconds, Timezone: projectRecord.Timezone,
	}
	result.Repositories = make([]RepositoryOption, 0, len(repositories))
	for _, record := range repositories {
		result.Repositories = append(result.Repositories, RepositoryOption{
			ID: record.PublicID.String(), Name: record.Name, SSHURL: record.SSHURL, DefaultBranch: record.DefaultBranch,
		})
	}
	result.Environments = make([]EnvironmentOption, 0, len(environments))
	for _, record := range environments {
		result.Environments = append(result.Environments, EnvironmentOption{
			ID: record.PublicID.String(), Name: record.Name, Backend: string(record.Backend), ImageUUID: record.ImageUUID, IsDefault: record.IsDefault,
		})
	}
	result.ResourceProfiles = make([]ResourceProfileOption, 0, len(profiles))
	for _, record := range profiles {
		result.ResourceProfiles = append(result.ResourceProfiles, ResourceProfileOption{
			ID: record.PublicID.String(), Name: record.Name, Backend: string(record.Backend), Region: record.Region, GPUNames: record.GpuNames,
			GPUNum: record.GpuNum, PriceFromMilli: record.PriceFromMilli, PriceToMilli: record.PriceToMilli,
			ReuseContainer: record.ReuseContainer, IsDefault: record.IsDefault,
		})
	}
	nodes, err := s.client.SelfHostedNode.Query().Where(
		selfhostednode.TenantIDEQ(principal.TenantID),
		selfhostednode.HasProjectAccessWith(
			nodeprojectaccess.ProjectIDEQ(principal.ProjectID),
			nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		),
	).Order(ent.Asc(selfhostednode.FieldLabel), ent.Asc(selfhostednode.FieldID)).All(ctx)
	if err != nil {
		return result, err
	}
	busyNodes := map[int]bool{}
	if len(nodes) > 0 {
		nodeIDs := make([]int, 0, len(nodes))
		for _, node := range nodes {
			nodeIDs = append(nodeIDs, node.ID)
		}
		assignments, err := s.client.NodeAssignment.Query().Where(
			nodeassignment.NodeIDIn(nodeIDs...),
			nodeassignment.StateIn(nodeassignment.StateStarting, nodeassignment.StateRunning, nodeassignment.StateStopping, nodeassignment.StateCollecting),
		).All(ctx)
		if err != nil {
			return result, err
		}
		for _, assignment := range assignments {
			busyNodes[assignment.NodeID] = true
		}
	}
	result.SelfHostedNodes = make([]SelfHostedNodeOption, 0, len(nodes))
	staleBefore := s.now().UTC().Add(-s.proposalConfig.NodeStaleAfter)
	for _, node := range nodes {
		gpus := selfHostedOptionGPUs(node.Capabilities)
		executionModes := selfHostedOptionExecutionModes(node.Capabilities)
		runtimeConfigured := selfHostedOptionRuntimeConfigured(gpus, environments, profiles)
		blockers := selfHostedOptionBlockers(node, executionModes, runtimeConfigured, busyNodes[node.ID], staleBefore)
		readiness := "ready"
		if len(blockers) > 0 {
			readiness = blockers[0]
		}
		result.SelfHostedNodes = append(result.SelfHostedNodes, SelfHostedNodeOption{
			ID: node.PublicID.String(), Label: node.Label, Status: string(node.Status), ObservedState: string(node.ObservedState),
			AgentVersion: node.AgentVersion, GPUs: gpus, ExecutionModes: executionModes, LastSeenAt: node.LastSeenAt,
			RuntimeConfigured: runtimeConfigured, Ready: len(blockers) == 0, Readiness: readiness, Blockers: blockers,
		})
	}
	return result, nil
}

func selfHostedOptionGPUs(capabilities map[string]any) []SelfHostedGPUOption {
	values := proposalGPUValues(capabilities)
	result := make([]SelfHostedGPUOption, 0, len(values))
	for _, value := range values {
		gpu, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gpu["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var memoryBytes int64
		switch value := gpu["memory_bytes"].(type) {
		case float64:
			if value > 0 && value <= math.MaxInt64 {
				memoryBytes = int64(value)
			}
		case int64:
			memoryBytes = value
		case int:
			memoryBytes = int64(value)
		}
		result = append(result, SelfHostedGPUOption{Name: name, MemoryBytes: memoryBytes})
	}
	return result
}

func selfHostedOptionExecutionModes(capabilities map[string]any) []string {
	seen := map[string]bool{}
	var values []string
	switch raw := capabilities["execution_modes"].(type) {
	case []any:
		for _, value := range raw {
			if mode, ok := value.(string); ok {
				values = append(values, mode)
			}
		}
	case []string:
		values = append(values, raw...)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func selfHostedOptionRuntimeConfigured(gpus []SelfHostedGPUOption, environments []*ent.Environment, profiles []*ent.ResourceProfile) bool {
	approvedEnvironments := map[string]bool{}
	for _, record := range environments {
		if record.Backend == environment.BackendSelfHosted {
			approvedEnvironments[record.Name] = true
		}
	}
	for _, profile := range profiles {
		if profile.Backend != resourceprofile.BackendSelfHosted || !approvedEnvironments[profile.Name] || profile.GpuNum != 1 {
			continue
		}
		for _, gpu := range gpus {
			for _, accepted := range profile.GpuNames {
				if strings.EqualFold(strings.TrimSpace(accepted), gpu.Name) {
					return true
				}
			}
		}
	}
	return false
}

func selfHostedOptionBlockers(node *ent.SelfHostedNode, executionModes []string, runtimeConfigured, busy bool, staleBefore time.Time) []string {
	result := make([]string, 0, 5)
	if node.Status != selfhostednode.StatusActive {
		result = append(result, "node_not_active")
	}
	switch node.ObservedState {
	case selfhostednode.ObservedStateOnline:
	case selfhostednode.ObservedStateExternallyBusy:
		result = append(result, "gpu_busy")
	case selfhostednode.ObservedStateIncompatible:
		result = append(result, "node_incompatible")
	default:
		result = append(result, "node_not_online")
	}
	if node.LastSeenAt == nil || node.LastSeenAt.Before(staleBefore) {
		result = append(result, "node_stale")
	}
	if !slices.Contains(executionModes, "argv") {
		result = append(result, "argv_upgrade_required")
	}
	if busy {
		result = append(result, "node_busy")
	}
	if !runtimeConfigured {
		result = append(result, "runtime_configuration_required")
	}
	return result
}

func (s *Service) Cost(ctx context.Context, principal agentauth.Principal) (CostView, error) {
	var result CostView
	if !principal.HasScope("read") {
		return result, ErrForbidden
	}
	projectRecord, err := s.client.Project.Get(ctx, principal.ProjectID)
	if ent.IsNotFound(err) {
		return result, ErrProjectPaused
	}
	if err != nil {
		return result, err
	}
	period, err := budgetPeriod(s.now().UTC(), projectRecord.Timezone)
	if err != nil {
		return result, err
	}
	entries, err := s.client.BudgetEntry.Query().Where(
		budgetentry.ProjectIDEQ(principal.ProjectID), budgetentry.PeriodEQ(period),
	).All(ctx)
	if err != nil {
		return result, err
	}
	result.Period = period
	result.MonthlyBudgetMilli = projectRecord.MonthlyBudgetMilli
	for _, entry := range entries {
		var next int64
		var ok bool
		switch entry.Kind {
		case "reservation", "release":
			next, ok = addInt64(result.ReservedMilli, entry.AmountMilli)
			result.ReservedMilli = next
		case "charge":
			next, ok = addInt64(result.ChargedMilli, entry.AmountMilli)
			result.ChargedMilli = next
		case "adjustment":
			next, ok = addInt64(result.AdjustmentsMilli, entry.AmountMilli)
			result.AdjustmentsMilli = next
		default:
			ok = true
		}
		if !ok {
			return CostView{}, fmt.Errorf("budget ledger overflow")
		}
	}
	committed, ok := addInt64(result.ReservedMilli, result.ChargedMilli)
	if !ok {
		return CostView{}, fmt.Errorf("budget ledger overflow")
	}
	committed, ok = addInt64(committed, result.AdjustmentsMilli)
	if !ok {
		return CostView{}, fmt.Errorf("budget ledger overflow")
	}
	result.CommittedMilli = committed
	available, ok := subtractInt64(projectRecord.MonthlyBudgetMilli, result.CommittedMilli)
	if !ok {
		return CostView{}, fmt.Errorf("budget ledger overflow")
	}
	result.AvailableMilli = available
	if result.AvailableMilli < 0 {
		result.AvailableMilli = 0
	}
	return result, nil
}

func (s *Service) Artifacts(ctx context.Context, principal agentauth.Principal, experimentID string) (ArtifactView, error) {
	var result ArtifactView
	if !principal.HasScope("read") {
		return result, ErrForbidden
	}
	record, err := s.getRecord(ctx, principal.ProjectID, experimentID)
	if err != nil {
		return result, err
	}
	isDiagnostic, err := record.QueryDiagnosticRun().Exist(ctx)
	if err != nil {
		return result, err
	}
	artifacts := []string{}
	if strings.HasPrefix(record.OutputPath, "/root/autodl-fs/") && record.ProviderResourceID != nil {
		artifacts = append(artifacts, "gemcp-launch.log")
	}
	if record.StartedAt != nil || record.LogTail != nil {
		artifacts = append(artifacts, "run.log")
	}
	if record.ExitCode != nil {
		artifacts = append(artifacts, "gemcp-result.json")
	}
	if len(record.Metrics) > 0 {
		artifacts = append(artifacts, "metrics.json")
	}
	if isDiagnostic && record.ExitCode != nil {
		artifacts = append(artifacts, "diagnostic-report.txt")
	}
	return ArtifactView{ExperimentID: record.PublicID.String(), OutputPath: record.OutputPath, Artifacts: artifacts}, nil
}

func (s *Service) getRecord(ctx context.Context, projectID int, value string) (*ent.Experiment, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, ErrNotFound
	}
	record, err := s.client.Experiment.Query().Where(
		entexperiment.PublicIDEQ(publicID), entexperiment.ProjectIDEQ(projectID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return record, err
}

func addInt64(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, false
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func subtractInt64(left, right int64) (int64, bool) {
	if right > 0 && left < math.MinInt64+right {
		return 0, false
	}
	if right < 0 && left > math.MaxInt64+right {
		return 0, false
	}
	return left - right, true
}
