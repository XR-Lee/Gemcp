package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/provideraccount"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	providerservice "github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

type fakeLifecycleAPI struct {
	createInput autodl.PrivateElasticDeploymentCreate
	pages       int
	maxPage     int
	deployments map[int][]autodl.Deployment
}

func (f *fakeLifecycleAPI) CreatePrivateElasticDeployment(_ context.Context, input autodl.PrivateElasticDeploymentCreate) (autodl.DeploymentCreateResult, string, error) {
	f.createInput = input
	return autodl.DeploymentCreateResult{DeploymentUUID: "deployment-1"}, "req-create", nil
}
func (f *fakeLifecycleAPI) ElasticDeployments(_ context.Context, page, _ int, deploymentID string) (autodl.Page[autodl.Deployment], string, error) {
	f.pages++
	list := f.deployments[page]
	if deploymentID != "" && len(list) == 0 {
		list = []autodl.Deployment{{UUID: deploymentID, Status: "running", FinishedNum: 1}}
	}
	return autodl.Page[autodl.Deployment]{List: list, PageIndex: page, PageSize: 100, MaxPage: f.maxPage, ResultTotal: f.maxPage * 100}, "req-list", nil
}
func (f *fakeLifecycleAPI) ElasticContainers(context.Context, string, int, int) (autodl.Page[autodl.Container], string, error) {
	return autodl.Page[autodl.Container]{List: []autodl.Container{{UUID: "container-1", PriceMilli: 1000}}}, "req-containers", nil
}
func (f *fakeLifecycleAPI) ElasticEvents(context.Context, string, int, int, int) (autodl.Page[autodl.ContainerEvent], string, error) {
	return autodl.Page[autodl.ContainerEvent]{}, "req-events", nil
}
func (f *fakeLifecycleAPI) StopElasticDeployment(context.Context, string) (string, error) {
	return "req-stop", nil
}
func (f *fakeLifecycleAPI) DeleteElasticDeployment(context.Context, string) (string, error) {
	return "req-delete", nil
}

func providerFixture(t *testing.T, api lifecycleAPI) (*Provider, *ent.ProviderAccount) {
	t.Helper()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	ciphertext, err := providerservice.EncryptCredential(box, "provider-token-long-enough-for-unit-testing")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewProvider(box, "test", WithLifecycleFactory(func(baseURL, token string) (lifecycleAPI, error) {
		if baseURL != autodl.PrivateBaseURL || token != "provider-token-long-enough-for-unit-testing" {
			return nil, errors.New("unexpected Provider credential")
		}
		return api, nil
	}))
	account := &ent.ProviderAccount{BaseURL: autodl.PrivateBaseURL, Backend: provideraccount.BackendPrivate, Status: provideraccount.StatusActive, CredentialCiphertext: ciphertext}
	return provider, account
}

func TestProviderUsesPrivateDeploymentContract(t *testing.T) {
	api := &fakeLifecycleAPI{deployments: map[int][]autodl.Deployment{}}
	provider, account := providerFixture(t, api)
	providerID, requestIDs, err := provider.Create(context.Background(), account, DeploymentSpec{
		Name: "gemcp-attempt", ImageUUID: "image-1", Command: "runner", CUDAVersion: 118,
		GPUNames: []string{"RTX 3090"}, GPUNum: 1, CPUFrom: 1, CPUTo: 8, MemoryFromGB: 1, MemoryToGB: 32,
		PriceFromMilli: 100, PriceToMilli: 1000, ReuseContainer: true,
	})
	if err != nil || providerID != "deployment-1" || requestIDs["create"] != "req-create" {
		t.Fatalf("Create() id=%q requests=%v err=%v", providerID, requestIDs, err)
	}
	if api.createInput.ContainerTemplate.CUDAVersion != 118 || api.createInput.ContainerTemplate.Command != "runner" || !api.createInput.ReuseContainer {
		t.Fatalf("create input = %+v", api.createInput)
	}
	observation, err := provider.Observe(context.Background(), account, providerID)
	if err != nil || !observation.Found || observation.Deployment.FinishedNum != 1 || len(observation.Containers) != 1 {
		t.Fatalf("Observe() = %+v, %v", observation, err)
	}
}

func TestFindByNameNeverTreatsTruncatedReconciliationAsAbsent(t *testing.T) {
	api := &fakeLifecycleAPI{maxPage: 101, deployments: map[int][]autodl.Deployment{}}
	for page := 1; page <= 100; page++ {
		items := make([]autodl.Deployment, 100)
		for index := range items {
			items[index] = autodl.Deployment{UUID: "other", Name: "other"}
		}
		api.deployments[page] = items
	}
	api.deployments[50] = nil
	provider, account := providerFixture(t, api)
	observation, err := provider.FindByName(context.Background(), account, "gemcp-owned")
	if err == nil || observation.Found || api.pages != 100 {
		t.Fatalf("FindByName() observation=%+v pages=%d err=%v", observation, api.pages, err)
	}
}
