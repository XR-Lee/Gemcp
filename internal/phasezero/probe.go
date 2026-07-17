package phasezero

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/autodl"
)

type ReadReport struct {
	GeneratedAt  time.Time             `json:"generated_at"`
	Backend      string                `json:"backend"`
	BaseURL      string                `json:"base_url"`
	Wallet       *autodl.WalletBalance `json:"wallet,omitempty"`
	Images       []ImageSummary        `json:"images"`
	SystemImages []ImageSummary        `json:"system_images,omitempty"`
	Instances    []ProInstanceSummary  `json:"instances,omitempty"`
	Deployments  []DeploymentSummary   `json:"deployments,omitempty"`
	GPUStock     []GPUStockSummary     `json:"gpu_stock,omitempty"`
	RequestIDs   map[string]string     `json:"request_ids,omitempty"`
}

type ImageSummary struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Status      string `json:"status,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	CUDAVersion string `json:"cuda_version,omitempty"`
	ChipCorp    string `json:"chip_corp,omitempty"`
	CPUArch     string `json:"cpu_arch,omitempty"`
}

type ProInstanceSummary struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Region    string `json:"region"`
	GPUAmount int    `json:"gpu_amount"`
	GPUSpec   string `json:"gpu_spec"`
}

type GPUStockSummary struct {
	Name  string `json:"name"`
	Idle  int    `json:"idle"`
	Total int    `json:"total"`
}

type DeploymentSummary struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	ReplicaNum  int    `json:"replica_num"`
	RunningNum  int    `json:"running_num"`
	FinishedNum int    `json:"finished_num"`
}

func RunReadProbe(ctx context.Context, client *autodl.Client, backend, baseURL, region string) (ReadReport, error) {
	report := ReadReport{
		GeneratedAt: time.Now().UTC(),
		Backend:     backend,
		BaseURL:     baseURL,
		RequestIDs:  map[string]string{},
	}
	switch backend {
	case "elastic", "pro":
		wallet, requestID, err := client.WalletBalance(ctx)
		if err != nil {
			return report, fmt.Errorf("read wallet: %w", err)
		}
		report.Wallet = &wallet
		setRequestID(report.RequestIDs, "wallet", requestID)
	case "private":
		// AutoDL Private Cloud has no developer wallet endpoint.
	default:
		return report, fmt.Errorf("unsupported backend %q", backend)
	}

	switch backend {
	case "elastic":
		images, imageRequestID, err := client.ElasticImages(ctx, 1, 100)
		if err != nil {
			return report, fmt.Errorf("read Elastic images: %w", err)
		}
		setRequestID(report.RequestIDs, "images", imageRequestID)
		report.Images = summarizeImages(images.List)
		if strings.TrimSpace(region) == "" {
			return report, fmt.Errorf("region is required for an Elastic read probe")
		}
		stock, stockRequestID, err := client.ElasticGPUStock(ctx, region, nil)
		if err != nil {
			return report, fmt.Errorf("read Elastic GPU stock: %w", err)
		}
		setRequestID(report.RequestIDs, "gpu_stock", stockRequestID)
		for name, entry := range stock {
			report.GPUStock = append(report.GPUStock, GPUStockSummary{Name: name, Idle: entry.Idle, Total: entry.Total})
		}
		sort.Slice(report.GPUStock, func(i, j int) bool { return report.GPUStock[i].Name < report.GPUStock[j].Name })
	case "private":
		images, imageRequestID, err := client.ElasticImages(ctx, 1, 100)
		if err != nil {
			return report, fmt.Errorf("read Private Cloud images: %w", err)
		}
		setRequestID(report.RequestIDs, "images", imageRequestID)
		report.Images = summarizeImages(images.List)

		systemImages, systemImageRequestID, err := client.PrivateSystemImages(ctx, 1, 100)
		if err != nil {
			return report, fmt.Errorf("read Private Cloud system images: %w", err)
		}
		setRequestID(report.RequestIDs, "system_images", systemImageRequestID)
		report.SystemImages = summarizeImages(systemImages.List)

		stock, stockRequestID, err := client.PrivateElasticGPUStock(ctx)
		if err != nil {
			return report, fmt.Errorf("read Private Cloud GPU stock: %w", err)
		}
		setRequestID(report.RequestIDs, "gpu_stock", stockRequestID)
		for name, entry := range stock {
			report.GPUStock = append(report.GPUStock, GPUStockSummary{Name: name, Idle: entry.Idle, Total: entry.Total})
		}
		sort.Slice(report.GPUStock, func(i, j int) bool { return report.GPUStock[i].Name < report.GPUStock[j].Name })

		deployments, deploymentRequestID, err := client.ElasticDeployments(ctx, 1, 100, "")
		if err != nil {
			return report, fmt.Errorf("read Private Cloud deployments: %w", err)
		}
		setRequestID(report.RequestIDs, "deployments", deploymentRequestID)
		for _, deployment := range deployments.List {
			report.Deployments = append(report.Deployments, DeploymentSummary{
				UUID:        deployment.UUID,
				Name:        deployment.Name,
				Type:        deployment.Type,
				Status:      deployment.Status,
				ReplicaNum:  deployment.ReplicaNum,
				RunningNum:  deployment.RunningNum,
				FinishedNum: deployment.FinishedNum,
			})
		}
	case "pro":
		images, imageRequestID, err := client.ProImages(ctx, 1, 100)
		if err != nil {
			return report, fmt.Errorf("read Pro images: %w", err)
		}
		setRequestID(report.RequestIDs, "images", imageRequestID)
		report.Images = summarizeImages(images.List)
		instances, instanceRequestID, err := client.ProInstances(ctx, 1, 100)
		if err != nil {
			return report, fmt.Errorf("read Pro instances: %w", err)
		}
		setRequestID(report.RequestIDs, "instances", instanceRequestID)
		for _, instance := range instances.List {
			report.Instances = append(report.Instances, ProInstanceSummary{
				UUID:      instance.UUID,
				Name:      instance.Name,
				Status:    instance.Status,
				Region:    instance.Region,
				GPUAmount: instance.GPUAmount,
				GPUSpec:   instance.GPUSpecUUID,
			})
		}
	}
	return report, nil
}

func summarizeImages(images []autodl.Image) []ImageSummary {
	result := make([]ImageSummary, 0, len(images))
	for _, image := range images {
		name := image.Name
		if name == "" {
			name = image.FallbackName
		}
		result = append(result, ImageSummary{
			UUID:        image.UUID,
			Name:        name,
			Status:      image.Status,
			SizeBytes:   image.SizeBytes,
			CUDAVersion: image.CUDAVersion,
			ChipCorp:    image.ChipCorp,
			CPUArch:     image.CPUArch,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UUID < result[j].UUID })
	return result
}

func setRequestID(values map[string]string, key, value string) {
	if value != "" {
		values[key] = value
	}
}
