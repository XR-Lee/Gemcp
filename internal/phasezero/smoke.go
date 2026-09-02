package phasezero

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/autodl"
)

type ElasticSmokeOptions struct {
	Region                   string
	ImageUUID                string
	PriceCeilingMilliPerHour int64
	ProvisionTimeoutSeconds  int
}

type elasticSmokeAPI interface {
	ElasticImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error)
	ElasticGPUStock(context.Context, string, map[string]any) (autodl.GPUStock, string, error)
}

func PrepareElasticSmoke(ctx context.Context, api elasticSmokeAPI, options ElasticSmokeOptions) (JobSpec, error) {
	region := strings.TrimSpace(options.Region)
	if region == "" {
		return JobSpec{}, fmt.Errorf("region is required")
	}
	if options.PriceCeilingMilliPerHour <= 0 {
		return JobSpec{}, fmt.Errorf("price ceiling must be positive")
	}

	images, _, err := api.ElasticImages(ctx, 1, 100)
	if err != nil {
		return JobSpec{}, fmt.Errorf("read AutoDL private images: %w", err)
	}
	imageUUID := strings.TrimSpace(options.ImageUUID)
	if imageUUID == "" {
		visible := make([]string, 0, len(images.List))
		for _, image := range images.List {
			if value := strings.TrimSpace(image.UUID); value != "" {
				visible = append(visible, value)
			}
		}
		sort.Strings(visible)
		if len(visible) == 0 {
			return JobSpec{}, fmt.Errorf("AutoDL account has no visible private image")
		}
		imageUUID = visible[0]
	} else {
		found := false
		for _, image := range images.List {
			if strings.TrimSpace(image.UUID) == imageUUID {
				found = true
				break
			}
		}
		if !found {
			return JobSpec{}, fmt.Errorf("requested image is not visible to the AutoDL account")
		}
	}

	stock, _, err := api.ElasticGPUStock(ctx, region, nil)
	if err != nil {
		return JobSpec{}, fmt.Errorf("read AutoDL GPU inventory: %w", err)
	}
	type gpuChoice struct {
		name string
		idle int
	}
	choices := make([]gpuChoice, 0, len(stock))
	for name, entry := range stock {
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, "CPU") || entry.Idle <= 0 {
			continue
		}
		choices = append(choices, gpuChoice{name: name, idle: entry.Idle})
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].idle == choices[j].idle {
			return choices[i].name < choices[j].name
		}
		return choices[i].idle > choices[j].idle
	})
	if len(choices) == 0 {
		return JobSpec{}, fmt.Errorf("AutoDL region %q has no idle GPU", region)
	}
	gpuNames := make([]string, len(choices))
	for index, choice := range choices {
		gpuNames[index] = choice.name
	}

	provisionTimeout := options.ProvisionTimeoutSeconds
	if provisionTimeout == 0 {
		provisionTimeout = 600
	}
	return JobSpec{
		Backend: "elastic", Region: region, GPUNames: gpuNames, GPUNum: 1,
		CUDAFrom: 118, CUDATo: 128, CPUFrom: 1, CPUTo: 128,
		MemoryFromGB: 1, MemoryToGB: 512,
		PriceFromMilliPerHour: 10, PriceToMilliPerHour: options.PriceCeilingMilliPerHour,
		ImageUUID: imageUUID, MaxRuntimeSeconds: 5, ProvisionTimeoutSeconds: provisionTimeout,
		OutputRoot: "/root/autodl-fs/gemcp-phase0", ReuseContainer: false, Minimal: true,
	}, nil
}
