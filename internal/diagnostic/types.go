package diagnostic

import (
	"errors"
	"time"

	"github.com/XR-Lee/Gemcp/internal/experiment"
)

const (
	BackendAutoDL     = "autodl_private"
	BackendSelfHosted = "self_hosted"

	SuiteGPUConnectivity = "gpu_connectivity"
	SuitePyTorchCUDA     = "pytorch_cuda"

	CheckPass = "pass"
	CheckWarn = "warn"
	CheckFail = "fail"
)

var (
	ErrNotFound             = errors.New("diagnostic project or run not found")
	ErrInvalid              = errors.New("invalid diagnostic request")
	ErrPreflightFailed      = errors.New("diagnostic preflight did not pass")
	ErrConfirmationRequired = errors.New("explicit diagnostic confirmation is required")
	ErrProposalChanged      = errors.New("diagnostic proposal changed after confirmation")
	ErrIdempotencyConflict  = errors.New("diagnostic idempotency key was reused with different parameters")
	ErrBudgetExceeded       = errors.New("project budget cannot reserve the diagnostic run")
	ErrExperimentCap        = errors.New("diagnostic reservation exceeds the project experiment cap")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

type RepositoryOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

type EnvironmentOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Backend   string `json:"backend"`
	ImageUUID string `json:"image_uuid"`
}

type ProfileOption struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Backend      string   `json:"backend"`
	GPUNames     []string `json:"gpu_names"`
	GPUNum       int      `json:"gpu_num"`
	PriceToMilli int64    `json:"price_to_milli"`
}

type SuiteOption struct {
	ID                string `json:"id"`
	RuntimeSeconds    int    `json:"runtime_seconds"`
	RequiresPyTorch   bool   `json:"requires_pytorch"`
	ChecksCUDACompute bool   `json:"checks_cuda_compute"`
}

type Options struct {
	ProjectID    string              `json:"project_id"`
	Repositories []RepositoryOption  `json:"repositories"`
	Environments []EnvironmentOption `json:"environments"`
	Profiles     []ProfileOption     `json:"resource_profiles"`
	Suites       []SuiteOption       `json:"suites"`
	GeneratedAt  time.Time           `json:"generated_at"`
}

type PreflightInput struct {
	Backend           string `json:"backend"`
	Suite             string `json:"suite"`
	RepositoryID      string `json:"repository_id"`
	EnvironmentID     string `json:"environment_id"`
	ResourceProfileID string `json:"resource_profile_id"`
	CommitSHA         string `json:"commit_sha"`
}

type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type Proposal struct {
	Backend                 string   `json:"backend"`
	Suite                   string   `json:"suite"`
	RepositoryID            string   `json:"repository_id"`
	EnvironmentID           string   `json:"environment_id"`
	ResourceProfileID       string   `json:"resource_profile_id"`
	CommitSHA               string   `json:"commit_sha"`
	Command                 string   `json:"command"`
	RuntimeSeconds          int      `json:"runtime_seconds"`
	TerminationGraceSeconds int      `json:"termination_grace_seconds"`
	ReservedCostMilli       int64    `json:"reserved_cost_milli"`
	Billable                bool     `json:"billable"`
	GPUModels               []string `json:"gpu_models"`
	GPUNum                  int      `json:"gpu_num"`
	ImageUUID               string   `json:"image_uuid"`
	Region                  string   `json:"region"`
	CUDAFrom                int      `json:"cuda_from"`
	CUDATo                  int      `json:"cuda_to"`
	CPUFrom                 int      `json:"cpu_from"`
	CPUTo                   int      `json:"cpu_to"`
	MemoryFromGB            int      `json:"memory_from_gb"`
	MemoryToGB              int      `json:"memory_to_gb"`
	PriceFromMilli          int64    `json:"price_from_milli"`
	PriceToMilli            int64    `json:"price_to_milli"`
	ReuseContainer          bool     `json:"reuse_container"`
}

type Preflight struct {
	Eligible             bool      `json:"eligible"`
	RequiresConfirmation bool      `json:"requires_confirmation"`
	Checks               []Check   `json:"checks"`
	Proposal             Proposal  `json:"proposal"`
	ConfirmationDigest   string    `json:"confirmation_digest"`
	GeneratedAt          time.Time `json:"generated_at"`
}

type SubmitInput struct {
	PreflightInput
	IdempotencyKey     string `json:"idempotency_key"`
	ConfirmationDigest string `json:"confirmation_digest"`
	Confirmed          bool   `json:"confirmed"`
}

type SubmitResult struct {
	Run        RunView `json:"run"`
	Idempotent bool    `json:"idempotent"`
}

type Assessment struct {
	Status          string   `json:"status"`
	Classification  string   `json:"classification"`
	Summary         string   `json:"summary"`
	Recommendations []string `json:"recommendations,omitempty"`
	CleanupComplete bool     `json:"cleanup_complete"`
}

type AttemptObservation struct {
	ID              string         `json:"id"`
	Number          int            `json:"number"`
	State           string         `json:"state"`
	SourceDownloads int            `json:"source_downloads"`
	ExitCode        *int           `json:"exit_code,omitempty"`
	LogTail         *string        `json:"log_tail,omitempty"`
	Metrics         map[string]any `json:"metrics,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	FailureCode     *string        `json:"failure_code,omitempty"`
	FailureReason   *string        `json:"failure_reason,omitempty"`
}

type BackendObservation struct {
	Kind            string     `json:"kind"`
	ID              string     `json:"id"`
	State           string     `json:"state"`
	Status          *string    `json:"status,omitempty"`
	StopReason      *string    `json:"stop_reason,omitempty"`
	LastError       *string    `json:"last_error,omitempty"`
	StopRequestedAt *time.Time `json:"stop_requested_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

type TimelineEvent struct {
	At     time.Time `json:"at"`
	Code   string    `json:"code"`
	Detail string    `json:"detail,omitempty"`
}

type RunView struct {
	ID           string               `json:"id"`
	ProjectID    string               `json:"project_id"`
	Backend      string               `json:"backend"`
	Suite        string               `json:"suite"`
	RequestedBy  string               `json:"requested_by"`
	Preflight    Preflight            `json:"preflight"`
	Experiment   experiment.View      `json:"experiment"`
	Assessment   Assessment           `json:"assessment"`
	Attempts     []AttemptObservation `json:"attempts"`
	BackendState *BackendObservation  `json:"backend_observation,omitempty"`
	Timeline     []TimelineEvent      `json:"timeline"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

type ExperimentSummary struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type RunSummary struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	Backend     string            `json:"backend"`
	Suite       string            `json:"suite"`
	RequestedBy string            `json:"requested_by"`
	Experiment  ExperimentSummary `json:"experiment"`
	Assessment  Assessment        `json:"assessment"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type ListResult struct {
	Runs []RunSummary `json:"runs"`
}
