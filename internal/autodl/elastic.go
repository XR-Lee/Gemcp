package autodl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type ElasticContainerTemplate struct {
	DCList            []string `json:"dc_list"`
	CUDAFrom          int      `json:"cuda_v_from"`
	CUDATo            int      `json:"cuda_v_to"`
	GPUNames          []string `json:"gpu_name_set"`
	GPUNum            int      `json:"gpu_num"`
	MemoryFromGB      int      `json:"memory_size_from"`
	MemoryToGB        int      `json:"memory_size_to"`
	CPUFrom           int      `json:"cpu_num_from"`
	CPUTo             int      `json:"cpu_num_to"`
	PriceFromMilli    int64    `json:"price_from"`
	PriceToMilli      int64    `json:"price_to"`
	ImageUUID         string   `json:"image_uuid"`
	Command           string   `json:"cmd"`
	CommandBeforeStop string   `json:"cmd_before_shutdown,omitempty"`
}

type ElasticDeploymentCreate struct {
	Name                string                   `json:"name"`
	DeploymentType      string                   `json:"deployment_type"`
	ReplicaNum          int                      `json:"replica_num"`
	ParallelismNum      int                      `json:"parallelism_num"`
	ReuseContainer      bool                     `json:"reuse_container"`
	ReuseContainerScope string                   `json:"reuse_container_scope,omitempty"`
	ContainerTemplate   ElasticContainerTemplate `json:"container_template"`
}

func (c *Client) ElasticImages(ctx context.Context, pageIndex, pageSize int) (Page[Image], string, error) {
	return doJSON[Page[Image]](ctx, c, http.MethodPost, "/api/v1/dev/image/private/list", map[string]int{
		"page_index": pageIndex,
		"page_size":  pageSize,
	}, requestOptions{idempotent: true})
}

func (c *Client) ElasticGPUStock(ctx context.Context, region string, filters map[string]any) (GPUStock, string, error) {
	body := make(map[string]any, len(filters)+1)
	for key, value := range filters {
		body[key] = value
	}
	body["region_sign"] = region
	raw, requestID, err := doJSON[[]map[string]GPUStockEntry](ctx, c, http.MethodPost, "/api/v1/dev/machine/region/gpu_stock", body, requestOptions{idempotent: true})
	if err != nil {
		return nil, requestID, err
	}
	stock := GPUStock{}
	for _, entry := range raw {
		for name, value := range entry {
			stock[name] = value
		}
	}
	return stock, requestID, nil
}

func (c *Client) CreateElasticDeployment(ctx context.Context, input ElasticDeploymentCreate) (DeploymentCreateResult, string, error) {
	if err := validateElasticDeployment(input); err != nil {
		return DeploymentCreateResult{}, "", err
	}
	return doJSON[DeploymentCreateResult](ctx, c, http.MethodPost, "/api/v1/dev/deployment", input, requestOptions{idempotent: false})
}

func (c *Client) ElasticDeployments(ctx context.Context, pageIndex, pageSize int, deploymentUUID string) (Page[Deployment], string, error) {
	body := map[string]any{"page_index": pageIndex, "page_size": pageSize}
	if deploymentUUID != "" {
		body["deployment_uuid"] = deploymentUUID
	}
	return doJSON[Page[Deployment]](ctx, c, http.MethodPost, "/api/v1/dev/deployment/list", body, requestOptions{idempotent: true})
}

func (c *Client) ElasticContainers(ctx context.Context, deploymentUUID string, pageIndex, pageSize int) (Page[Container], string, error) {
	return doJSON[Page[Container]](ctx, c, http.MethodPost, "/api/v1/dev/deployment/container/list", map[string]any{
		"deployment_uuid": deploymentUUID,
		"released":        false,
		"page_index":      pageIndex,
		"page_size":       pageSize,
	}, requestOptions{idempotent: true})
}

func (c *Client) ElasticEvents(ctx context.Context, deploymentUUID string, pageIndex, pageSize, offset int) (Page[ContainerEvent], string, error) {
	return doJSON[Page[ContainerEvent]](ctx, c, http.MethodPost, "/api/v1/dev/deployment/container/event/list", map[string]any{
		"deployment_uuid": deploymentUUID,
		"page_index":      pageIndex,
		"page_size":       pageSize,
		"offset":          offset,
	}, requestOptions{idempotent: true})
}

func (c *Client) StopElasticDeployment(ctx context.Context, deploymentUUID string) (string, error) {
	_, requestID, err := doJSON[json.RawMessage](ctx, c, http.MethodPut, "/api/v1/dev/deployment/operate", map[string]any{
		"deployment_uuid": deploymentUUID,
		"operate":         "stop",
	}, requestOptions{idempotent: true})
	return requestID, err
}

func (c *Client) DeleteElasticDeployment(ctx context.Context, deploymentUUID string) (string, error) {
	_, requestID, err := doJSON[json.RawMessage](ctx, c, http.MethodDelete, "/api/v1/dev/deployment", map[string]string{
		"deployment_uuid": deploymentUUID,
	}, requestOptions{idempotent: true})
	return requestID, err
}

func validateElasticDeployment(input ElasticDeploymentCreate) error {
	if input.Name == "" || input.DeploymentType == "" {
		return fmt.Errorf("deployment name and type are required")
	}
	if input.ReplicaNum < 1 || input.ParallelismNum < 1 {
		return fmt.Errorf("replica and parallelism must be positive")
	}
	template := input.ContainerTemplate
	if len(template.DCList) == 0 || len(template.GPUNames) == 0 || template.GPUNum < 1 {
		return fmt.Errorf("region, GPU names, and GPU count are required")
	}
	if template.ImageUUID == "" || template.Command == "" {
		return fmt.Errorf("image UUID and command are required")
	}
	if template.PriceToMilli <= 0 || template.PriceFromMilli < 0 || template.PriceFromMilli > template.PriceToMilli {
		return fmt.Errorf("invalid price range")
	}
	return nil
}
