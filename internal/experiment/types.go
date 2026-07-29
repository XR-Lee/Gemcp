package experiment

import (
	"time"

	"github.com/XR-Lee/Gemcp/internal/executionmeta"
)

type ExecutionContextView struct {
	AgentLabel          string                     `json:"agent_label,omitempty"`
	AgentTokenPrefix    string                     `json:"agent_token_prefix,omitempty"`
	ProposalID          string                     `json:"proposal_id,omitempty"`
	RepositoryName      string                     `json:"repository_name"`
	RepositorySSHURL    string                     `json:"repository_ssh_url"`
	RequestedRef        string                     `json:"requested_ref,omitempty"`
	Backend             string                     `json:"backend"`
	EnvironmentName     string                     `json:"environment_name"`
	Image               string                     `json:"image"`
	ResourceProfileName string                     `json:"resource_profile_name"`
	Region              string                     `json:"region"`
	GPUModels           []string                   `json:"gpu_models"`
	GPUNum              int                        `json:"gpu_num"`
	WorkspacePolicy     string                     `json:"workspace_policy"`
	WorkspacePath       string                     `json:"workspace_path,omitempty"`
	ContainerOutputPath string                     `json:"container_output_path"`
	RuntimeInfo         *executionmeta.RuntimeInfo `json:"runtime_info,omitempty"`
}

type BackendObservationView struct {
	Kind            string     `json:"kind"`
	ID              string     `json:"id"`
	ProviderID      string     `json:"provider_id,omitempty"`
	State           string     `json:"state"`
	Status          string     `json:"status,omitempty"`
	NodeLabel       string     `json:"node_label,omitempty"`
	StopReason      string     `json:"stop_reason,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	CleanupComplete bool       `json:"cleanup_complete"`
	UpdatedAt       time.Time  `json:"updated_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

type TimelineEvent struct {
	At     time.Time `json:"at"`
	Code   string    `json:"code"`
	Detail string    `json:"detail,omitempty"`
}

type SubmitInput struct {
	RepositoryID      string   `json:"repository_id" jsonschema:"ID of an active project repository"`
	EnvironmentID     string   `json:"environment_id,omitempty" jsonschema:"approved environment ID; omit to use the project default"`
	ResourceProfileID string   `json:"resource_profile_id,omitempty" jsonschema:"active resource profile ID; omit to use the project default"`
	CommitSHA         string   `json:"commit_sha" jsonschema:"full 40- or 64-character Git commit SHA"`
	Command           string   `json:"command" jsonschema:"shell command to execute in the approved image"`
	MaxRuntimeSeconds int      `json:"max_runtime_seconds,omitempty" jsonschema:"hard runtime limit; omit to use the project maximum"`
	SecretNames       []string `json:"secret_names,omitempty" jsonschema:"reserved for a later release; must be omitted"`
	IdempotencyKey    string   `json:"idempotency_key" jsonschema:"unique retry key, 8 to 128 characters"`
}

type View struct {
	ID                    string                  `json:"id"`
	ProjectID             string                  `json:"project_id"`
	RepositoryID          string                  `json:"repository_id"`
	EnvironmentID         string                  `json:"environment_id"`
	ResourceProfileID     string                  `json:"resource_profile_id"`
	State                 string                  `json:"state"`
	DesiredState          string                  `json:"desired_state"`
	CommitSHA             string                  `json:"commit_sha"`
	ExecutionMode         string                  `json:"execution_mode"`
	Argv                  []string                `json:"argv,omitempty"`
	Command               string                  `json:"command"`
	MaxRuntimeSeconds     int                     `json:"max_runtime_seconds"`
	ReservedCostMilli     int64                   `json:"reserved_cost_milli"`
	EstimatedCostMilli    int64                   `json:"estimated_cost_milli"`
	OutputPath            string                  `json:"output_path"`
	ProviderResourceID    *string                 `json:"provider_resource_id,omitempty"`
	ProviderStatus        *string                 `json:"provider_status,omitempty"`
	ExitCode              *int                    `json:"exit_code,omitempty"`
	FailureCode           *string                 `json:"failure_code,omitempty"`
	FailureReason         *string                 `json:"failure_reason,omitempty"`
	RunnerAttemptID       *string                 `json:"runner_attempt_id,omitempty"`
	RunnerSourceDownloads *int                    `json:"runner_source_downloads,omitempty"`
	RunnerStage           *string                 `json:"runner_stage,omitempty"`
	RunnerStageUpdatedAt  *time.Time              `json:"runner_stage_updated_at,omitempty"`
	RunnerErrorType       *string                 `json:"runner_error_type,omitempty"`
	LogTail               *string                 `json:"log_tail,omitempty"`
	Metrics               map[string]any          `json:"metrics,omitempty"`
	ExecutionContext      ExecutionContextView    `json:"execution_context"`
	BackendObservation    *BackendObservationView `json:"backend_observation,omitempty"`
	Timeline              []TimelineEvent         `json:"timeline,omitempty"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
	StartedAt             *time.Time              `json:"started_at,omitempty"`
	DeadlineAt            *time.Time              `json:"deadline_at,omitempty"`
	FinishedAt            *time.Time              `json:"finished_at,omitempty"`
	CancelRequestedAt     *time.Time              `json:"cancel_requested_at,omitempty"`
}

type AttemptView struct {
	ID                 string         `json:"id"`
	Number             int            `json:"number"`
	State              string         `json:"state"`
	ProviderResourceID *string        `json:"provider_resource_id,omitempty"`
	RetryReason        *string        `json:"retry_reason,omitempty"`
	FailureCode        *string        `json:"failure_code,omitempty"`
	FailureReason      *string        `json:"failure_reason,omitempty"`
	StartedAt          *time.Time     `json:"started_at,omitempty"`
	FinishedAt         *time.Time     `json:"finished_at,omitempty"`
	EstimatedCostMilli int64          `json:"estimated_cost_milli"`
	ExitCode           *int           `json:"exit_code,omitempty"`
	LogTail            *string        `json:"log_tail,omitempty"`
	Metrics            map[string]any `json:"metrics,omitempty"`
	LastHeartbeatAt    *time.Time     `json:"last_heartbeat_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type SubmitResult struct {
	Experiment View `json:"experiment"`
	Idempotent bool `json:"idempotent"`
}

type ListInput struct {
	Limit  int      `json:"limit,omitempty" jsonschema:"maximum results from 1 to 100"`
	States []string `json:"states,omitempty" jsonschema:"optional experiment states"`
}

type ListResult struct {
	Experiments []View `json:"experiments"`
}

type GetInput struct {
	ExperimentID string `json:"experiment_id" jsonschema:"experiment ID"`
}

type CancelInput struct {
	ExperimentID string `json:"experiment_id" jsonschema:"experiment ID"`
}

type ProjectOptions struct {
	Project          ProjectPolicy           `json:"project"`
	Repositories     []RepositoryOption      `json:"repositories"`
	Environments     []EnvironmentOption     `json:"environments"`
	ResourceProfiles []ResourceProfileOption `json:"resource_profiles"`
	SelfHostedNodes  []SelfHostedNodeOption  `json:"self_hosted_nodes"`
}

type ProjectPolicy struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	MonthlyBudgetMilli      int64  `json:"monthly_budget_milli"`
	MaxExperimentMilli      int64  `json:"max_experiment_milli"`
	MaxConcurrency          int    `json:"max_concurrency"`
	MaxRuntimeSeconds       int    `json:"max_runtime_seconds"`
	TimeoutExtensionSeconds int    `json:"timeout_extension_seconds"`
	TerminationGraceSeconds int    `json:"termination_grace_seconds"`
	Timezone                string `json:"timezone"`
}

type RepositoryOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
}

type EnvironmentOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Backend   string `json:"backend"`
	ImageUUID string `json:"image_uuid"`
	IsDefault bool   `json:"is_default"`
}

type ResourceProfileOption struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Backend        string   `json:"backend"`
	Region         string   `json:"region"`
	GPUNames       []string `json:"gpu_names"`
	GPUNum         int      `json:"gpu_num"`
	PriceFromMilli int64    `json:"price_from_milli"`
	PriceToMilli   int64    `json:"price_to_milli"`
	ReuseContainer bool     `json:"reuse_container"`
	IsDefault      bool     `json:"is_default"`
}

type SelfHostedNodeOption struct {
	ID                string                `json:"id"`
	Label             string                `json:"label"`
	Status            string                `json:"status"`
	ObservedState     string                `json:"observed_state"`
	AgentVersion      string                `json:"agent_version"`
	GPUs              []SelfHostedGPUOption `json:"gpus"`
	ExecutionModes    []string              `json:"execution_modes"`
	ExecutionPolicy   string                `json:"execution_policy"`
	WorkspacePath     string                `json:"workspace_path,omitempty"`
	SuccessfulImages  []string              `json:"successful_images,omitempty"`
	WorkspaceCapable  bool                  `json:"workspace_capable"`
	LastSeenAt        *time.Time            `json:"last_seen_at,omitempty"`
	RuntimeConfigured bool                  `json:"runtime_configured"`
	Ready             bool                  `json:"ready"`
	Readiness         string                `json:"readiness"`
	Blockers          []string              `json:"blockers"`
}

type SelfHostedGPUOption struct {
	Name        string `json:"name"`
	MemoryBytes int64  `json:"memory_bytes"`
}

type CostView struct {
	Period             string `json:"period"`
	MonthlyBudgetMilli int64  `json:"monthly_budget_milli"`
	ReservedMilli      int64  `json:"reserved_milli"`
	ChargedMilli       int64  `json:"charged_milli"`
	AdjustmentsMilli   int64  `json:"adjustments_milli"`
	CommittedMilli     int64  `json:"committed_milli"`
	AvailableMilli     int64  `json:"available_milli"`
}

type ArtifactView struct {
	ExperimentID string   `json:"experiment_id"`
	OutputPath   string   `json:"output_path"`
	Artifacts    []string `json:"artifacts"`
}
