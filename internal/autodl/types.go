package autodl

import (
	"encoding/json"
	"fmt"
	"time"
)

const SuccessCode = "Success"

type envelope struct {
	Code      string          `json:"code"`
	Data      json.RawMessage `json:"data"`
	Message   string          `json:"msg"`
	RequestID string          `json:"request_id"`
}

type ProviderError struct {
	HTTPStatus int
	Code       string
	Message    string
	RequestID  string
}

func (e *ProviderError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("autodl request failed: status=%d code=%s request_id=%s message=%s", e.HTTPStatus, e.Code, e.RequestID, e.Message)
	}
	return fmt.Sprintf("autodl request failed: status=%d code=%s message=%s", e.HTTPStatus, e.Code, e.Message)
}

type WalletBalance struct {
	Assets         int64 `json:"assets"`
	Accumulate     int64 `json:"accumulate"`
	VoucherBalance int64 `json:"voucher_balance"`
}

type Image struct {
	UUID         string    `json:"image_uuid"`
	Name         string    `json:"image_name"`
	FallbackName string    `json:"name,omitempty"`
	Status       string    `json:"status,omitempty"`
	SizeBytes    int64     `json:"image_size,omitempty"`
	CreatedAt    time.Time `json:"-"`
}

type Page[T any] struct {
	List        []T `json:"list"`
	PageIndex   int `json:"page_index"`
	PageSize    int `json:"page_size"`
	MaxPage     int `json:"max_page"`
	ResultTotal int `json:"result_total"`
}

type GPUStockEntry struct {
	Idle  int `json:"idle_gpu_num"`
	Total int `json:"total_gpu_num"`
}

type GPUStock map[string]GPUStockEntry

type Deployment struct {
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	Type           string `json:"deployment_type"`
	Status         string `json:"status"`
	ReplicaNum     int    `json:"replica_num"`
	StartingNum    int    `json:"starting_num"`
	RunningNum     int    `json:"running_num"`
	FinishedNum    int    `json:"finished_num"`
	PriceEstimates int64  `json:"price_estimates"`
}

type DeploymentCreateResult struct {
	DeploymentUUID string `json:"deployment_uuid"`
}

type Container struct {
	UUID           string     `json:"uuid"`
	DeploymentUUID string     `json:"deployment_uuid"`
	DataCenter     string     `json:"data_center"`
	Status         string     `json:"status"`
	GPUName        string     `json:"gpu_name"`
	GPUNum         int        `json:"gpu_num"`
	CPUNum         int        `json:"cpu_num"`
	MemoryBytes    int64      `json:"memory_size"`
	ImageUUID      string     `json:"image_uuid"`
	PriceMilli     int64      `json:"price"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	StoppedAt      *time.Time `json:"stopped_at,omitempty"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
}

type ContainerEvent struct {
	ContainerUUID string    `json:"deployment_container_uuid"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type ProInstance struct {
	UUID         string     `json:"uuid"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Region       string     `json:"region_sign"`
	GPUAmount    int        `json:"req_gpu_amount"`
	GPUSpecUUID  string     `json:"gpu_spec_uuid"`
	ChargeType   string     `json:"charge_type"`
	StartedAt    *time.Time `json:"-"`
	ProviderData any        `json:"-"`
}
