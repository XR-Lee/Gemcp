package imagebake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/XR-Lee/Gemcp/internal/autodl"
)

type InstanceSpec struct {
	Name          string
	BaseImageUUID string
	RecipePath    string
	CommitSHA     string
}

type Provider interface {
	CreateInstance(context.Context, InstanceSpec) (instanceUUID string, err error)
	RunRecipe(ctx context.Context, instanceUUID, recipePath, commitSHA string) error
	StopInstance(ctx context.Context, instanceUUID string) error
	SaveImage(ctx context.Context, instanceUUID, name string) (imageUUID string, err error)
}

type FailClosedProvider struct{}

func NewFailClosedProvider() FailClosedProvider { return FailClosedProvider{} }

func (FailClosedProvider) CreateInstance(context.Context, InstanceSpec) (string, error) {
	return "", ErrProvider
}

func (FailClosedProvider) RunRecipe(context.Context, string, string, string) error {
	return ErrProvider
}

func (FailClosedProvider) StopInstance(context.Context, string) error { return ErrProvider }

func (FailClosedProvider) SaveImage(context.Context, string, string) (string, error) {
	return "", ErrProvider
}

type FakeProvider struct {
	mu              sync.Mutex
	Creates         int
	Recipes         int
	Stops           int
	Saves           int
	LastSpec        InstanceSpec
	ResultImageUUID string
	CreateErr       error
	RecipeErr       error
	StopErr         error
	SaveErr         error
}

func NewFakeProvider(imageUUID string) *FakeProvider {
	if strings.TrimSpace(imageUUID) == "" {
		imageUUID = "image-baked12345"
	}
	return &FakeProvider{ResultImageUUID: imageUUID}
}

func (f *FakeProvider) CreateInstance(_ context.Context, spec InstanceSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Creates++
	f.LastSpec = spec
	if f.CreateErr != nil {
		return "", f.CreateErr
	}
	return "pro-instance-fake", nil
}

func (f *FakeProvider) RunRecipe(_ context.Context, _, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Recipes++
	return f.RecipeErr
}

func (f *FakeProvider) StopInstance(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Stops++
	return f.StopErr
}

func (f *FakeProvider) SaveImage(_ context.Context, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Saves++
	if f.SaveErr != nil {
		return "", f.SaveErr
	}
	return f.ResultImageUUID, nil
}

func (f *FakeProvider) CreateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Creates
}

type AutoDLProvider struct {
	client *autodl.Client
}

func NewAutoDLProvider(client *autodl.Client) *AutoDLProvider {
	return &AutoDLProvider{client: client}
}

func (p *AutoDLProvider) CreateInstance(ctx context.Context, spec InstanceSpec) (string, error) {
	if p == nil || p.client == nil {
		return "", ErrProvider
	}
	instance, _, err := p.client.ProCreateInstance(ctx, autodl.ProInstanceCreate{
		Name:      spec.Name,
		ImageUUID: spec.BaseImageUUID,
		Command:   recipeCommand(spec.RecipePath),
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrProviderCreate, err)
	}
	if strings.TrimSpace(instance.UUID) == "" {
		return "", ErrProviderCreate
	}
	return instance.UUID, nil
}

func (p *AutoDLProvider) RunRecipe(context.Context, string, string, string) error {
	// Recipe is declared on instance create as cmd. There is no separate run API.
	return nil
}

func (p *AutoDLProvider) StopInstance(ctx context.Context, instanceUUID string) error {
	if p == nil || p.client == nil {
		return ErrProvider
	}
	if _, err := p.client.ProStopInstance(ctx, instanceUUID); err != nil {
		return err
	}
	return nil
}

func (p *AutoDLProvider) SaveImage(ctx context.Context, instanceUUID, name string) (string, error) {
	if p == nil || p.client == nil {
		return "", ErrProvider
	}
	image, _, err := p.client.ProSaveImage(ctx, autodl.ProImageSave{InstanceUUID: instanceUUID, Name: name})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(image.UUID) == "" {
		return "", errors.New("AutoDL Pro image save returned no image UUID")
	}
	return image.UUID, nil
}

func recipeCommand(recipePath string) string {
	path := strings.TrimSpace(recipePath)
	if path == "" {
		path = DefaultRecipe
	}
	return "python -m pip install --user -r " + path
}
