package experiment

import "time"

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
	ID                    string         `json:"id"`
	ProjectID             string         `json:"project_id"`
	RepositoryID          string         `json:"repository_id"`
	EnvironmentID         string         `json:"environment_id"`
	ResourceProfileID     string         `json:"resource_profile_id"`
	State                 string         `json:"state"`
	DesiredState          string         `json:"desired_state"`
	CommitSHA             string         `json:"commit_sha"`
	ExecutionMode         string         `json:"execution_mode"`
	Argv                  []string       `json:"argv,omitempty"`
	Command               string         `json:"command"`
	MaxRuntimeSeconds     int            `json:"max_runtime_seconds"`
	ReservedCostMilli     int64          `json:"reserved_cost_milli"`
	EstimatedCostMilli    int64          `json:"estimated_cost_milli"`
	OutputPath            string         `json:"output_path"`
	ProviderResourceID    *string        `json:"provider_resource_id,omitempty"`
	ProviderStatus        *string        `json:"provider_status,omitempty"`
	ExitCode              *int           `json:"exit_code,omitempty"`
	FailureCode           *string        `json:"failure_code,omitempty"`
	FailureReason         *string        `json:"failure_reason,omitempty"`
	RunnerAttemptID       *string        `json:"runner_attempt_id,omitempty"`
	RunnerSourceDownloads *int           `json:"runner_source_downloads,omitempty"`
	RunnerStage           *string        `json:"runner_stage,omitempty"`
	RunnerStageUpdatedAt  *time.Time     `json:"runner_stage_updated_at,omitempty"`
	RunnerErrorType       *string        `json:"runner_error_type,omitempty"`
	Metrics               map[string]any `json:"metrics,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	StartedAt             *time.Time     `json:"started_at,omitempty"`
	DeadlineAt            *time.Time     `json:"deadline_at,omitempty"`
	FinishedAt            *time.Time     `json:"finished_at,omitempty"`
	CancelRequestedAt     *time.Time     `json:"cancel_requested_at,omitempty"`
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
