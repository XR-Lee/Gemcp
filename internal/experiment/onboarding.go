package experiment

import (
	"context"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/provider"
)

func publicCloudOnboarding(options ProjectOptions) *ExecutionOnboarding {
	bindings := 0
	provisionable := 0
	environments := 0
	lockedImage := ""
	backend := datasetcatalog.BackendElastic
	for _, binding := range options.DatasetBindings {
		if binding.Backend == datasetcatalog.BackendElastic || binding.Backend == datasetcatalog.BackendPrivate {
			bindings++
			if len(binding.Sources) > 0 && binding.Status == "active" {
				provisionable++
			}
		}
	}
	for _, env := range options.Environments {
		if env.Backend == datasetcatalog.BackendElastic || env.Backend == datasetcatalog.BackendPrivate {
			environments++
			if env.IsDefault || lockedImage == "" {
				lockedImage = env.ImageUUID
				backend = env.Backend
			}
		}
	}
	steps := make([]OnboardingStep, 0, 4)
	if bindings == 0 {
		steps = append(steps, OnboardingStep{
			Tool:   "register_dataset_binding",
			Reason: "Probe and train on Public Elastic require a /root/autodl-fs dataset binding. Do not use register_workspace_dataset; that tool only declares paths under an Owner-approved Self-hosted workspace.",
			Example: map[string]any{
				"catalog": "scanobjectnn-objbg",
				"sources": []map[string]string{{
					"url": "https://huggingface.co/datasets/example/resolve/main/train.h5", "relative_path": "main_split/training_objectdataset_augmentedrot_scale75.h5",
				}},
			},
		})
	} else if provisionable == 0 {
		steps = append(steps, OnboardingStep{
			Tool:   "register_dataset_binding",
			Reason: "The binding is declared but has no allowlisted HTTPS sources. Re-register it with sources, then prepare_experiment with runtime_preset=provision. The Agent must not write wget or curl.",
		})
	} else {
		steps = append(steps, OnboardingStep{
			Tool:   "prepare_experiment",
			Reason: "A provision run downloads the registered sources onto AutoDL file storage using a Gemcp-owned fetch. Confirm the digest, then prepare smoke, probe, or train.",
			Example: map[string]any{"runtime_preset": "provision", "dataset": "scanobjectnn-objbg"},
		})
	}
	if environments == 0 {
		steps = append(steps, OnboardingStep{
			Tool:   "register_environment",
			Reason: "No AutoDL Environment is registered. Register a Provider-visible image from get_project_options.provider_images. Official image-* UUIDs are Owner-only unless already used on the Project.",
			Example: map[string]any{"name": "torch-train", "backend": backend, "image_uuid": "image-visible"},
		})
	} else {
		steps = append(steps, OnboardingStep{
			Tool:   "register_environment",
			Reason: "The AutoDL image is Owner-locked. If that image lacks the training stack, register another Provider-visible image. Do not wrap argv in conda or compile mamba; set install_dependencies or change the image.",
			Example: map[string]any{"name": "torch-train", "backend": backend, "image_uuid": lockedImage},
		})
	}
	return &ExecutionOnboarding{PublicCloud: PublicCloudOnboarding{
		Backend: backend, DatasetBindings: bindings, ProvisionableBindings: provisionable,
		Environments: environments, LockedImage: lockedImage, NextSteps: steps,
	}}
}

func catalogSourceOptions() []DatasetSourceOption {
	entries := datasetcatalog.Catalog()
	result := make([]DatasetSourceOption, 0, len(entries))
	for _, entry := range entries {
		result = append(result, DatasetSourceOption{
			Name: entry.Name, DisplayName: entry.DisplayName, Backend: entry.Backend,
			CanonicalRoot: entry.CanonicalRoot, RequiredMarkers: append([]string(nil), entry.RequiredMarkers...),
			Notes: entry.Notes,
		})
	}
	return result
}

func (s *Service) appendProviderImages(ctx context.Context, tenantID int, environments []*ent.Environment, profiles []*ent.ResourceProfile, result *ProjectOptions) {
	result.ProviderImages = []ProviderImageOption{}
	if s.providerReader == nil {
		return
	}
	wantElastic, wantPrivate := false, false
	for _, record := range appendEnvironmentsAndProfiles(environments, profiles) {
		switch record {
		case string(resourceprofile.BackendAutodlElastic):
			wantElastic = true
		case string(resourceprofile.BackendAutodlPrivate):
			wantPrivate = true
		}
	}
	seen := map[string]bool{}
	for _, backend := range []string{"elastic", "private"} {
		if backend == "elastic" && !wantElastic || backend == "private" && !wantPrivate {
			continue
		}
		queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		snapshot, err := s.providerReader.QueryResources(queryCtx, tenantID, backend)
		cancel()
		if err != nil {
			continue
		}
		for _, image := range append(append([]provider.Image{}, snapshot.PrivateImages...), snapshot.SystemImages...) {
			if image.UUID == "" || seen[image.UUID] {
				continue
			}
			seen[image.UUID] = true
			result.ProviderImages = append(result.ProviderImages, ProviderImageOption{
				UUID: image.UUID, Name: image.Name, Source: image.Source, CUDAVersion: image.CUDAVersion,
			})
		}
	}
}

func appendEnvironmentsAndProfiles(environments []*ent.Environment, profiles []*ent.ResourceProfile) []string {
	backends := make([]string, 0, len(environments)+len(profiles))
	for _, record := range environments {
		backends = append(backends, string(record.Backend))
	}
	for _, record := range profiles {
		backends = append(backends, string(record.Backend))
	}
	return backends
}

func datasetBindingSources(record *ent.DatasetBinding) []DatasetBindingSource {
	sources := datasetcatalog.SourcesFromRecord(record.Sources)
	result := make([]DatasetBindingSource, 0, len(sources))
	for _, source := range sources {
		result = append(result, DatasetBindingSource{URL: source.URL, RelativePath: source.RelativePath, SHA256: source.SHA256})
	}
	return result
}
