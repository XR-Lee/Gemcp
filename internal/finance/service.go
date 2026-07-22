package finance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/google/uuid"
)

const (
	maxLedgerEntries   = 200
	maxAuditEntries    = 100
	maxAdjustmentMilli = int64(1_000_000_000_000)
)

var (
	ErrProjectNotFound     = errors.New("finance project not found")
	ErrInvalidPeriod       = errors.New("invalid finance period")
	ErrInvalidAdjustment   = errors.New("invalid budget adjustment")
	ErrIdempotencyConflict = errors.New("budget adjustment idempotency conflict")
	idempotencyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
)

type Service struct {
	client *ent.Client
	now    func() time.Time
}

type Option func(*Service)

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func NewService(client *ent.Client, options ...Option) *Service {
	service := &Service{client: client, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Dashboard(ctx context.Context, tenantID int, period, projectPublicID string) (Dashboard, error) {
	var result Dashboard
	if s == nil || s.client == nil {
		return result, errors.New("finance service is not initialized")
	}
	period = strings.TrimSpace(period)
	projectPublicID = strings.TrimSpace(projectPublicID)

	query := s.client.Project.Query().Where(project.TenantIDEQ(tenantID)).Order(ent.Asc(project.FieldName))
	if projectPublicID != "" {
		projectID, err := uuid.Parse(projectPublicID)
		if err != nil {
			return result, ErrProjectNotFound
		}
		query.Where(project.PublicIDEQ(projectID))
	}
	projects, err := query.All(ctx)
	if err != nil {
		return result, err
	}
	if projectPublicID != "" && len(projects) == 0 {
		return result, ErrProjectNotFound
	}
	if period == "" {
		location := time.UTC
		if len(projects) == 1 {
			if loaded, loadErr := time.LoadLocation(projects[0].Timezone); loadErr == nil {
				location = loaded
			}
		}
		period = s.now().In(location).Format("2006-01")
	}
	if !validPeriod(period) {
		return result, ErrInvalidPeriod
	}

	result.Period = period
	result.AuditScope = "organization"
	result.GeneratedAt = s.now().UTC()
	projectByID := make(map[int]*ent.Project, len(projects))
	summaryIndex := make(map[int]int, len(projects))
	projectIDs := make([]int, 0, len(projects))
	result.Projects = make([]ProjectSummary, 0, len(projects))
	for _, record := range projects {
		projectByID[record.ID] = record
		projectIDs = append(projectIDs, record.ID)
		summary := ProjectSummary{
			ID: record.PublicID.String(), Name: record.Name, Status: string(record.Status), Timezone: record.Timezone,
			Totals: Totals{BaseBudgetMilli: record.MonthlyBudgetMilli},
		}
		result.Projects = append(result.Projects, summary)
		summaryIndex[record.ID] = len(result.Projects) - 1
		if err := add(&result.Totals.BaseBudgetMilli, record.MonthlyBudgetMilli); err != nil {
			return Dashboard{}, err
		}
	}

	entries := []*ent.BudgetEntry{}
	if len(projectIDs) > 0 {
		entries, err = s.client.BudgetEntry.Query().Where(
			budgetentry.TenantIDEQ(tenantID), budgetentry.ProjectIDIn(projectIDs...), budgetentry.PeriodEQ(period),
		).WithProject().WithExperiment().Order(ent.Desc(budgetentry.FieldCreatedAt), ent.Desc(budgetentry.FieldID)).All(ctx)
		if err != nil {
			return Dashboard{}, err
		}
	}

	daily := map[string]*DailyPoint{}
	backends := map[string]*BackendSummary{}
	backendExperiments := map[string]map[int]struct{}{}
	for index, entry := range entries {
		summaryPosition, ok := summaryIndex[entry.ProjectID]
		if !ok {
			continue
		}
		summary := &result.Projects[summaryPosition]
		if err := applyEntry(&summary.Totals, entry); err != nil {
			return Dashboard{}, err
		}
		if err := applyEntry(&result.Totals, entry); err != nil {
			return Dashboard{}, err
		}

		projectRecord := projectByID[entry.ProjectID]
		location := time.UTC
		if projectRecord != nil {
			if loaded, loadErr := time.LoadLocation(projectRecord.Timezone); loadErr == nil {
				location = loaded
			}
		}
		day := entry.CreatedAt.In(location).Format("2006-01-02")
		point := daily[day]
		if point == nil {
			point = &DailyPoint{Date: day}
			daily[day] = point
		}
		if err := applyDaily(point, entry); err != nil {
			return Dashboard{}, err
		}

		backend := entryBackend(entry)
		if backend != "" && entry.Edges.Experiment != nil {
			breakdown := backends[backend]
			if breakdown == nil {
				breakdown = &BackendSummary{Backend: backend}
				backends[backend] = breakdown
				backendExperiments[backend] = map[int]struct{}{}
			}
			backendExperiments[backend][entry.Edges.Experiment.ID] = struct{}{}
			switch entry.Kind {
			case "reservation", "release":
				if err := add(&breakdown.ReservedMilli, entry.AmountMilli); err != nil {
					return Dashboard{}, err
				}
			case "charge":
				if err := add(&breakdown.ChargedMilli, entry.AmountMilli); err != nil {
					return Dashboard{}, err
				}
			}
		}

		if index < maxLedgerEntries {
			result.Ledger = append(result.Ledger, ledgerView(entry, entry.Edges.Project, entry.Edges.Experiment))
		}
	}

	if err := finalizeTotals(&result.Totals); err != nil {
		return Dashboard{}, err
	}
	for index := range result.Projects {
		if err := finalizeTotals(&result.Projects[index].Totals); err != nil {
			return Dashboard{}, err
		}
	}
	for _, point := range daily {
		result.Daily = append(result.Daily, *point)
	}
	sort.Slice(result.Daily, func(i, j int) bool { return result.Daily[i].Date < result.Daily[j].Date })
	for backend, breakdown := range backends {
		breakdown.Experiments = len(backendExperiments[backend])
		result.Backends = append(result.Backends, *breakdown)
	}
	sort.Slice(result.Backends, func(i, j int) bool {
		if result.Backends[i].ChargedMilli == result.Backends[j].ChargedMilli {
			return result.Backends[i].Backend < result.Backends[j].Backend
		}
		return result.Backends[i].ChargedMilli > result.Backends[j].ChargedMilli
	})

	audits, err := s.client.AuditEvent.Query().Where(
		auditevent.TenantIDEQ(tenantID),
		auditevent.Or(
			auditevent.ActionHasPrefix("budget."),
			auditevent.ActionHasPrefix("experiment."),
			auditevent.ActionHasPrefix("provider.resource_"),
			auditevent.ActionEQ("provider.emergency_stop_requested"),
			auditevent.ActionEQ("watchdog.stop_enforced"),
		),
	).Order(ent.Desc(auditevent.FieldCreatedAt), ent.Desc(auditevent.FieldID)).Limit(maxAuditEntries).All(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	result.Audit = make([]AuditEntry, 0, len(audits))
	for _, record := range audits {
		result.Audit = append(result.Audit, AuditEntry{
			ID: record.PublicID.String(), ActorType: string(record.ActorType), ActorID: record.ActorID,
			Action: record.Action, TargetType: record.TargetType, TargetID: record.TargetID, CreatedAt: record.CreatedAt,
		})
	}
	return result, nil
}

func (s *Service) Adjust(ctx context.Context, tenantID int, actorID, projectPublicID string, input AdjustmentInput) (AdjustmentResult, error) {
	var result AdjustmentResult
	if s == nil || s.client == nil {
		return result, errors.New("finance service is not initialized")
	}
	direction := strings.ToLower(strings.TrimSpace(input.Direction))
	reason := strings.TrimSpace(input.Reason)
	key := strings.TrimSpace(input.IdempotencyKey)
	if (direction != "credit" && direction != "debit") || input.AmountMilli <= 0 || input.AmountMilli > maxAdjustmentMilli ||
		utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 255 || !idempotencyPattern.MatchString(key) {
		return result, ErrInvalidAdjustment
	}
	projectID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return result, ErrProjectNotFound
	}
	amount := input.AmountMilli
	if direction == "credit" {
		amount = -amount
	}

	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	projectRecord, err := tx.Project.Query().Where(
		project.PublicIDEQ(projectID), project.TenantIDEQ(tenantID), project.StatusNEQ(project.StatusArchived),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrProjectNotFound
	}
	if err != nil {
		return result, err
	}
	existing, err := tx.BudgetEntry.Query().Where(
		budgetentry.ProjectIDEQ(projectRecord.ID), budgetentry.IdempotencyKeyEQ(key),
	).WithExperiment().Only(ctx)
	if err == nil {
		return existingAdjustment(existing, projectRecord, amount, reason)
	}
	if !ent.IsNotFound(err) {
		return result, err
	}
	location, err := time.LoadLocation(projectRecord.Timezone)
	if err != nil {
		return result, fmt.Errorf("load project timezone: %w", err)
	}
	period := s.now().In(location).Format("2006-01")
	entry, err := tx.BudgetEntry.Create().
		SetTenantID(tenantID).
		SetProjectID(projectRecord.ID).
		SetPeriod(period).
		SetKind("adjustment").
		SetAmountMilli(amount).
		SetDescription(reason).
		SetIdempotencyKey(key).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			originalErr := err
			_ = tx.Rollback()
			existing, lookupErr := s.client.BudgetEntry.Query().Where(
				budgetentry.TenantIDEQ(tenantID), budgetentry.ProjectIDEQ(projectRecord.ID), budgetentry.IdempotencyKeyEQ(key),
			).WithExperiment().Only(ctx)
			if lookupErr == nil {
				return existingAdjustment(existing, projectRecord, amount, reason)
			}
			return result, originalErr
		}
		return result, err
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType("user").
		SetActorID(strings.TrimSpace(actorID)).
		SetAction("budget." + direction + "_recorded").
		SetTargetType("budget_entry").
		SetTargetID(entry.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectRecord.PublicID.String(), "period": period, "direction": direction,
			"amount_milli": input.AmountMilli,
		}).
		Save(ctx); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.Entry = ledgerView(entry, projectRecord, nil)
	return result, nil
}

func existingAdjustment(entry *ent.BudgetEntry, projectRecord *ent.Project, amount int64, reason string) (AdjustmentResult, error) {
	if entry.Kind != "adjustment" || entry.AmountMilli != amount || entry.Description != reason {
		return AdjustmentResult{}, ErrIdempotencyConflict
	}
	return AdjustmentResult{Entry: ledgerView(entry, projectRecord, entry.Edges.Experiment), Idempotent: true}, nil
}

func validPeriod(value string) bool {
	if len(value) != 7 {
		return false
	}
	parsed, err := time.Parse("2006-01", value)
	return err == nil && parsed.Format("2006-01") == value && parsed.Year() >= 2000 && parsed.Year() <= 2100
}

func applyEntry(totals *Totals, entry *ent.BudgetEntry) error {
	switch entry.Kind {
	case "reservation", "release":
		return add(&totals.ReservedMilli, entry.AmountMilli)
	case "charge":
		return add(&totals.ChargedMilli, entry.AmountMilli)
	case "adjustment":
		if entry.AmountMilli < 0 {
			credit, err := negate(entry.AmountMilli)
			if err != nil {
				return err
			}
			return add(&totals.CreditsMilli, credit)
		}
		return add(&totals.DebitsMilli, entry.AmountMilli)
	default:
		return fmt.Errorf("unsupported budget entry kind %q", entry.Kind)
	}
}

func applyDaily(point *DailyPoint, entry *ent.BudgetEntry) error {
	switch entry.Kind {
	case "reservation", "release":
		return add(&point.ReservedMilli, entry.AmountMilli)
	case "charge":
		return add(&point.ChargedMilli, entry.AmountMilli)
	case "adjustment":
		if entry.AmountMilli < 0 {
			credit, err := negate(entry.AmountMilli)
			if err != nil {
				return err
			}
			return add(&point.CreditsMilli, credit)
		}
		return add(&point.DebitsMilli, entry.AmountMilli)
	default:
		return nil
	}
}

func finalizeTotals(totals *Totals) error {
	committed := int64(0)
	for _, value := range []int64{totals.ReservedMilli, totals.ChargedMilli, totals.DebitsMilli} {
		if err := add(&committed, value); err != nil {
			return err
		}
	}
	if totals.CreditsMilli > 0 && committed < math.MinInt64+totals.CreditsMilli {
		return errors.New("finance total overflow")
	}
	committed -= totals.CreditsMilli
	totals.CommittedMilli = committed
	if committed > 0 && totals.BaseBudgetMilli < math.MinInt64+committed {
		return errors.New("finance total overflow")
	}
	if committed < 0 && totals.BaseBudgetMilli > math.MaxInt64+committed {
		return errors.New("finance total overflow")
	}
	totals.AvailableMilli = totals.BaseBudgetMilli - committed
	return nil
}

func add(target *int64, value int64) error {
	if value > 0 && *target > math.MaxInt64-value || value < 0 && *target < math.MinInt64-value {
		return errors.New("finance total overflow")
	}
	*target += value
	return nil
}

func negate(value int64) (int64, error) {
	if value == math.MinInt64 {
		return 0, errors.New("finance total overflow")
	}
	return -value, nil
}

func entryBackend(entry *ent.BudgetEntry) string {
	if entry.Edges.Experiment == nil {
		return ""
	}
	backend, _ := entry.Edges.Experiment.EnvironmentSnapshot["backend"].(string)
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return "unknown"
	}
	return backend
}

func ledgerView(entry *ent.BudgetEntry, projectRecord *ent.Project, experimentRecord *ent.Experiment) LedgerEntry {
	view := LedgerEntry{
		ID: entry.PublicID.String(), Period: entry.Period, Kind: entry.Kind, AmountMilli: entry.AmountMilli,
		Description: entry.Description, CreatedAt: entry.CreatedAt, Backend: entryBackend(entry),
	}
	if effect, err := negate(entry.AmountMilli); err == nil {
		view.BalanceEffectMilli = effect
	}
	if projectRecord != nil {
		view.ProjectID = projectRecord.PublicID.String()
		view.ProjectName = projectRecord.Name
	}
	if experimentRecord != nil {
		view.ExperimentID = experimentRecord.PublicID.String()
	}
	if entry.Kind == "adjustment" {
		if entry.AmountMilli < 0 {
			view.Direction = "credit"
		} else {
			view.Direction = "debit"
		}
	}
	return view
}
