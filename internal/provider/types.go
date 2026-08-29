package provider

import "time"

type Summary struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	BaseURL              string     `json:"base_url"`
	Backend              string     `json:"backend"`
	Status               string     `json:"status"`
	CredentialConfigured bool       `json:"credential_configured"`
	LastValidatedAt      *time.Time `json:"last_validated_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ConfigureInput struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

type ConfigureResult struct {
	Provider  Summary          `json:"provider"`
	Resources ResourceSnapshot `json:"resources"`
}

type ResourceSnapshot struct {
	GeneratedAt      time.Time    `json:"generated_at"`
	Provider         Summary      `json:"provider"`
	GPUStock         []GPUStock   `json:"gpu_stock"`
	PrivateImages    []Image      `json:"private_images"`
	SystemImages     []Image      `json:"system_images"`
	Deployments      []Deployment `json:"deployments"`
	ActiveContainers []Container  `json:"active_containers"`
	CachedContainers []Container  `json:"cached_containers"`
	Truncated        []string     `json:"truncated,omitempty"`
}

type GPUStock struct {
	Name   string `json:"name"`
	Region string `json:"region,omitempty"`
	Idle   int    `json:"idle"`
	Total  int    `json:"total"`
}

type Image struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Status      string `json:"status,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	CUDAVersion string `json:"cuda_version,omitempty"`
	ChipCorp    string `json:"chip_corp,omitempty"`
	CPUArch     string `json:"cpu_arch,omitempty"`
	Source      string `json:"source"`
}

type Deployment struct {
	UUID           string     `json:"uuid"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	ReplicaNum     int        `json:"replica_num"`
	ParallelismNum int        `json:"parallelism_num"`
	StartingNum    int        `json:"starting_num"`
	RunningNum     int        `json:"running_num"`
	FinishedNum    int        `json:"finished_num"`
	FailedNum      int        `json:"failed_num"`
	ImageUUID      string     `json:"image_uuid,omitempty"`
	ReuseContainer bool       `json:"reuse_container"`
	PriceEstimate  int64      `json:"price_estimate_milli"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	StoppedAt      *time.Time `json:"stopped_at,omitempty"`
}

type Container struct {
	UUID           string     `json:"uuid"`
	DeploymentUUID string     `json:"deployment_uuid"`
	MachineID      string     `json:"machine_id,omitempty"`
	DataCenter     string     `json:"data_center,omitempty"`
	Status         string     `json:"status"`
	GPUName        string     `json:"gpu_name"`
	GPUNum         int        `json:"gpu_num"`
	CPUNum         int        `json:"cpu_num"`
	MemoryBytes    int64      `json:"memory_bytes"`
	ImageUUID      string     `json:"image_uuid"`
	PriceMilli     int64      `json:"price_milli_per_hour"`
	Released       bool       `json:"released"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	StoppedAt      *time.Time `json:"stopped_at,omitempty"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type Event struct {
	ContainerUUID string    `json:"container_uuid"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type DeploymentDetails struct {
	GeneratedAt        time.Time   `json:"generated_at"`
	Deployment         Deployment  `json:"deployment"`
	ActiveContainers   []Container `json:"active_containers"`
	ReleasedContainers []Container `json:"released_containers"`
	Events             []Event     `json:"events"`
	Truncated          []string    `json:"truncated,omitempty"`
}
