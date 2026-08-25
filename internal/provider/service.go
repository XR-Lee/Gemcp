package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/provideraccount"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/validation"
)

const (
	defaultPublicElasticRegion = "westDC2"
	publicElasticRuntimeName   = "public-elastic"
)

var (
	ErrNotFound           = errors.New("Provider account not found")
	ErrDeploymentNotFound = errors.New("Provider deployment not found")
	ErrUnsupportedBackend = errors.New("Provider backend is not supported")
)

var deploymentIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{6,128}$`)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

type OperationError struct {
	Operation string
	Cause     error
}

func (e *OperationError) Error() string {
	var providerErr *autodl.ProviderError
	if errors.As(e.Cause, &providerErr) {
		if isElasticAccessDenied(e.Cause) {
			detail := "this AutoDL account has no Elastic deployment access; Public Elastic needs enterprise verification, and sub-accounts need 弹性部署 permission"
			if providerErr.RequestID != "" {
				return fmt.Sprintf("AutoDL %s failed: %s (%s)", e.Operation, detail, providerErr.RequestID)
			}
			return fmt.Sprintf("AutoDL %s failed: %s", e.Operation, detail)
		}
		detail := strings.TrimSpace(providerErr.Code)
		message := boundedProviderMessage(providerErr.Message)
		if detail == "" {
			detail = "Provider error"
		}
		if message != "" {
			detail += ": " + message
		}
		if providerErr.RequestID != "" {
			return fmt.Sprintf("AutoDL %s failed: %s (%s)", e.Operation, detail, providerErr.RequestID)
		}
		return fmt.Sprintf("AutoDL %s failed: %s", e.Operation, detail)
	}
	return fmt.Sprintf("AutoDL %s failed", e.Operation)
}

func isElasticAccessDenied(err error) bool {
	var providerErr *autodl.ProviderError
	return errors.As(err, &providerErr) && strings.Contains(providerErr.Message, "无当前资源访问权限")
}

func boundedProviderMessage(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	runes := []rune(message)
	if len(runes) > 240 {
		return string(runes[:240]) + "..."
	}
	return message
}

func (e *OperationError) Unwrap() error { return e.Cause }

type api interface {
	WalletBalance(context.Context) (autodl.WalletBalance, string, error)
	ElasticImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error)
	PrivateSystemImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error)
	ElasticGPUStock(context.Context, string, map[string]any) (autodl.GPUStock, string, error)
	PrivateElasticGPUStock(context.Context) (autodl.GPUStock, string, error)
	ElasticDeployments(context.Context, int, int, string) (autodl.Page[autodl.Deployment], string, error)
	ElasticContainersWithReleased(context.Context, string, bool, int, int) (autodl.Page[autodl.Container], string, error)
	ElasticEvents(context.Context, string, int, int, int) (autodl.Page[autodl.ContainerEvent], string, error)
}

type clientFactory func(baseURL, token string) (api, error)

type Option func(*Service)

func WithClientFactory(factory clientFactory) Option {
	return func(service *Service) {
		if factory != nil {
			service.newClient = factory
		}
	}
}

type Service struct {
	client    *ent.Client
	box       *secrets.Box
	version   string
	now       func() time.Time
	newClient clientFactory
}

func NewService(client *ent.Client, box *secrets.Box, version string, options ...Option) *Service {
	service := &Service{client: client, box: box, version: strings.TrimSpace(version), now: time.Now}
	service.newClient = func(baseURL, token string) (api, error) {
		normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
		if normalized != autodl.DefaultBaseURL && normalized != autodl.PrivateBaseURL {
			return nil, ErrUnsupportedBackend
		}
		userAgent := "Gemcp/" + service.version + " provider-observer"
		return autodl.NewClient(baseURL, token, autodl.WithUserAgent(userAgent))
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Summary(ctx context.Context, tenantID int) (Summary, error) {
	record, err := s.account(ctx, tenantID)
	if err != nil {
		return Summary{}, err
	}
	return summaryFromRecord(record), nil
}

func (s *Service) Summaries(ctx context.Context, tenantID int) ([]Summary, error) {
	records, err := s.accounts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	views := make([]Summary, 0, len(records))
	for _, record := range records {
		views = append(views, summaryFromRecord(record))
	}
	return views, nil
}

func (s *Service) QueryResources(ctx context.Context, tenantID int, backend string) (ResourceSnapshot, error) {
	record, err := s.accountForQuery(ctx, tenantID, backend)
	if err != nil {
		return ResourceSnapshot{}, err
	}
	providerClient, err := s.clientFor(record)
	if err != nil {
		return ResourceSnapshot{}, err
	}
	resolved, err := backendForRecord(record)
	if err != nil {
		return ResourceSnapshot{}, err
	}
	regions, err := s.elasticRegions(ctx, tenantID, resolved)
	if err != nil {
		return ResourceSnapshot{}, err
	}
	snapshot, err := s.collectResources(ctx, providerClient, resolved, regions)
	if err != nil {
		statusCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = record.Update().SetStatus(provideraccount.StatusError).Save(statusCtx)
		cancel()
		return ResourceSnapshot{}, err
	}

	now := s.now().UTC()
	updated, err := record.Update().
		SetBackend(resolved).
		SetStatus(provideraccount.StatusActive).
		SetLastValidatedAt(now).
		Save(ctx)
	if err != nil {
		return ResourceSnapshot{}, fmt.Errorf("update Provider validation state: %w", err)
	}
	snapshot.GeneratedAt = now
	snapshot.Provider = summaryFromRecord(updated)
	return snapshot, nil
}

func (s *Service) Configure(ctx context.Context, tenantID int, actorID string, input ConfigureInput) (ConfigureResult, error) {
	var result ConfigureResult
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 {
		return result, &ValidationError{Message: "Provider name is required and must not exceed 120 characters"}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	backend, err := configuredBackend(baseURL, input.Backend)
	if err != nil {
		return result, err
	}
	token := strings.TrimSpace(input.Token)
	if len(token) < 32 || len(token) > 4096 {
		return result, &ValidationError{Message: "Provider token must contain between 32 and 4096 characters"}
	}
	record, err := s.accountForBackend(ctx, tenantID, backend)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return result, err
	}
	if record != nil {
		if current, currentErr := backendForRecord(record); currentErr == nil && current != backend {
			return result, &ValidationError{Message: "this Provider account is already bound to a different AutoDL backend"}
		}
	}
	if err := s.ensureUniqueProviderName(ctx, tenantID, name, record); err != nil {
		return result, err
	}
	providerClient, err := s.newClient(baseURL, token)
	if err != nil {
		return result, err
	}
	regions, err := s.elasticRegions(ctx, tenantID, backend)
	if err != nil {
		return result, err
	}
	snapshot, err := s.collectResources(ctx, providerClient, backend, regions)
	if err != nil {
		return result, err
	}
	ciphertext, err := EncryptCredential(s.box, token)
	if err != nil {
		return result, err
	}
	now := s.now().UTC()

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return result, fmt.Errorf("begin Provider configuration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var updated *ent.ProviderAccount
	action := "provider.credential_rotated"
	if record == nil {
		action = "provider.account_created"
		updated, err = tx.ProviderAccount.Create().
			SetTenantID(tenantID).
			SetName(name).
			SetBaseURL(baseURL).
			SetBackend(backend).
			SetCredentialCiphertext(ciphertext).
			SetStatus(provideraccount.StatusActive).
			SetLastValidatedAt(now).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("create Provider account: %w", err)
		}
	} else {
		updated, err = tx.ProviderAccount.UpdateOneID(record.ID).
			SetName(name).
			SetBaseURL(baseURL).
			SetBackend(backend).
			SetCredentialCiphertext(ciphertext).
			SetStatus(provideraccount.StatusActive).
			SetLastValidatedAt(now).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("update Provider account: %w", err)
		}
	}
	_, err = tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType(auditevent.ActorTypeUser).
		SetActorID(strings.TrimSpace(actorID)).
		SetAction(action).
		SetTargetType("provider_account").
		SetTargetID(updated.PublicID.String()).
		SetMetadata(map[string]any{"backend": backend, "base_url": baseURL}).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("write Provider audit event: %w", err)
	}
	if backend == provideraccount.BackendElastic {
		if err := ensurePublicElasticRuntimes(ctx, tx, tenantID, snapshot); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit Provider configuration: %w", err)
	}

	snapshot.GeneratedAt = now
	snapshot.Provider = summaryFromRecord(updated)
	result.Provider = snapshot.Provider
	result.Resources = snapshot
	return result, nil
}

func (s *Service) Deployment(ctx context.Context, tenantID int, deploymentID string) (DeploymentDetails, error) {
	var details DeploymentDetails
	deploymentID = strings.TrimSpace(deploymentID)
	if !deploymentIDPattern.MatchString(deploymentID) {
		return details, &ValidationError{Message: "invalid Provider deployment ID"}
	}
	records, err := s.accounts(ctx, tenantID)
	if err != nil {
		return details, err
	}
	if len(records) == 0 {
		return details, ErrNotFound
	}
	foundMissing := false
	var lastErr error
	for _, record := range records {
		details, err = s.deploymentOnAccount(ctx, record, deploymentID)
		if err == nil {
			return details, nil
		}
		if errors.Is(err, ErrDeploymentNotFound) {
			foundMissing = true
			continue
		}
		lastErr = err
	}
	if foundMissing {
		return DeploymentDetails{}, ErrDeploymentNotFound
	}
	if lastErr != nil {
		return DeploymentDetails{}, lastErr
	}
	return DeploymentDetails{}, ErrNotFound
}

func (s *Service) deploymentOnAccount(ctx context.Context, record *ent.ProviderAccount, deploymentID string) (DeploymentDetails, error) {
	var details DeploymentDetails
	providerClient, err := s.clientFor(record)
	if err != nil {
		return details, err
	}

	deployments, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Deployment], string, error) {
		return providerClient.ElasticDeployments(ctx, page, size, deploymentID)
	})
	if err != nil {
		if isElasticAccessDenied(err) {
			return details, ErrDeploymentNotFound
		}
		return details, operationError("deployment query", err)
	}
	if truncated {
		details.Truncated = append(details.Truncated, "deployments")
	}
	var selected *autodl.Deployment
	for index := range deployments {
		if deployments[index].UUID == deploymentID {
			selected = &deployments[index]
			break
		}
	}
	if selected == nil {
		return details, ErrDeploymentNotFound
	}
	active, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Container], string, error) {
		return providerClient.ElasticContainersWithReleased(ctx, deploymentID, false, page, size)
	})
	if err != nil {
		return details, operationError("active-container query", err)
	}
	if truncated {
		details.Truncated = append(details.Truncated, "active_containers")
	}
	released, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Container], string, error) {
		return providerClient.ElasticContainersWithReleased(ctx, deploymentID, true, page, size)
	})
	if err != nil {
		return details, operationError("released-container query", err)
	}
	if truncated {
		details.Truncated = append(details.Truncated, "released_containers")
	}
	events, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.ContainerEvent], string, error) {
		return providerClient.ElasticEvents(ctx, deploymentID, page, size, 0)
	})
	if err != nil {
		return details, operationError("event query", err)
	}
	if truncated {
		details.Truncated = append(details.Truncated, "events")
	}

	details.GeneratedAt = s.now().UTC()
	details.Deployment = deploymentView(*selected)
	details.ActiveContainers = containerViews(active, false)
	details.ReleasedContainers = containerViews(released, true)
	details.Events = eventViews(events)
	return details, nil
}

func (s *Service) accounts(ctx context.Context, tenantID int) ([]*ent.ProviderAccount, error) {
	if s == nil || s.client == nil || s.box == nil {
		return nil, fmt.Errorf("Provider service is not initialized")
	}
	records, err := s.client.ProviderAccount.Query().
		Where(provideraccount.TenantIDEQ(tenantID)).
		Order(ent.Asc(provideraccount.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Provider accounts: %w", err)
	}
	sort.SliceStable(records, func(i, j int) bool {
		return backendRank(records[i]) < backendRank(records[j])
	})
	return records, nil
}

func (s *Service) account(ctx context.Context, tenantID int) (*ent.ProviderAccount, error) {
	records, err := s.accounts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	return records[0], nil
}

func (s *Service) accountForQuery(ctx context.Context, tenantID int, backend string) (*ent.ProviderAccount, error) {
	requested, err := parseBackendFilter(backend)
	if err != nil {
		return nil, err
	}
	if requested != nil {
		return s.accountForBackend(ctx, tenantID, *requested)
	}
	records, err := s.accounts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	if len(records) == 1 {
		return records[0], nil
	}
	return nil, &ValidationError{Message: "Provider backend is required when both Private Cloud and Public Elastic are configured"}
}

func (s *Service) accountForBackend(ctx context.Context, tenantID int, backend provideraccount.Backend) (*ent.ProviderAccount, error) {
	records, err := s.accounts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if recordMatchesBackend(record, backend) {
			return record, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) ensureUniqueProviderName(ctx context.Context, tenantID int, name string, current *ent.ProviderAccount) error {
	records, err := s.accounts(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Name == name && (current == nil || record.ID != current.ID) {
			return &ValidationError{Message: "Provider name is already used by the other AutoDL account"}
		}
	}
	return nil
}

func parseBackendFilter(backend string) (*provideraccount.Backend, error) {
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend == "" {
		return nil, nil
	}
	switch backend {
	case "private":
		value := provideraccount.BackendPrivate
		return &value, nil
	case "elastic":
		value := provideraccount.BackendElastic
		return &value, nil
	default:
		return nil, &ValidationError{Message: "Provider backend must be private or elastic"}
	}
}

func recordMatchesBackend(record *ent.ProviderAccount, backend provideraccount.Backend) bool {
	if record == nil {
		return false
	}
	if resolved, err := backendForRecord(record); err == nil && resolved == backend {
		return true
	}
	if record.Backend == backend {
		return true
	}
	baseURL := strings.TrimRight(strings.TrimSpace(record.BaseURL), "/")
	return (backend == provideraccount.BackendPrivate && baseURL == autodl.PrivateBaseURL) ||
		(backend == provideraccount.BackendElastic && baseURL == autodl.DefaultBaseURL)
}

func backendRank(record *ent.ProviderAccount) int {
	if recordMatchesBackend(record, provideraccount.BackendPrivate) {
		return 0
	}
	if recordMatchesBackend(record, provideraccount.BackendElastic) {
		return 1
	}
	return 2
}

func (s *Service) clientFor(record *ent.ProviderAccount) (api, error) {
	if _, err := backendForRecord(record); err != nil {
		return nil, ErrUnsupportedBackend
	}
	token, err := DecryptCredential(s.box, record.CredentialCiphertext)
	if err != nil {
		return nil, err
	}
	providerClient, err := s.newClient(record.BaseURL, token)
	if err != nil {
		return nil, err
	}
	return providerClient, nil
}

func configuredBackend(baseURL, requested string) (provideraccount.Backend, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		if baseURL == autodl.PrivateBaseURL {
			requested = "private"
		} else {
			requested = "elastic"
		}
	}
	switch requested {
	case "private":
		if baseURL != autodl.PrivateBaseURL {
			return "", &ValidationError{Message: "AutoDL Private Cloud must use https://private.autodl.com"}
		}
		return provideraccount.BackendPrivate, nil
	case "elastic":
		if baseURL != autodl.DefaultBaseURL {
			return "", &ValidationError{Message: "AutoDL Public Elastic must use https://api.autodl.com"}
		}
		return provideraccount.BackendElastic, nil
	default:
		return "", &ValidationError{Message: "Provider backend must be private or elastic"}
	}
}

func backendForRecord(record *ent.ProviderAccount) (provideraccount.Backend, error) {
	if record == nil {
		return "", ErrUnsupportedBackend
	}
	baseURL := strings.TrimRight(strings.TrimSpace(record.BaseURL), "/")
	backend := record.Backend
	if backend == provideraccount.BackendUnverified {
		if baseURL == autodl.PrivateBaseURL {
			backend = provideraccount.BackendPrivate
		} else if baseURL == autodl.DefaultBaseURL {
			backend = provideraccount.BackendElastic
		}
	}
	if (backend == provideraccount.BackendPrivate && baseURL == autodl.PrivateBaseURL) ||
		(backend == provideraccount.BackendElastic && baseURL == autodl.DefaultBaseURL) {
		return backend, nil
	}
	return "", ErrUnsupportedBackend
}

func (s *Service) elasticRegions(ctx context.Context, tenantID int, backend provideraccount.Backend) ([]string, error) {
	if backend != provideraccount.BackendElastic {
		return nil, nil
	}
	records, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.BackendEQ(resourceprofile.BackendAutodlElastic),
		resourceprofile.HasProjectWith(project.TenantIDEQ(tenantID)),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query AutoDL Public Elastic regions: %w", err)
	}
	unique := make(map[string]struct{}, len(records))
	for _, record := range records {
		region := strings.TrimSpace(record.Region)
		if region != "" && region != "private" {
			unique[region] = struct{}{}
		}
	}
	regions := make([]string, 0, len(unique))
	for region := range unique {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	return regions, nil
}

func ensurePublicElasticRuntimes(ctx context.Context, tx *ent.Tx, tenantID int, snapshot ResourceSnapshot) error {
	projects, err := tx.Project.Query().Where(project.TenantIDEQ(tenantID), project.StatusEQ(project.StatusActive)).All(ctx)
	if err != nil {
		return fmt.Errorf("list projects for Public Elastic runtimes: %w", err)
	}
	imageUUID := ""
	if len(snapshot.PrivateImages) > 0 {
		imageUUID = strings.TrimSpace(snapshot.PrivateImages[0].UUID)
	}
	gpuNames := []string{"RTX 4090"}
	region := defaultPublicElasticRegion
	for _, stock := range snapshot.GPUStock {
		if name := strings.TrimSpace(stock.Name); name != "" {
			gpuNames = []string{name}
		}
		if candidate := strings.TrimSpace(stock.Region); candidate != "" && candidate != "private" {
			region = candidate
			break
		}
	}
	for _, projectRecord := range projects {
		profiles, err := tx.ResourceProfile.Query().Where(resourceprofile.ProjectIDEQ(projectRecord.ID)).All(ctx)
		if err != nil {
			return fmt.Errorf("list resource profiles: %w", err)
		}
		envs, err := tx.Environment.Query().Where(environment.ProjectIDEQ(projectRecord.ID)).All(ctx)
		if err != nil {
			return fmt.Errorf("list environments: %w", err)
		}
		var elasticProfile *ent.ResourceProfile
		for _, record := range profiles {
			if record.Backend == resourceprofile.BackendAutodlElastic {
				elasticProfile = record
				break
			}
		}
		if elasticProfile == nil {
			elasticProfile, err = tx.ResourceProfile.Create().
				SetProjectID(projectRecord.ID).
				SetBackend(resourceprofile.BackendAutodlElastic).
				SetName(publicElasticRuntimeName).
				SetRegion(region).
				SetGpuNames(gpuNames).
				SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).
				SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
				SetPriceFromMilli(10).SetPriceToMilli(3000).
				Save(ctx)
			if err != nil {
				return fmt.Errorf("create Public Elastic resource profile: %w", err)
			}
			profiles = append(profiles, elasticProfile)
		}
		var elasticEnv *ent.Environment
		for _, record := range envs {
			if record.Backend == environment.BackendAutodlElastic {
				elasticEnv = record
				break
			}
		}
		if elasticEnv == nil && imageUUID != "" {
			elasticEnv, err = tx.Environment.Create().
				SetProjectID(projectRecord.ID).
				SetBackend(environment.BackendAutodlElastic).
				SetName(publicElasticRuntimeName).
				SetImageUUID(imageUUID).
				SetStatus(environment.StatusApproved).
				Save(ctx)
			if err != nil {
				return fmt.Errorf("create Public Elastic environment: %w", err)
			}
			envs = append(envs, elasticEnv)
		}
		if elasticProfile == nil || elasticEnv == nil {
			continue
		}
		keepPrivateDefault, err := tenantHasPrivateProvider(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		if keepPrivateDefault {
			continue
		}
		if !elasticProfile.IsDefault {
			if _, err := tx.ResourceProfile.UpdateOneID(elasticProfile.ID).SetIsDefault(true).Save(ctx); err != nil {
				return err
			}
		}
		if !elasticEnv.IsDefault {
			if _, err := tx.Environment.UpdateOneID(elasticEnv.ID).SetIsDefault(true).Save(ctx); err != nil {
				return err
			}
		}
		for _, record := range profiles {
			if record.ID == elasticProfile.ID || record.Backend != resourceprofile.BackendAutodlPrivate || !record.IsDefault {
				continue
			}
			if _, err := tx.ResourceProfile.UpdateOneID(record.ID).SetIsDefault(false).Save(ctx); err != nil {
				return err
			}
		}
		for _, record := range envs {
			if record.ID == elasticEnv.ID || record.Backend != environment.BackendAutodlPrivate || !record.IsDefault {
				continue
			}
			if _, err := tx.Environment.UpdateOneID(record.ID).SetIsDefault(false).Save(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func tenantHasPrivateProvider(ctx context.Context, tx *ent.Tx, tenantID int) (bool, error) {
	records, err := tx.ProviderAccount.Query().Where(provideraccount.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return false, fmt.Errorf("list Provider accounts for default runtime selection: %w", err)
	}
	for _, record := range records {
		if recordMatchesBackend(record, provideraccount.BackendPrivate) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) collectResources(ctx context.Context, providerClient api, backend provideraccount.Backend, regions []string) (ResourceSnapshot, error) {
	var snapshot ResourceSnapshot
	privateImages, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Image], string, error) {
		return providerClient.ElasticImages(ctx, page, size)
	})
	if err != nil {
		return snapshot, operationError("private-image query", err)
	}
	if truncated {
		snapshot.Truncated = append(snapshot.Truncated, "private_images")
	}
	var systemImages []autodl.Image
	stockByRegion := map[string]autodl.GPUStock{}
	if backend == provideraccount.BackendPrivate {
		systemImages, truncated, err = collectPages(ctx, func(page, size int) (autodl.Page[autodl.Image], string, error) {
			return providerClient.PrivateSystemImages(ctx, page, size)
		})
		if err != nil {
			if !optionalSystemImagesUnavailable(err) {
				return snapshot, operationError("system-image query", err)
			}
			snapshot.Truncated = append(snapshot.Truncated, "system_images")
			systemImages = nil
		} else if truncated {
			snapshot.Truncated = append(snapshot.Truncated, "system_images")
		}
		stock, _, stockErr := providerClient.PrivateElasticGPUStock(ctx)
		if stockErr != nil {
			return snapshot, operationError("GPU inventory query", stockErr)
		}
		stockByRegion[""] = stock
	} else {
		wallet, _, walletErr := providerClient.WalletBalance(ctx)
		if walletErr != nil {
			return snapshot, operationError("wallet query", walletErr)
		}
		snapshot.Wallet = &Wallet{Assets: wallet.Assets, Accumulate: wallet.Accumulate, VoucherBalance: wallet.VoucherBalance}
		for _, region := range regions {
			stock, _, stockErr := providerClient.ElasticGPUStock(ctx, region, nil)
			if stockErr != nil {
				return snapshot, operationError("GPU inventory query for "+region, stockErr)
			}
			stockByRegion[region] = stock
		}
	}
	deployments, deploymentsTruncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Deployment], string, error) {
		return providerClient.ElasticDeployments(ctx, page, size, "")
	})
	if err != nil {
		return snapshot, operationError("deployment query", err)
	}
	if deploymentsTruncated {
		snapshot.Truncated = append(snapshot.Truncated, "deployments")
	}
	active, activeTruncated, err := collectDeploymentContainers(ctx, providerClient, deployments, false)
	if err != nil {
		return snapshot, operationError("active-container query", err)
	}
	if activeTruncated || deploymentsTruncated {
		snapshot.Truncated = append(snapshot.Truncated, "active_containers")
	}
	cached, cachedTruncated, err := collectDeploymentContainers(ctx, providerClient, deployments, true)
	if err != nil {
		return snapshot, operationError("cache query", err)
	}
	if cachedTruncated || deploymentsTruncated {
		snapshot.Truncated = append(snapshot.Truncated, "cached_containers")
	}

	snapshot.PrivateImages = imageViews(privateImages, "private")
	snapshot.SystemImages = imageViews(systemImages, "system")
	snapshot.GPUStock = stockViews(stockByRegion)
	snapshot.Deployments = deploymentViews(deployments)
	snapshot.ActiveContainers = containerViews(active, false)
	snapshot.CachedContainers = containerViews(cached, true)
	return snapshot, nil
}

func summaryFromRecord(record *ent.ProviderAccount) Summary {
	return Summary{
		ID:                   record.PublicID.String(),
		Name:                 record.Name,
		BaseURL:              record.BaseURL,
		Backend:              string(record.Backend),
		Status:               string(record.Status),
		CredentialConfigured: strings.TrimSpace(record.CredentialCiphertext) != "",
		LastValidatedAt:      record.LastValidatedAt,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
}

func operationError(operation string, err error) error {
	return &OperationError{Operation: operation, Cause: err}
}

func optionalSystemImagesUnavailable(err error) bool {
	var providerErr *autodl.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(providerErr.Code)) {
	case "authorizefailed", "unauthorized":
		return true
	default:
		return false
	}
}

const (
	providerPageSize = 100
	maxProviderPages = 10
)

func collectPages[T any](ctx context.Context, fetch func(page, size int) (autodl.Page[T], string, error)) ([]T, bool, error) {
	items := make([]T, 0)
	for pageIndex := 1; pageIndex <= maxProviderPages; pageIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		page, _, err := fetch(pageIndex, providerPageSize)
		if err != nil {
			return nil, false, err
		}
		items = append(items, page.List...)
		maxPage := page.MaxPage
		if maxPage <= 0 && page.ResultTotal > 0 {
			maxPage = (page.ResultTotal + providerPageSize - 1) / providerPageSize
		}
		if maxPage <= pageIndex || len(page.List) == 0 {
			return items, false, nil
		}
		if pageIndex == maxProviderPages {
			return items, true, nil
		}
	}
	return items, false, nil
}

func collectDeploymentContainers(ctx context.Context, providerClient api, deployments []autodl.Deployment, released bool) ([]autodl.Container, bool, error) {
	items := make([]autodl.Container, 0)
	pagesUsed := 0
	for deploymentIndex, deployment := range deployments {
		deploymentID := strings.TrimSpace(deployment.UUID)
		if deploymentID == "" {
			return items, true, nil
		}
		for pageIndex := 1; ; pageIndex++ {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
			if pagesUsed == maxProviderPages {
				return items, true, nil
			}
			page, _, err := providerClient.ElasticContainersWithReleased(ctx, deploymentID, released, pageIndex, providerPageSize)
			if err != nil {
				return nil, false, err
			}
			pagesUsed++
			items = append(items, page.List...)
			maxPage := page.MaxPage
			if maxPage <= 0 && page.ResultTotal > 0 {
				maxPage = (page.ResultTotal + providerPageSize - 1) / providerPageSize
			}
			hasMore := maxPage > pageIndex || (maxPage <= 0 && len(page.List) == providerPageSize)
			if !hasMore {
				break
			}
		}
		if pagesUsed == maxProviderPages && deploymentIndex < len(deployments)-1 {
			return items, true, nil
		}
	}
	return items, false, nil
}

func imageViews(records []autodl.Image, source string) []Image {
	views := make([]Image, 0, len(records))
	for _, record := range records {
		name := record.Name
		if name == "" {
			name = record.FallbackName
		}
		views = append(views, Image{
			UUID: record.UUID, Name: name, Status: record.Status, SizeBytes: record.SizeBytes,
			CUDAVersion: record.CUDAVersion, ChipCorp: record.ChipCorp, CPUArch: record.CPUArch, Source: source,
		})
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Name == views[j].Name {
			return views[i].UUID < views[j].UUID
		}
		return views[i].Name < views[j].Name
	})
	return views
}

func stockViews(stockByRegion map[string]autodl.GPUStock) []GPUStock {
	count := 0
	for _, stock := range stockByRegion {
		count += len(stock)
	}
	views := make([]GPUStock, 0, count)
	for region, stock := range stockByRegion {
		for name, entry := range stock {
			views = append(views, GPUStock{Region: region, Name: name, Idle: entry.Idle, Total: entry.Total})
		}
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Region == views[j].Region {
			return views[i].Name < views[j].Name
		}
		return views[i].Region < views[j].Region
	})
	return views
}

func deploymentViews(records []autodl.Deployment) []Deployment {
	views := make([]Deployment, 0, len(records))
	for _, record := range records {
		views = append(views, deploymentView(record))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].CreatedAt == nil {
			return false
		}
		if views[j].CreatedAt == nil {
			return true
		}
		return views[i].CreatedAt.After(*views[j].CreatedAt)
	})
	return views
}

func deploymentView(record autodl.Deployment) Deployment {
	return Deployment{
		UUID: record.UUID, Name: record.Name, Type: record.Type, Status: record.Status,
		ReplicaNum: record.ReplicaNum, ParallelismNum: record.ParallelismNum,
		StartingNum: record.StartingNum, RunningNum: record.RunningNum,
		FinishedNum: record.FinishedNum, FailedNum: record.FailedNum,
		ImageUUID: record.ImageUUID, ReuseContainer: record.ReuseContainer,
		PriceEstimate: record.PriceEstimates, CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt, StoppedAt: record.StoppedAt,
	}
}

func containerViews(records []autodl.Container, released bool) []Container {
	views := make([]Container, 0, len(records))
	for _, record := range records {
		views = append(views, Container{
			UUID: record.UUID, DeploymentUUID: record.DeploymentUUID, MachineID: record.MachineID,
			DataCenter: record.DataCenter, Status: record.Status, GPUName: record.GPUName,
			GPUNum: record.GPUNum, CPUNum: record.CPUNum, MemoryBytes: record.MemoryBytes,
			ImageUUID: record.ImageUUID, PriceMilli: record.PriceMilli, Released: released,
			StartedAt: record.StartedAt, StoppedAt: record.StoppedAt,
			CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		})
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].CreatedAt == nil {
			return false
		}
		if views[j].CreatedAt == nil {
			return true
		}
		return views[i].CreatedAt.After(*views[j].CreatedAt)
	})
	return views
}

func eventViews(records []autodl.ContainerEvent) []Event {
	views := make([]Event, 0, len(records))
	for _, record := range records {
		views = append(views, Event{ContainerUUID: record.ContainerUUID, Status: record.Status, CreatedAt: record.CreatedAt})
	}
	sort.SliceStable(views, func(i, j int) bool { return views[i].CreatedAt.After(views[j].CreatedAt) })
	return views
}
