package diagnostic

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/diagnosticrun"
	"github.com/XR-Lee/Gemcp/ent/environment"
	entproject "github.com/XR-Lee/Gemcp/ent/project"
	entrepository "github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
)

const (
	defaultSourceMaxBytes = int64(256 << 20)
	defaultNodeStaleAfter = time.Minute
	maxDiagnosticRuns     = 100
)

var (
	commitPattern      = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	pinnedImagePattern = regexp.MustCompile(`^[^[:space:]@]+@sha256:[0-9a-f]{64}$`)
)

type ArchiveReader interface {
	ArchiveCommit(context.Context, int, string, int64) (gitrepository.Archive, error)
}

type ProviderReader interface {
	QueryResources(context.Context, int) (provider.ResourceSnapshot, error)
}

type RuntimeReader interface {
	Status(context.Context) (execution.RuntimeStatus, error)
}

type Config struct {
	SourceMaxBytes    int64
	SelfHostedEnabled bool
	NodeStaleAfter    time.Duration
}

type Service struct {
	client      *ent.Client
	box         *secrets.Box
	archiver    ArchiveReader
	provider    ProviderReader
	runtime     RuntimeReader
	experiments *experiment.Service
	config      Config
	now         func() time.Time
}

func NewService(client *ent.Client, box *secrets.Box, archiver ArchiveReader, providerReader ProviderReader, runtime RuntimeReader, experiments *experiment.Service, config Config) *Service {
	if config.SourceMaxBytes <= 0 {
		config.SourceMaxBytes = defaultSourceMaxBytes
	}
	if config.NodeStaleAfter <= 0 {
		config.NodeStaleAfter = defaultNodeStaleAfter
	}
	return &Service{
		client: client, box: box, archiver: archiver, provider: providerReader, runtime: runtime,
		experiments: experiments, config: config, now: time.Now,
	}
}

type normalizedInput struct {
	Backend           string `json:"backend"`
	Suite             string `json:"suite"`
	RepositoryID      string `json:"repository_id"`
	EnvironmentID     string `json:"environment_id"`
	ResourceProfileID string `json:"resource_profile_id"`
	CommitSHA         string `json:"commit_sha"`
}

type resolvedInput struct {
	normalized  normalizedInput
	project     *ent.Project
	repository  *ent.Repository
	environment *ent.Environment
	profile     *ent.ResourceProfile
	command     string
	runtime     int
	grace       int
	reservation int64
}

func (s *Service) Options(ctx context.Context, tenantID int, projectID string) (Options, error) {
	var result Options
	projectRecord, err := s.project(ctx, tenantID, projectID)
	if err != nil {
		return result, err
	}
	repositories, err := s.client.Repository.Query().Where(
		entrepository.ProjectIDEQ(projectRecord.ID), entrepository.StatusEQ(entrepository.StatusActive),
	).Order(ent.Asc(entrepository.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	environments, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ(environment.StatusApproved),
	).Order(ent.Asc(environment.FieldBackend), ent.Asc(environment.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	profiles, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Order(ent.Asc(resourceprofile.FieldBackend), ent.Asc(resourceprofile.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	result.ProjectID = projectRecord.PublicID.String()
	result.GeneratedAt = s.now().UTC()
	result.Repositories = make([]RepositoryOption, 0, len(repositories))
	for _, record := range repositories {
		result.Repositories = append(result.Repositories, RepositoryOption{ID: record.PublicID.String(), Name: record.Name, DefaultBranch: record.DefaultBranch})
	}
	result.Environments = make([]EnvironmentOption, 0, len(environments))
	for _, record := range environments {
		result.Environments = append(result.Environments, EnvironmentOption{
			ID: record.PublicID.String(), Name: record.Name, Backend: string(record.Backend), ImageUUID: record.ImageUUID,
		})
	}
	result.Profiles = make([]ProfileOption, 0, len(profiles))
	for _, record := range profiles {
		result.Profiles = append(result.Profiles, ProfileOption{
			ID: record.PublicID.String(), Name: record.Name, Backend: string(record.Backend), GPUNames: append([]string(nil), record.GpuNames...),
			GPUNum: record.GpuNum, PriceToMilli: record.PriceToMilli,
		})
	}
	result.Suites = []SuiteOption{
		{ID: SuiteGPUConnectivity, RuntimeSeconds: 180},
		{ID: SuitePyTorchCUDA, RuntimeSeconds: 300, RequiresPyTorch: true, ChecksCUDACompute: true},
	}
	return result, nil
}

func (s *Service) project(ctx context.Context, tenantID int, value string) (*ent.Project, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, ErrNotFound
	}
	record, err := s.client.Project.Query().Where(
		entproject.PublicIDEQ(publicID), entproject.TenantIDEQ(tenantID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return record, err
}

func (s *Service) resolve(ctx context.Context, tenantID int, projectID string, input PreflightInput) (resolvedInput, error) {
	var result resolvedInput
	normalized, err := normalize(input)
	if err != nil {
		return result, err
	}
	projectRecord, err := s.project(ctx, tenantID, projectID)
	if err != nil {
		return result, err
	}
	if projectRecord.Status != entproject.StatusActive {
		return result, invalid("project must be active")
	}
	repositoryID, _ := uuid.Parse(normalized.RepositoryID)
	repositoryRecord, err := s.client.Repository.Query().Where(
		entrepository.PublicIDEQ(repositoryID), entrepository.ProjectIDEQ(projectRecord.ID), entrepository.StatusEQ(entrepository.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, invalid("repository must be active and belong to the project")
	}
	if err != nil {
		return result, err
	}
	environmentID, _ := uuid.Parse(normalized.EnvironmentID)
	environmentRecord, err := s.client.Environment.Query().Where(
		environment.PublicIDEQ(environmentID), environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ(environment.StatusApproved),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, invalid("environment must be approved and belong to the project")
	}
	if err != nil {
		return result, err
	}
	profileID, _ := uuid.Parse(normalized.ResourceProfileID)
	profileRecord, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.PublicIDEQ(profileID), resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, invalid("resource profile must be active and belong to the project")
	}
	if err != nil {
		return result, err
	}
	if string(environmentRecord.Backend) != normalized.Backend || string(profileRecord.Backend) != normalized.Backend {
		return result, invalid("environment and resource profile must match the requested backend")
	}
	if len(profileRecord.GpuNames) == 0 || profileRecord.CudaFrom > profileRecord.CudaTo ||
		profileRecord.CPUFrom > profileRecord.CPUTo || profileRecord.MemoryFromGB > profileRecord.MemoryToGB ||
		profileRecord.PriceFromMilli > profileRecord.PriceToMilli {
		return result, invalid("resource profile has invalid or empty execution bounds")
	}
	command, runtimeSeconds, err := commandForSuite(normalized.Suite, profileRecord.GpuNum)
	if err != nil {
		return result, err
	}
	if runtimeSeconds > projectRecord.MaxRuntimeSeconds {
		return result, invalid(fmt.Sprintf("project max runtime must be at least %d seconds for this suite", runtimeSeconds))
	}
	grace := projectRecord.TerminationGraceSeconds
	if grace > 30 {
		grace = 30
	}
	reservation, err := diagnosticReservation(profileRecord, runtimeSeconds, grace)
	if err != nil {
		return result, invalid("resource profile cannot produce a bounded diagnostic reservation")
	}
	return resolvedInput{
		normalized: normalized, project: projectRecord, repository: repositoryRecord, environment: environmentRecord,
		profile: profileRecord, command: command, runtime: runtimeSeconds, grace: grace, reservation: reservation,
	}, nil
}

func normalize(input PreflightInput) (normalizedInput, error) {
	var result normalizedInput
	result.Backend = strings.TrimSpace(input.Backend)
	if result.Backend != BackendAutoDL && result.Backend != BackendSelfHosted {
		return result, invalid("backend must be autodl_private or self_hosted")
	}
	result.Suite = strings.TrimSpace(input.Suite)
	if result.Suite != SuiteGPUConnectivity && result.Suite != SuitePyTorchCUDA {
		return result, invalid("suite must be gpu_connectivity or pytorch_cuda")
	}
	parseID := func(source string) (string, error) {
		value, err := uuid.Parse(strings.TrimSpace(source))
		if err != nil {
			return "", invalid("repository_id, environment_id, and resource_profile_id must be UUIDs")
		}
		return value.String(), nil
	}
	var err error
	if result.RepositoryID, err = parseID(input.RepositoryID); err != nil {
		return result, err
	}
	if result.EnvironmentID, err = parseID(input.EnvironmentID); err != nil {
		return result, err
	}
	if result.ResourceProfileID, err = parseID(input.ResourceProfileID); err != nil {
		return result, err
	}
	result.CommitSHA = strings.ToLower(strings.TrimSpace(input.CommitSHA))
	if !commitPattern.MatchString(result.CommitSHA) {
		return result, invalid("commit_sha must be a full 40- or 64-character hexadecimal SHA")
	}
	return result, nil
}

func (s *Service) existing(ctx context.Context, projectID int, keyHash, fingerprint []byte) (*ent.DiagnosticRun, bool, error) {
	record, err := s.client.DiagnosticRun.Query().Where(
		diagnosticrun.ProjectIDEQ(projectID), diagnosticrun.IdempotencyKeyHashEQ(keyHash),
	).WithExperiment().WithProject().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !hmac.Equal(record.RequestFingerprint, fingerprint) {
		return nil, false, ErrIdempotencyConflict
	}
	return record, true, nil
}

func diagnosticFingerprint(input normalizedInput) []byte {
	encoded, _ := json.Marshal(input)
	hash := sha256.Sum256(encoded)
	return hash[:]
}

func proposalFor(resolved resolvedInput) Proposal {
	return Proposal{
		Backend: resolved.normalized.Backend, Suite: resolved.normalized.Suite,
		RepositoryID: resolved.repository.PublicID.String(), EnvironmentID: resolved.environment.PublicID.String(),
		ResourceProfileID: resolved.profile.PublicID.String(), CommitSHA: resolved.normalized.CommitSHA,
		Command: resolved.command, RuntimeSeconds: resolved.runtime, TerminationGraceSeconds: resolved.grace,
		ReservedCostMilli: resolved.reservation, Billable: resolved.normalized.Backend == BackendAutoDL,
		GPUModels: append([]string(nil), resolved.profile.GpuNames...), GPUNum: resolved.profile.GpuNum, ImageUUID: resolved.environment.ImageUUID,
		Region: resolved.profile.Region, CUDAFrom: resolved.profile.CudaFrom, CUDATo: resolved.profile.CudaTo,
		CPUFrom: resolved.profile.CPUFrom, CPUTo: resolved.profile.CPUTo,
		MemoryFromGB: resolved.profile.MemoryFromGB, MemoryToGB: resolved.profile.MemoryToGB,
		PriceFromMilli: resolved.profile.PriceFromMilli, PriceToMilli: resolved.profile.PriceToMilli,
		ReuseContainer: false,
	}
}

func proposalDigest(resolved resolvedInput, proposal Proposal) string {
	material := struct {
		Proposal                     Proposal `json:"proposal"`
		RepositorySSHURL             string   `json:"repository_ssh_url"`
		RepositoryHostKeyFingerprint string   `json:"repository_host_key_fingerprint"`
	}{
		Proposal: proposal, RepositorySSHURL: resolved.repository.SSHURL,
		RepositoryHostKeyFingerprint: resolved.repository.HostKeyFingerprint,
	}
	encoded, _ := json.Marshal(material)
	hash := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", hash[:])
}
