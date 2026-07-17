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

type PrivateElasticContainerTemplate struct {
	CUDAVersion    int      `json:"cuda_v"`
	GPUNames       []string `json:"gpu_name_set"`
	GPUNum         int      `json:"gpu_num"`
	MemoryFromGB   int      `json:"memory_size_from"`
	MemoryToGB     int      `json:"memory_size_to"`
	CPUFrom        int      `json:"cpu_num_from"`
	CPUTo          int      `json:"cpu_num_to"`
	PriceFromMilli int64    `json:"price_from"`
	PriceToMilli   int64    `json:"price_to"`
	ImageUUID      string   `json:"image_uuid"`
	Command        string   `json:"cmd"`
}

type PrivateElasticDeploymentCreate struct {
	Name              string                          `json:"name"`
	DeploymentType    string                          `json:"deployment_type"`
	ReplicaNum        int                             `json:"replica_num"`
	ParallelismNum    int                             `json:"parallelism_num"`
	ReuseContainer    bool                            `json:"reuse_container"`
	ContainerTemplate PrivateElasticContainerTemplate `json:"container_template"`
}

func (c *Client) ElasticImages(ctx context.Context, pageIndex, pageSize int) (Page[Image], string, error) {
	return doJSON[Page[Image]](ctx, c, http.MethodPost, "/api/v1/dev/image/private/list", map[string]int{
		"page_index": pageIndex,
		"page_size":  pageSize,
	}, requestOptions{idempotent: true})
}

func (c *Client) PrivateSystemImages(ctx context.Context, pageIndex, pageSize int) (Page[Image], string, error) {
	return doJSON[Page[Image]](ctx, c, http.MethodPost, "/api/v2/image/list", map[string]int{
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
	return c.gpuStock(ctx, "/api/v1/dev/machine/region/gpu_stock", body)
}

func (c *Client) PrivateElasticGPUStock(ctx context.Context) (GPUStock, string, error) {
	return c.gpuStockWithMethod(ctx, http.MethodGet, "/api/v1/dev/machine/gpu_stock", nil)
}

func (c *Client) gpuStock(ctx context.Context, requestPath string, body any) (GPUStock, string, error) {
	return c.gpuStockWithMethod(ctx, http.MethodPost, requestPath, body)
}

func (c *Client) gpuStockWithMethod(ctx context.Context, method, requestPath string, body any) (GPUStock, string, error) {
	raw, requestID, err := doJSON[json.RawMessage](ctx, c, method, requestPath, body, requestOptions{idempotent: true})
	if err != nil {
		return nil, requestID, err
	}

	stock := GPUStock{}
	var direct map[string]GPUStockEntry
	if err := json.Unmarshal(raw, &direct); err == nil && direct != nil {
		for name, value := range direct {
			stock[name] = value
		}
		return stock, requestID, nil
	}

	var entries []map[string]GPUStockEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, requestID, fmt.Errorf("decode AutoDL GPU stock: %w", err)
	}
	for _, entry := range entries {
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

func (c *Client) CreatePrivateElasticDeployment(ctx context.Context, input PrivateElasticDeploymentCreate) (DeploymentCreateResult, string, error) {
	if err := validatePrivateElasticDeployment(input); err != nil {
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
	return c.ElasticContainersWithReleased(ctx, deploymentUUID, false, pageIndex, pageSize)
}

func (c *Client) ElasticContainersWithReleased(ctx context.Context, deploymentUUID string, released bool, pageIndex, pageSize int) (Page[Container], string, error) {
	return doJSON[Page[Container]](ctx, c, http.MethodPost, "/api/v1/dev/deployment/container/list", map[string]any{
		"deployment_uuid": deploymentUUID,
		"released":        released,
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
	if err := validateDeploymentCommon(input.Name, input.DeploymentType, input.ReplicaNum, input.ParallelismNum); err != nil {
		return err
	}
	template := input.ContainerTemplate
	if len(template.DCList) == 0 || len(template.GPUNames) == 0 || template.GPUNum < 1 {
		return fmt.Errorf("region, GPU names, and GPU count are required")
	}
	return validateContainerCommon(template.ImageUUID, template.Command, template.PriceFromMilli, template.PriceToMilli)
}

func validatePrivateElasticDeployment(input PrivateElasticDeploymentCreate) error {
	if err := validateDeploymentCommon(input.Name, input.DeploymentType, input.ReplicaNum, input.ParallelismNum); err != nil {
		return err
	}
	template := input.ContainerTemplate
	if template.CUDAVersion <= 0 || len(template.GPUNames) == 0 || template.GPUNum < 1 {
		return fmt.Errorf("CUDA version, GPU names, and GPU count are required")
	}
	return validateContainerCommon(template.ImageUUID, template.Command, template.PriceFromMilli, template.PriceToMilli)
}

func validateDeploymentCommon(name, deploymentType string, replicaNum, parallelismNum int) error {
	if name == "" || deploymentType == "" {
		return fmt.Errorf("deployment name and type are required")
	}
	if replicaNum < 1 || parallelismNum < 1 {
		return fmt.Errorf("replica and parallelism must be positive")
	}
	return nil
}

func validateContainerCommon(imageUUID, command string, priceFromMilli, priceToMilli int64) error {
	if imageUUID == "" || command == "" {
		return fmt.Errorf("image UUID and command are required")
	}
	if priceToMilli <= 0 || priceFromMilli < 0 || priceFromMilli > priceToMilli {
		return fmt.Errorf("invalid price range")
	}
	return nil
}
