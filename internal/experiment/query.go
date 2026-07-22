package experiment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/environment"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/providerresource"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
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
	return makeView(record), nil
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
		view, err = s.cancelOnce(ctx, principal, publicID)
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

func (s *Service) cancelOnce(ctx context.Context, principal agentauth.Principal, publicID uuid.UUID) (View, error) {
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
		SetActorType("agent_token").
		SetActorID(principal.TokenPublicID).
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
	return result, nil
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
	artifacts := []string{}
	if record.StartedAt != nil || record.LogTail != nil {
		artifacts = append(artifacts, "run.log")
	}
	if record.ExitCode != nil {
		artifacts = append(artifacts, "gemcp-result.json")
	}
	if len(record.Metrics) > 0 {
		artifacts = append(artifacts, "metrics.json")
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
