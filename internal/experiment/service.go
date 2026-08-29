package experiment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
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
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/idempotencyrecord"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/study"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/XR-Lee/Gemcp/internal/validation"
	"github.com/google/uuid"
)

var (
	ErrForbidden           = errors.New("Agent token lacks the required scope")
	ErrNotFound            = errors.New("experiment not found")
	ErrOptionNotFound      = errors.New("requested project option was not found or is inactive")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
	ErrBudgetExceeded      = errors.New("project budget does not have enough uncommitted capacity")
	ErrExperimentCap       = errors.New("experiment cost reservation exceeds the project cap")
	ErrCommitVerification  = errors.New("Git commit could not be verified as reachable from the registered repository")
	ErrProjectPaused       = errors.New("project is not accepting experiments")
	commitPattern          = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	idempotencyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	secretNamePattern      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
)

const (
	idempotencyRetention              = 30 * 24 * time.Hour
	startupReserveSeconds             = int64(600)
	shutdownObservationReserveSeconds = int64(30)
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

type CommitVerifier interface {
	VerifyCommit(context.Context, int, string) error
}

type Service struct {
	client         *ent.Client
	box            *secrets.Box
	verifier       CommitVerifier
	refResolver    ProposalRefResolver
	archiver       ProposalArchiveReader
	providerReader ProposalProviderReader
	runtimeReader  ProposalRuntimeReader
	proposalConfig ProposalConfig
	graphBinder    GraphBinder
	sshCloud       SSHCloudController
	now            func() time.Time
}

type SSHCloudController interface {
	EnsureForProject(ctx context.Context, tenantID int, actorID, projectPublicID, image string) (sshcloud.EnsureResult, error)
}

type GraphBinder interface {
	BindPreparedRun(context.Context, agentauth.Principal, string, string, string) (string, error)
}

func WithGraphBinder(binder GraphBinder) ServiceOption {
	return func(service *Service) {
		service.graphBinder = binder
	}
}

func (s *Service) SetGraphBinder(binder GraphBinder) {
	s.graphBinder = binder
}

type ServiceOption func(*Service)

func WithSSHCloud(controller SSHCloudController) ServiceOption {
	return func(service *Service) {
		service.sshCloud = controller
	}
}

func WithPreparedExperiments(refResolver ProposalRefResolver, archiver ProposalArchiveReader, providerReader ProposalProviderReader, runtimeReader ProposalRuntimeReader, config ProposalConfig) ServiceOption {
	return func(service *Service) {
		service.refResolver = refResolver
		service.archiver = archiver
		service.providerReader = providerReader
		service.runtimeReader = runtimeReader
		service.proposalConfig = config
	}
}

func NewService(client *ent.Client, box *secrets.Box, verifier CommitVerifier, options ...ServiceOption) *Service {
	service := &Service{client: client, box: box, verifier: verifier, now: time.Now}
	for _, option := range options {
		option(service)
	}
	service.normalizeProposalConfig()
	return service
}

type normalizedSubmit struct {
	RepositoryID      string   `json:"repository_id"`
	EnvironmentID     string   `json:"environment_id,omitempty"`
	ResourceProfileID string   `json:"resource_profile_id,omitempty"`
	CommitSHA         string   `json:"commit_sha"`
	Command           string   `json:"command"`
	MaxRuntimeSeconds int      `json:"max_runtime_seconds,omitempty"`
	SecretNames       []string `json:"secret_names,omitempty"`
}

type resolvedOptions struct {
	repositoryID      int
	environmentID     int
	resourceProfileID int
}

func (s *Service) Submit(ctx context.Context, principal agentauth.Principal, input SubmitInput) (SubmitResult, error) {
	var result SubmitResult
	if !principal.HasScope("submit") {
		return result, ErrForbidden
	}
	normalized, err := normalizeSubmit(input)
	if err != nil {
		return result, err
	}
	hasStudy, err := s.client.Study.Query().Where(
		study.ProjectIDEQ(principal.ProjectID), study.StatusEQ(study.StatusActive),
	).Exist(ctx)
	if err != nil {
		return result, err
	}
	if hasStudy {
		return result, &ValidationError{Message: "submit_experiment cannot skip the Study and hypothesis; use prepare_experiment with from_node_id"}
	}
	fingerprintJSON, _ := json.Marshal(normalized)
	fingerprint := sha256.Sum256(fingerprintJSON)
	keyHash := s.box.Digest("experiment-idempotency", strings.TrimSpace(input.IdempotencyKey))
	if existing, found, err := s.findExisting(ctx, s.client.IdempotencyRecord.Query(), principal.TokenID, keyHash, fingerprint[:]); err != nil {
		return result, err
	} else if found {
		return SubmitResult{Experiment: makeView(existing), Idempotent: true}, nil
	}

	options, err := s.resolveOptions(ctx, principal.ProjectID, normalized)
	if err != nil {
		return result, err
	}
	if s.verifier == nil {
		return result, fmt.Errorf("%w: verifier is unavailable", ErrCommitVerification)
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	err = s.verifier.VerifyCommit(verifyCtx, options.repositoryID, normalized.CommitSHA)
	cancel()
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrCommitVerification, err)
	}

	for attempt := 0; attempt < 3; attempt++ {
		result, err = s.createSubmission(ctx, principal, normalized, keyHash, fingerprint[:], options)
		if err == nil {
			return result, nil
		}
		if ent.IsConstraintError(err) {
			if existing, found, lookupErr := s.findExisting(ctx, s.client.IdempotencyRecord.Query(), principal.TokenID, keyHash, fingerprint[:]); lookupErr != nil {
				return SubmitResult{}, lookupErr
			} else if found {
				return SubmitResult{Experiment: makeView(existing), Idempotent: true}, nil
			}
		}
		if !isRetryableTransaction(err) {
			return SubmitResult{}, err
		}
	}
	return SubmitResult{}, err
}

func (s *Service) createSubmission(ctx context.Context, principal agentauth.Principal, input normalizedSubmit, keyHash, fingerprint []byte, options resolvedOptions) (SubmitResult, error) {
	var result SubmitResult
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if existing, found, err := s.findExisting(ctx, tx.IdempotencyRecord.Query(), principal.TokenID, keyHash, fingerprint); err != nil {
		return result, err
	} else if found {
		return SubmitResult{Experiment: makeView(existing), Idempotent: true}, nil
	}

	projectRecord, err := tx.Project.Query().Where(project.IDEQ(principal.ProjectID), project.StatusEQ("active")).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrProjectPaused
	}
	if err != nil {
		return result, err
	}
	repositoryRecord, err := tx.Repository.Query().Where(
		repository.IDEQ(options.repositoryID), repository.ProjectIDEQ(projectRecord.ID), repository.StatusEQ("active"),
	).Only(ctx)
	if err != nil {
		return result, optionError(err)
	}
	environmentRecord, err := tx.Environment.Query().Where(
		environment.IDEQ(options.environmentID), environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ("approved"),
	).Only(ctx)
	if err != nil {
		return result, optionError(err)
	}
	profileRecord, err := tx.ResourceProfile.Query().Where(
		resourceprofile.IDEQ(options.resourceProfileID), resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ("active"),
	).Only(ctx)
	if err != nil {
		return result, optionError(err)
	}
	if string(environmentRecord.Backend) != string(profileRecord.Backend) {
		return result, &ValidationError{Message: "environment and resource profile must use the same backend"}
	}
	runtimeSeconds := input.MaxRuntimeSeconds
	if runtimeSeconds == 0 {
		runtimeSeconds = projectRecord.MaxRuntimeSeconds
	}
	if runtimeSeconds <= 0 || runtimeSeconds > projectRecord.MaxRuntimeSeconds {
		return result, &ValidationError{Message: fmt.Sprintf("max_runtime_seconds must be between 1 and %d", projectRecord.MaxRuntimeSeconds)}
	}
	selfHosted := unmeteredBackend(profileRecord.Backend)
	reservation := int64(0)
	if !selfHosted {
		billableSeconds, err := billableRuntimeSeconds(runtimeSeconds, projectRecord.TimeoutExtensionSeconds, projectRecord.TerminationGraceSeconds)
		if err != nil {
			return result, err
		}
		reservation, err = reserveCost(profileRecord.PriceToMilli, profileRecord.GpuNum, billableSeconds)
		if err != nil {
			return result, err
		}
		if reservation > projectRecord.MaxExperimentMilli {
			return result, ErrExperimentCap
		}
	}
	period, err := budgetPeriod(s.now().UTC(), projectRecord.Timezone)
	if err != nil {
		return result, err
	}
	if !selfHosted {
		committed, err := ledgerTotal(ctx, tx, projectRecord.ID, period)
		if err != nil {
			return result, err
		}
		if reservation > projectRecord.MonthlyBudgetMilli || committed > projectRecord.MonthlyBudgetMilli-reservation {
			return result, ErrBudgetExceeded
		}
	}

	publicID := uuid.New()
	outputPath := "/root/autodl-fs/projects/" + projectRecord.PublicID.String() + "/experiments/" + publicID.String() + "/"
	if selfHosted {
		outputPath = "managed://experiments/" + publicID.String() + "/outputs"
	}
	record, err := tx.Experiment.Create().
		SetPublicID(publicID).
		SetTenantID(principal.TenantID).
		SetProjectID(projectRecord.ID).
		SetAgentTokenID(principal.TokenID).
		SetRepositoryID(repositoryRecord.ID).
		SetEnvironmentID(environmentRecord.ID).
		SetResourceProfileID(profileRecord.ID).
		SetCommitSha(input.CommitSHA).
		SetCommand(input.Command).
		SetMaxRuntimeSeconds(runtimeSeconds).
		SetTimeoutExtensionSeconds(projectRecord.TimeoutExtensionSeconds).
		SetTerminationGraceSeconds(projectRecord.TerminationGraceSeconds).
		SetRepositorySnapshot(repositorySnapshot(repositoryRecord, projectRecord.PublicID.String())).
		SetEnvironmentSnapshot(environmentSnapshot(environmentRecord)).
		SetResourceSnapshot(resourceSnapshot(profileRecord)).
		SetSecretNames(input.SecretNames).
		SetOutputPath(outputPath).
		SetReservedCostMilli(reservation).
		Save(ctx)
	if err != nil {
		return result, err
	}
	if _, err := tx.BudgetEntry.Create().
		SetTenantID(principal.TenantID).
		SetProjectID(projectRecord.ID).
		SetExperimentID(record.ID).
		SetPeriod(period).
		SetKind("reservation").
		SetAmountMilli(reservation).
		SetDescription(map[bool]string{true: "unmetered Self-hosted reservation", false: "experiment budget reservation"}[selfHosted]).
		Save(ctx); err != nil {
		return result, err
	}
	if _, err := tx.IdempotencyRecord.Create().
		SetTenantID(principal.TenantID).
		SetAgentTokenID(principal.TokenID).
		SetExperimentID(record.ID).
		SetKeyHash(keyHash).
		SetRequestFingerprint(fingerprint).
		SetExpiresAt(s.now().UTC().Add(idempotencyRetention)).
		Save(ctx); err != nil {
		return result, err
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(principal.TenantID).
		SetActorType("agent_token").
		SetActorID(principal.TokenPublicID).
		SetAction("experiment.submitted").
		SetTargetType("experiment").
		SetTargetID(publicID.String()).
		SetMetadata(map[string]any{"project_id": projectRecord.PublicID.String(), "backend": profileRecord.Backend, "reserved_cost_milli": reservation}).
		Save(ctx); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return SubmitResult{Experiment: makeView(record)}, nil
}

func (s *Service) resolveOptions(ctx context.Context, projectID int, input normalizedSubmit) (resolvedOptions, error) {
	var options resolvedOptions
	repositoryPublicID, err := uuid.Parse(input.RepositoryID)
	if err != nil {
		return options, ErrOptionNotFound
	}
	repositoryRecord, err := s.client.Repository.Query().Where(
		repository.PublicIDEQ(repositoryPublicID), repository.ProjectIDEQ(projectID), repository.StatusEQ("active"),
	).Only(ctx)
	if err != nil {
		return options, optionError(err)
	}
	options.repositoryID = repositoryRecord.ID

	environmentQuery := s.client.Environment.Query().Where(environment.ProjectIDEQ(projectID), environment.StatusEQ("approved"))
	if input.EnvironmentID == "" {
		environmentQuery.Where(environment.IsDefaultEQ(true))
	} else if publicID, err := uuid.Parse(input.EnvironmentID); err == nil {
		environmentQuery.Where(environment.PublicIDEQ(publicID))
	} else {
		return options, ErrOptionNotFound
	}
	environmentRecord, err := environmentQuery.Only(ctx)
	if err != nil {
		return options, optionError(err)
	}
	options.environmentID = environmentRecord.ID

	profileQuery := s.client.ResourceProfile.Query().Where(resourceprofile.ProjectIDEQ(projectID), resourceprofile.StatusEQ("active"))
	if input.ResourceProfileID == "" {
		profileQuery.Where(resourceprofile.IsDefaultEQ(true))
	} else if publicID, err := uuid.Parse(input.ResourceProfileID); err == nil {
		profileQuery.Where(resourceprofile.PublicIDEQ(publicID))
	} else {
		return options, ErrOptionNotFound
	}
	profileRecord, err := profileQuery.Only(ctx)
	if err != nil {
		return options, optionError(err)
	}
	options.resourceProfileID = profileRecord.ID
	return options, nil
}

func (s *Service) findExisting(ctx context.Context, query *ent.IdempotencyRecordQuery, tokenID int, keyHash, fingerprint []byte) (*ent.Experiment, bool, error) {
	record, err := query.Where(
		idempotencyrecord.AgentTokenIDEQ(tokenID), idempotencyrecord.KeyHashEQ(keyHash),
	).WithExperiment().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !hmac.Equal(record.RequestFingerprint, fingerprint) {
		return nil, false, ErrIdempotencyConflict
	}
	if record.Edges.Experiment == nil {
		return nil, false, fmt.Errorf("idempotency record has no experiment")
	}
	return record.Edges.Experiment, true, nil
}

func normalizeSubmit(input SubmitInput) (normalizedSubmit, error) {
	var normalized normalizedSubmit
	normalized.RepositoryID = strings.ToLower(strings.TrimSpace(input.RepositoryID))
	if _, err := uuid.Parse(normalized.RepositoryID); err != nil {
		return normalized, &ValidationError{Message: "valid repository_id is required"}
	}
	if input.EnvironmentID != "" {
		value, err := uuid.Parse(strings.TrimSpace(input.EnvironmentID))
		if err != nil {
			return normalized, &ValidationError{Message: "environment_id must be a UUID"}
		}
		normalized.EnvironmentID = value.String()
	}
	if input.ResourceProfileID != "" {
		value, err := uuid.Parse(strings.TrimSpace(input.ResourceProfileID))
		if err != nil {
			return normalized, &ValidationError{Message: "resource_profile_id must be a UUID"}
		}
		normalized.ResourceProfileID = value.String()
	}
	normalized.CommitSHA = strings.ToLower(strings.TrimSpace(input.CommitSHA))
	if !commitPattern.MatchString(normalized.CommitSHA) {
		return normalized, &ValidationError{Message: "commit_sha must be a full 40- or 64-character hexadecimal SHA"}
	}
	normalized.Command = strings.TrimSpace(input.Command)
	if normalized.Command == "" || len(normalized.Command) > 16384 || strings.ContainsRune(normalized.Command, 0) {
		return normalized, &ValidationError{Message: "command must contain 1 to 16384 bytes and no NUL characters"}
	}
	if input.MaxRuntimeSeconds < 0 {
		return normalized, &ValidationError{Message: "max_runtime_seconds cannot be negative"}
	}
	normalized.MaxRuntimeSeconds = input.MaxRuntimeSeconds
	if !idempotencyPattern.MatchString(strings.TrimSpace(input.IdempotencyKey)) {
		return normalized, &ValidationError{Message: "idempotency_key must contain 8 to 128 URL-safe characters"}
	}
	if len(input.SecretNames) > 0 {
		return normalized, &ValidationError{Message: "secret_names must be omitted because project Secret injection is not available"}
	}
	seen := make(map[string]struct{}, len(input.SecretNames))
	for _, value := range input.SecretNames {
		name := strings.TrimSpace(value)
		if !secretNamePattern.MatchString(name) {
			return normalized, &ValidationError{Message: "secret_names contains an invalid name"}
		}
		seen[name] = struct{}{}
	}
	normalized.SecretNames = make([]string, 0, len(seen))
	for name := range seen {
		normalized.SecretNames = append(normalized.SecretNames, name)
	}
	sort.Strings(normalized.SecretNames)
	return normalized, nil
}

func billableRuntimeSeconds(values ...int) (int64, error) {
	var result int64
	for _, value := range values {
		if value < 0 || result > math.MaxInt64-int64(value) {
			return 0, fmt.Errorf("billable runtime overflow")
		}
		result += int64(value)
	}
	if result <= 0 {
		return 0, fmt.Errorf("billable runtime must be positive")
	}
	return result, nil
}

func reserveCost(priceToMilli int64, gpuCount int, runtimeSeconds int64) (int64, error) {
	if priceToMilli <= 0 || gpuCount <= 0 || runtimeSeconds <= 0 {
		return 0, fmt.Errorf("invalid resource profile price or runtime")
	}
	if runtimeSeconds > math.MaxInt64-startupReserveSeconds-shutdownObservationReserveSeconds {
		return 0, fmt.Errorf("cost reservation overflow")
	}
	seconds := runtimeSeconds + startupReserveSeconds + shutdownObservationReserveSeconds
	if priceToMilli > math.MaxInt64/int64(gpuCount) || priceToMilli*int64(gpuCount) > math.MaxInt64/seconds {
		return 0, fmt.Errorf("cost reservation overflow")
	}
	numerator := priceToMilli * int64(gpuCount) * seconds
	reservation := numerator / 3600
	if numerator%3600 != 0 {
		reservation++
	}
	return reservation, nil
}

func ledgerTotal(ctx context.Context, tx *ent.Tx, projectID int, period string) (int64, error) {
	entries, err := tx.BudgetEntry.Query().Where(
		budgetentry.ProjectIDEQ(projectID), budgetentry.PeriodEQ(period),
	).All(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if entry.AmountMilli > 0 && total > math.MaxInt64-entry.AmountMilli {
			return 0, fmt.Errorf("budget ledger overflow")
		}
		if entry.AmountMilli < 0 && total < math.MinInt64-entry.AmountMilli {
			return 0, fmt.Errorf("budget ledger underflow")
		}
		total += entry.AmountMilli
	}
	return total, nil
}

func budgetPeriod(now time.Time, timezone string) (string, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", fmt.Errorf("load project timezone: %w", err)
	}
	return now.In(location).Format("2006-01"), nil
}

func optionError(err error) error {
	if ent.IsNotFound(err) || ent.IsNotSingular(err) {
		return ErrOptionNotFound
	}
	return err
}

func isRetryableTransaction(err error) bool {
	var sqlState interface{ SQLState() string }
	return errors.As(err, &sqlState) && (sqlState.SQLState() == "40001" || sqlState.SQLState() == "40P01")
}

func repositorySnapshot(record *ent.Repository, projectID string) map[string]any {
	if record == nil {
		return map[string]any{"project_id": projectID, "source": "host"}
	}
	return map[string]any{
		"id": record.PublicID.String(), "project_id": projectID, "name": record.Name, "ssh_url": record.SSHURL,
		"default_branch": record.DefaultBranch, "host_key_fingerprint": record.HostKeyFingerprint,
	}
}

func environmentSnapshot(record *ent.Environment) map[string]any {
	return map[string]any{"id": record.PublicID.String(), "name": record.Name, "backend": record.Backend, "image_uuid": record.ImageUUID}
}

func resourceSnapshot(record *ent.ResourceProfile) map[string]any {
	return map[string]any{
		"id": record.PublicID.String(), "name": record.Name, "backend": record.Backend, "region": record.Region,
		"gpu_names": record.GpuNames, "gpu_num": record.GpuNum, "cuda_from": record.CudaFrom, "cuda_to": record.CudaTo,
		"cpu_from": record.CPUFrom, "cpu_to": record.CPUTo, "memory_from_gb": record.MemoryFromGB, "memory_to_gb": record.MemoryToGB,
		"price_from_milli": record.PriceFromMilli, "price_to_milli": record.PriceToMilli, "reuse_container": record.ReuseContainer,
	}
}

func makeView(record *ent.Experiment) View {
	backend := snapshotString(record.EnvironmentSnapshot, "backend")
	workspacePolicy := "runner_temporary"
	workspacePath := ""
	containerOutputPath := record.OutputPath
	if backend == "self_hosted" {
		workspacePolicy = "container_fixed"
		workspacePath = "/workspace"
		containerOutputPath = "/outputs"
	}
	if backend == "ssh_cloud" {
		workspacePolicy = "host_process"
		workspacePath = snapshotString(record.EnvironmentSnapshot, "working_directory")
		if workspacePath == "" {
			workspacePath = "$HOME"
		}
		containerOutputPath = "GEMCP_OUTPUT_DIR"
	}
	return View{
		ID: record.PublicID.String(), ProjectID: snapshotString(record.RepositorySnapshot, "project_id"),
		RepositoryID: snapshotString(record.RepositorySnapshot, "id"), EnvironmentID: snapshotString(record.EnvironmentSnapshot, "id"),
		ResourceProfileID: snapshotString(record.ResourceSnapshot, "id"), State: record.State, DesiredState: record.DesiredState,
		CommitSHA: record.CommitSha, Command: record.Command, MaxRuntimeSeconds: record.MaxRuntimeSeconds,
		ExecutionMode: string(record.ExecutionMode), Argv: append([]string(nil), record.Argv...),
		ReservedCostMilli: record.ReservedCostMilli, EstimatedCostMilli: record.EstimatedCostMilli, OutputPath: record.OutputPath,
		ProviderResourceID: record.ProviderResourceID, ProviderStatus: record.ProviderStatus, ExitCode: record.ExitCode,
		FailureCode: record.FailureCode, FailureReason: record.FailureReason, LogTail: record.LogTail, Metrics: record.Metrics,
		ExecutionContext: ExecutionContextView{
			RepositoryName: snapshotString(record.RepositorySnapshot, "name"), RepositorySSHURL: snapshotString(record.RepositorySnapshot, "ssh_url"),
			Backend: backend, EnvironmentName: snapshotString(record.EnvironmentSnapshot, "name"), Image: snapshotString(record.EnvironmentSnapshot, "image_uuid"),
			ResourceProfileName: snapshotString(record.ResourceSnapshot, "name"), Region: snapshotString(record.ResourceSnapshot, "region"),
			GPUModels: snapshotStrings(record.ResourceSnapshot, "gpu_names"), GPUNum: snapshotInt(record.ResourceSnapshot, "gpu_num"),
			WorkspacePolicy: workspacePolicy, WorkspacePath: workspacePath, ContainerOutputPath: containerOutputPath,
		},
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, StartedAt: record.StartedAt, DeadlineAt: record.DeadlineAt,
		FinishedAt: record.FinishedAt, CancelRequestedAt: record.CancelRequestedAt,
	}
}

func snapshotString(snapshot map[string]any, key string) string {
	value, _ := snapshot[key].(string)
	return value
}

func snapshotStrings(snapshot map[string]any, key string) []string {
	switch values := snapshot[key].(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if item, ok := value.(string); ok {
				result = append(result, item)
			}
		}
		return result
	default:
		return []string{}
	}
}

func unmeteredBackend(backend resourceprofile.Backend) bool {
	return backend == resourceprofile.BackendSelfHosted || backend == resourceprofile.BackendSSHCloud
}

func snapshotInt(snapshot map[string]any, key string) int {
	switch value := snapshot[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}
