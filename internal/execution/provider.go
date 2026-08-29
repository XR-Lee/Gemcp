package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/provideraccount"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	providerservice "github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

type DeploymentSpec struct {
	Name           string
	Backend        string
	Region         string
	ImageUUID      string
	Command        string
	CUDAFrom       int
	CUDATo         int
	GPUNames       []string
	GPUNum         int
	CPUFrom        int
	CPUTo          int
	MemoryFromGB   int
	MemoryToGB     int
	PriceFromMilli int64
	PriceToMilli   int64
	ReuseContainer bool
}

type Observation struct {
	Found      bool
	Deployment autodl.Deployment
	Containers []autodl.Container
	Events     []autodl.ContainerEvent
	RequestIDs map[string]string
	ObservedAt time.Time
}

type lifecycleAPI interface {
	CreateElasticDeployment(context.Context, autodl.ElasticDeploymentCreate) (autodl.DeploymentCreateResult, string, error)
	CreatePrivateElasticDeployment(context.Context, autodl.PrivateElasticDeploymentCreate) (autodl.DeploymentCreateResult, string, error)
	ElasticDeployments(context.Context, int, int, string) (autodl.Page[autodl.Deployment], string, error)
	ElasticContainers(context.Context, string, int, int) (autodl.Page[autodl.Container], string, error)
	ElasticEvents(context.Context, string, int, int, int) (autodl.Page[autodl.ContainerEvent], string, error)
	StopElasticDeployment(context.Context, string) (string, error)
	DeleteElasticDeployment(context.Context, string) (string, error)
}

type lifecycleFactory func(string, string) (lifecycleAPI, error)

type Provider struct {
	box       *secrets.Box
	version   string
	newClient lifecycleFactory
	now       func() time.Time
}

type ProviderOption func(*Provider)

func WithLifecycleFactory(factory lifecycleFactory) ProviderOption {
	return func(provider *Provider) {
		if factory != nil {
			provider.newClient = factory
		}
	}
}

func NewProvider(box *secrets.Box, version string, options ...ProviderOption) *Provider {
	provider := &Provider{box: box, version: strings.TrimSpace(version), now: time.Now}
	provider.newClient = func(baseURL, token string) (lifecycleAPI, error) {
		return autodl.NewClient(baseURL, token, autodl.WithUserAgent("Gemcp/"+provider.version+" executor"))
	}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (p *Provider) Create(ctx context.Context, account *ent.ProviderAccount, spec DeploymentSpec) (string, map[string]string, error) {
	client, err := p.client(account)
	if err != nil {
		return "", nil, err
	}
	var result autodl.DeploymentCreateResult
	var requestID string
	switch account.Backend {
	case provideraccount.BackendElastic:
		result, requestID, err = client.CreateElasticDeployment(ctx, autodl.ElasticDeploymentCreate{
			Name: spec.Name, DeploymentType: "Job", ReplicaNum: 1, ParallelismNum: 1, ReuseContainer: spec.ReuseContainer,
			ContainerTemplate: autodl.ElasticContainerTemplate{
				DCList: []string{spec.Region}, CUDAFrom: spec.CUDAFrom, CUDATo: spec.CUDATo,
				GPUNames: spec.GPUNames, GPUNum: spec.GPUNum,
				MemoryFromGB: spec.MemoryFromGB, MemoryToGB: spec.MemoryToGB,
				CPUFrom: spec.CPUFrom, CPUTo: spec.CPUTo,
				PriceFromMilli: spec.PriceFromMilli, PriceToMilli: spec.PriceToMilli,
				ImageUUID: spec.ImageUUID, Command: spec.Command,
			},
		})
	case provideraccount.BackendPrivate:
		result, requestID, err = client.CreatePrivateElasticDeployment(ctx, autodl.PrivateElasticDeploymentCreate{
			Name: spec.Name, DeploymentType: "Job", ReplicaNum: 1, ParallelismNum: 1, ReuseContainer: spec.ReuseContainer,
			ContainerTemplate: autodl.PrivateElasticContainerTemplate{
				CUDAVersion: spec.CUDAFrom, GPUNames: spec.GPUNames, GPUNum: spec.GPUNum,
				MemoryFromGB: spec.MemoryFromGB, MemoryToGB: spec.MemoryToGB,
				CPUFrom: spec.CPUFrom, CPUTo: spec.CPUTo,
				PriceFromMilli: spec.PriceFromMilli, PriceToMilli: spec.PriceToMilli,
				ImageUUID: spec.ImageUUID, Command: spec.Command,
			},
		})
	default:
		return "", nil, providerservice.ErrUnsupportedBackend
	}
	requestIDs := requestMap("create", requestID)
	if err != nil {
		return "", requestIDs, err
	}
	if strings.TrimSpace(result.DeploymentUUID) == "" {
		return "", requestIDs, fmt.Errorf("Provider create returned no deployment UUID")
	}
	return result.DeploymentUUID, requestIDs, nil
}

func (p *Provider) Observe(ctx context.Context, account *ent.ProviderAccount, providerID string) (Observation, error) {
	client, err := p.client(account)
	if err != nil {
		return Observation{}, err
	}
	page, requestID, err := client.ElasticDeployments(ctx, 1, 10, providerID)
	observation := Observation{RequestIDs: requestMap("deployment", requestID), ObservedAt: p.now().UTC()}
	if err != nil {
		if isProviderNotFound(err) {
			return observation, nil
		}
		return observation, err
	}
	for _, deployment := range page.List {
		if deployment.UUID != providerID {
			continue
		}
		observation.Found = true
		observation.Deployment = deployment
		containers, containerRequestID, containerErr := client.ElasticContainers(ctx, providerID, 1, 100)
		if containerRequestID != "" {
			observation.RequestIDs["containers"] = containerRequestID
		}
		if containerErr != nil {
			return observation, containerErr
		}
		observation.Containers = containers.List
		events, eventRequestID, eventErr := client.ElasticEvents(ctx, providerID, 1, 100, 0)
		if eventRequestID != "" {
			observation.RequestIDs["events"] = eventRequestID
		}
		if eventErr == nil {
			observation.Events = events.List
		}
		return observation, nil
	}
	return observation, nil
}

func (p *Provider) FindByName(ctx context.Context, account *ent.ProviderAccount, name string) (Observation, error) {
	const maxReconcilePages = 100
	client, err := p.client(account)
	if err != nil {
		return Observation{}, err
	}
	observation := Observation{RequestIDs: map[string]string{}, ObservedAt: p.now().UTC()}
	for pageIndex := 1; pageIndex <= maxReconcilePages; pageIndex++ {
		page, requestID, err := client.ElasticDeployments(ctx, pageIndex, 100, "")
		if requestID != "" {
			observation.RequestIDs["reconcile"] = requestID
		}
		if err != nil {
			return observation, err
		}
		for _, deployment := range page.List {
			if deployment.Name != name {
				continue
			}
			if observation.Found {
				return Observation{}, fmt.Errorf("multiple Provider deployments match owned name %q", name)
			}
			observation.Found = true
			observation.Deployment = deployment
		}
		maxPage := page.MaxPage
		if maxPage <= 0 && page.ResultTotal > 0 {
			maxPage = (page.ResultTotal + 99) / 100
		}
		hasMore := maxPage > pageIndex || (maxPage <= 0 && len(page.List) == 100)
		if !hasMore {
			break
		}
		if pageIndex == maxReconcilePages {
			return Observation{}, fmt.Errorf("Provider deployment reconciliation exceeded %d pages", maxReconcilePages)
		}
	}
	return observation, nil
}

func (p *Provider) Stop(ctx context.Context, account *ent.ProviderAccount, providerID string) (map[string]string, error) {
	client, err := p.client(account)
	if err != nil {
		return nil, err
	}
	requestID, err := client.StopElasticDeployment(ctx, providerID)
	if err != nil && !isProviderNotFound(err) && !isAlreadyStopped(err) {
		return requestMap("stop", requestID), err
	}
	return requestMap("stop", requestID), nil
}

func (p *Provider) Delete(ctx context.Context, account *ent.ProviderAccount, providerID string) (map[string]string, error) {
	client, err := p.client(account)
	if err != nil {
		return nil, err
	}
	requestID, err := client.DeleteElasticDeployment(ctx, providerID)
	if err != nil && !isProviderNotFound(err) {
		return requestMap("delete", requestID), err
	}
	return requestMap("delete", requestID), nil
}

func (p *Provider) client(account *ent.ProviderAccount) (lifecycleAPI, error) {
	if p == nil || p.box == nil || account == nil {
		return nil, fmt.Errorf("execution Provider is not initialized")
	}
	baseURL := strings.TrimRight(account.BaseURL, "/")
	validBackend := (baseURL == autodl.PrivateBaseURL && account.Backend == provideraccount.BackendPrivate) ||
		(baseURL == autodl.DefaultBaseURL && account.Backend == provideraccount.BackendElastic)
	if !validBackend || account.Status != provideraccount.StatusActive {
		return nil, providerservice.ErrUnsupportedBackend
	}
	token, err := providerservice.DecryptCredential(p.box, account.CredentialCiphertext)
	if err != nil {
		return nil, err
	}
	return p.newClient(account.BaseURL, token)
}

func requestMap(operation, requestID string) map[string]string {
	result := map[string]string{}
	if requestID != "" {
		result[operation] = requestID
	}
	return result
}

func isProviderNotFound(err error) bool {
	var providerErr *autodl.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	code := strings.ToLower(providerErr.Code + " " + providerErr.Message)
	return providerErr.HTTPStatus == 404 || strings.Contains(code, "not found") || strings.Contains(code, "not_exist")
}

func isAlreadyStopped(err error) bool {
	var providerErr *autodl.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	value := strings.ToLower(providerErr.Code + " " + providerErr.Message)
	return strings.Contains(value, "already") && (strings.Contains(value, "stop") || strings.Contains(value, "shutdown"))
}
