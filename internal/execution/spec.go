package execution

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/runner"
)

type environmentSnapshot struct {
	Backend   string `json:"backend"`
	ImageUUID string `json:"image_uuid"`
}

type resourceSnapshot struct {
	Backend        string   `json:"backend"`
	Region         string   `json:"region"`
	GPUNames       []string `json:"gpu_names"`
	GPUNum         int      `json:"gpu_num"`
	CUDAFrom       int      `json:"cuda_from"`
	CUDATo         int      `json:"cuda_to"`
	CPUFrom        int      `json:"cpu_from"`
	CPUTo          int      `json:"cpu_to"`
	MemoryFromGB   int      `json:"memory_from_gb"`
	MemoryToGB     int      `json:"memory_to_gb"`
	PriceFromMilli int64    `json:"price_from_milli"`
	PriceToMilli   int64    `json:"price_to_milli"`
	ReuseContainer bool     `json:"reuse_container"`
}

func deploymentSpec(experimentRecord *ent.Experiment, resourceRecord *ent.ProviderResource, runnerToken, publicURL string) (DeploymentSpec, error) {
	var environment environmentSnapshot
	if err := decodeSnapshot(experimentRecord.EnvironmentSnapshot, &environment); err != nil {
		return DeploymentSpec{}, fmt.Errorf("decode environment snapshot: %w", err)
	}
	var resource resourceSnapshot
	if err := decodeSnapshot(experimentRecord.ResourceSnapshot, &resource); err != nil {
		return DeploymentSpec{}, fmt.Errorf("decode resource snapshot: %w", err)
	}
	if strings.TrimSpace(environment.ImageUUID) == "" {
		return DeploymentSpec{}, fmt.Errorf("environment snapshot has no image UUID")
	}
	backend := strings.TrimSpace(resource.Backend)
	if backend == "" {
		backend = "autodl_private"
	}
	if environment.Backend != "" && environment.Backend != backend {
		return DeploymentSpec{}, fmt.Errorf("environment and resource snapshots use different backends")
	}
	switch backend {
	case "autodl_private":
		if resource.Region != "private" || resource.CUDAFrom <= 0 || resource.CUDAFrom != resource.CUDATo {
			return DeploymentSpec{}, fmt.Errorf("resource snapshot is not a Private Cloud profile")
		}
	case "autodl_elastic":
		if strings.TrimSpace(resource.Region) == "" || resource.Region == "private" || resource.CUDAFrom <= 0 || resource.CUDATo < resource.CUDAFrom {
			return DeploymentSpec{}, fmt.Errorf("resource snapshot is not an AutoDL Elastic profile")
		}
	default:
		return DeploymentSpec{}, fmt.Errorf("resource snapshot is not an AutoDL profile")
	}
	if len(resource.GPUNames) == 0 || resource.GPUNum <= 0 || resource.GPUNum > 4 {
		return DeploymentSpec{}, fmt.Errorf("resource snapshot has invalid GPU requirements")
	}
	if resource.CPUFrom <= 0 || resource.CPUTo < resource.CPUFrom || resource.MemoryFromGB <= 0 || resource.MemoryToGB < resource.MemoryFromGB {
		return DeploymentSpec{}, fmt.Errorf("resource snapshot has invalid CPU or memory requirements")
	}
	if resource.PriceFromMilli < 0 || resource.PriceToMilli <= 0 || resource.PriceFromMilli > resource.PriceToMilli {
		return DeploymentSpec{}, fmt.Errorf("resource snapshot has invalid price bounds")
	}
	command, err := runner.LaunchCommandForOutput(publicURL, runnerToken, experimentRecord.OutputPath)
	if err != nil {
		return DeploymentSpec{}, err
	}
	return DeploymentSpec{
		Name: resourceRecord.Name, Backend: backend, Region: resource.Region,
		ImageUUID: environment.ImageUUID, Command: command,
		CUDAFrom: resource.CUDAFrom, CUDATo: resource.CUDATo, GPUNames: resource.GPUNames, GPUNum: resource.GPUNum,
		CPUFrom: resource.CPUFrom, CPUTo: resource.CPUTo,
		MemoryFromGB: resource.MemoryFromGB, MemoryToGB: resource.MemoryToGB,
		PriceFromMilli: resource.PriceFromMilli, PriceToMilli: resource.PriceToMilli,
		ReuseContainer: resource.ReuseContainer,
	}, nil
}

func decodeSnapshot(value map[string]any, target any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}
