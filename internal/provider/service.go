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
	"github.com/XR-Lee/Gemcp/ent/provideraccount"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

var (
	ErrNotFound           = errors.New("Provider account not found")
	ErrDeploymentNotFound = errors.New("Provider deployment not found")
	ErrUnsupportedBackend = errors.New("Provider backend is not supported")
)

var deploymentIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{6,128}$`)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type OperationError struct {
	Operation string
	Cause     error
}

func (e *OperationError) Error() string {
	var providerErr *autodl.ProviderError
	if errors.As(e.Cause, &providerErr) {
		detail := strings.TrimSpace(providerErr.Code)
		message := boundedProviderMessage(providerErr.Message)
		if detail == "" {
			detail = "Provider error"
		}
		if message != "" {
			detail += ": " + message
		}
		if providerErr.RequestID != "" {
			return fmt.Sprintf("Private Cloud %s failed: %s (%s)", e.Operation, detail, providerErr.RequestID)
		}
		return fmt.Sprintf("Private Cloud %s failed: %s", e.Operation, detail)
	}
	return fmt.Sprintf("Private Cloud %s failed", e.Operation)
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
	ElasticImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error)
	PrivateSystemImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error)
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
		if strings.TrimRight(strings.TrimSpace(baseURL), "/") != autodl.PrivateBaseURL {
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

func (s *Service) QueryResources(ctx context.Context, tenantID int) (ResourceSnapshot, error) {
	record, err := s.account(ctx, tenantID)
	if err != nil {
		return ResourceSnapshot{}, err
	}
	providerClient, err := s.clientFor(record)
	if err != nil {
		return ResourceSnapshot{}, err
	}
	snapshot, err := s.collectResources(ctx, providerClient)
	if err != nil {
		statusCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = record.Update().SetStatus(provideraccount.StatusError).Save(statusCtx)
		cancel()
		return ResourceSnapshot{}, err
	}

	now := s.now().UTC()
	updated, err := record.Update().
		SetBackend(provideraccount.BackendPrivate).
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
	if baseURL != autodl.PrivateBaseURL {
		return result, &ValidationError{Message: "Provider API URL must be https://private.autodl.com"}
	}
	token := strings.TrimSpace(input.Token)
	if len(token) < 32 || len(token) > 4096 {
		return result, &ValidationError{Message: "Provider token must contain between 32 and 4096 characters"}
	}
	record, err := s.account(ctx, tenantID)
	if err != nil {
		return result, err
	}

	providerClient, err := s.newClient(baseURL, token)
	if err != nil {
		return result, err
	}
	snapshot, err := s.collectResources(ctx, providerClient)
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
	updated, err := tx.ProviderAccount.UpdateOneID(record.ID).
		SetName(name).
		SetBaseURL(baseURL).
		SetBackend(provideraccount.BackendPrivate).
		SetCredentialCiphertext(ciphertext).
		SetStatus(provideraccount.StatusActive).
		SetLastValidatedAt(now).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("update Provider account: %w", err)
	}
	_, err = tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType(auditevent.ActorTypeUser).
		SetActorID(strings.TrimSpace(actorID)).
		SetAction("provider.credential_rotated").
		SetTargetType("provider_account").
		SetTargetID(updated.PublicID.String()).
		SetMetadata(map[string]any{"backend": "private", "base_url": baseURL}).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("write Provider audit event: %w", err)
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
	record, err := s.account(ctx, tenantID)
	if err != nil {
		return details, err
	}
	providerClient, err := s.clientFor(record)
	if err != nil {
		return details, err
	}

	deployments, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Deployment], string, error) {
		return providerClient.ElasticDeployments(ctx, page, size, deploymentID)
	})
	if err != nil {
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

func (s *Service) account(ctx context.Context, tenantID int) (*ent.ProviderAccount, error) {
	if s == nil || s.client == nil || s.box == nil {
		return nil, fmt.Errorf("Provider service is not initialized")
	}
	record, err := s.client.ProviderAccount.Query().
		Where(provideraccount.TenantIDEQ(tenantID)).
		Order(ent.Asc(provideraccount.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query Provider account: %w", err)
	}
	return record, nil
}

func (s *Service) clientFor(record *ent.ProviderAccount) (api, error) {
	if strings.TrimRight(record.BaseURL, "/") != autodl.PrivateBaseURL {
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

func (s *Service) collectResources(ctx context.Context, providerClient api) (ResourceSnapshot, error) {
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
	systemImages, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Image], string, error) {
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
	stock, _, err := providerClient.PrivateElasticGPUStock(ctx)
	if err != nil {
		return snapshot, operationError("GPU inventory query", err)
	}
	deployments, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Deployment], string, error) {
		return providerClient.ElasticDeployments(ctx, page, size, "")
	})
	if err != nil {
		return snapshot, operationError("deployment query", err)
	}
	if truncated {
		snapshot.Truncated = append(snapshot.Truncated, "deployments")
	}
	active, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Container], string, error) {
		return providerClient.ElasticContainersWithReleased(ctx, "", false, page, size)
	})
	if err != nil {
		return snapshot, operationError("active-container query", err)
	}
	if truncated {
		snapshot.Truncated = append(snapshot.Truncated, "active_containers")
	}
	cached, truncated, err := collectPages(ctx, func(page, size int) (autodl.Page[autodl.Container], string, error) {
		return providerClient.ElasticContainersWithReleased(ctx, "", true, page, size)
	})
	if err != nil {
		return snapshot, operationError("cache query", err)
	}
	if truncated {
		snapshot.Truncated = append(snapshot.Truncated, "cached_containers")
	}

	snapshot.PrivateImages = imageViews(privateImages, "private")
	snapshot.SystemImages = imageViews(systemImages, "system")
	snapshot.GPUStock = stockViews(stock)
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

func stockViews(stock autodl.GPUStock) []GPUStock {
	views := make([]GPUStock, 0, len(stock))
	for name, entry := range stock {
		views = append(views, GPUStock{Name: name, Idle: entry.Idle, Total: entry.Total})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
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
