package experiment

import (
	"context"
	"errors"
	"time"

	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
)

const (
	ProposalCheckPass = "pass"
	ProposalCheckWarn = "warn"
	ProposalCheckFail = "fail"
)

var (
	ErrProposalNotFound = errors.New("experiment proposal not found")
	ErrProposalExpired  = errors.New("experiment proposal expired")
	ErrProposalChanged  = errors.New("experiment proposal changed after confirmation")
	ErrProposalBlocked  = errors.New("experiment proposal preflight did not pass")
)

type ProposalRefResolver interface {
	ResolveRef(context.Context, int, string) (string, error)
}

type ProposalArchiveReader interface {
	ArchiveCommit(context.Context, int, string, int64) (gitrepository.Archive, error)
}

type ProposalProviderReader interface {
	QueryResources(context.Context, int) (provider.ResourceSnapshot, error)
}

type ProposalRuntimeReader interface {
	Status(context.Context) (execution.RuntimeStatus, error)
}

type ProposalConfig struct {
	SourceMaxBytes    int64
	SelfHostedEnabled bool
	NodeStaleAfter    time.Duration
	Lifetime          time.Duration
}

type PrepareInput struct {
	Repository        string   `json:"repository,omitempty" jsonschema:"repository name or ID; omit when the Project has exactly one active repository"`
	RepositoryRemote  string   `json:"repository_remote,omitempty" jsonschema:"registered Git remote; omit when the Project has exactly one active repository"`
	Ref               string   `json:"ref,omitempty" jsonschema:"branch, tag, or full commit; omit to use the repository default branch"`
	Argv              []string `json:"argv" jsonschema:"ordered program argument vector; shell interpreters are rejected"`
	RuntimePreset     string   `json:"runtime_preset,omitempty" jsonschema:"runtime preset; phase one supports smoke"`
	MaxRuntimeSeconds int      `json:"max_runtime_seconds,omitempty" jsonschema:"runtime at most 300 seconds for the smoke preset"`
	Environment       string   `json:"environment,omitempty" jsonschema:"approved environment name or ID; omit to resolve a compatible default"`
	ResourceProfile   string   `json:"resource_profile,omitempty" jsonschema:"active resource profile name or ID; omit to resolve a compatible default"`
	Image             string   `json:"image,omitempty" jsonschema:"public OCI image tag or digest; accepted only by an Owner-approved trusted Self-hosted workspace"`
}

type ProposalChoice struct {
	Field   string `json:"field"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Backend string `json:"backend,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type ProposalCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type ProposalRepository struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	SSHURL             string `json:"ssh_url"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	RequestedRef       string `json:"requested_ref"`
	CommitSHA          string `json:"commit_sha"`
	DefaultBranch      string `json:"default_branch"`
}

type ProposalExecution struct {
	Mode           string   `json:"mode"`
	Argv           []string `json:"argv"`
	DisplayCommand string   `json:"display_command"`
}

type ProposalResource struct {
	EnvironmentID       string   `json:"environment_id"`
	EnvironmentName     string   `json:"environment_name"`
	ResourceProfileID   string   `json:"resource_profile_id"`
	ResourceProfileName string   `json:"resource_profile_name"`
	Backend             string   `json:"backend"`
	Image               string   `json:"image"`
	GPUModels           []string `json:"gpu_models"`
	GPUNum              int      `json:"gpu_num"`
	Region              string   `json:"region"`
	CUDAFrom            int      `json:"cuda_from"`
	CUDATo              int      `json:"cuda_to"`
	CPUFrom             int      `json:"cpu_from"`
	CPUTo               int      `json:"cpu_to"`
	MemoryFromGB        int      `json:"memory_from_gb"`
	MemoryToGB          int      `json:"memory_to_gb"`
	PriceFromMilli      int64    `json:"price_from_milli"`
	PriceToMilli        int64    `json:"price_to_milli"`
	ReuseContainer      bool     `json:"reuse_container"`
	Billable            bool     `json:"billable"`
	ExecutionPolicy     string   `json:"execution_policy,omitempty"`
	WorkspacePath       string   `json:"workspace_path,omitempty"`
	NodeID              string   `json:"node_id,omitempty"`
	NodeLabel           string   `json:"node_label,omitempty"`
	ImageMutable        bool     `json:"image_mutable,omitempty"`
}

type PreparedProposal struct {
	ID                      string             `json:"id"`
	ProjectID               string             `json:"project_id"`
	Eligible                bool               `json:"eligible"`
	RequiresConfirmation    bool               `json:"requires_confirmation"`
	Repository              ProposalRepository `json:"repository"`
	Execution               ProposalExecution  `json:"execution"`
	Resource                ProposalResource   `json:"resource"`
	RuntimePreset           string             `json:"runtime_preset"`
	MaxRuntimeSeconds       int                `json:"max_runtime_seconds"`
	TimeoutExtensionSeconds int                `json:"timeout_extension_seconds"`
	TerminationGraceSeconds int                `json:"termination_grace_seconds"`
	ReservedCostMilli       int64              `json:"reserved_cost_milli"`
	ReservedCostCNY         string             `json:"reserved_cost_cny"`
	Checks                  []ProposalCheck    `json:"checks"`
	ConfirmationDigest      string             `json:"confirmation_digest"`
	ExpiresAt               time.Time          `json:"expires_at"`
	CreatedAt               time.Time          `json:"created_at"`
}

type PrepareResult struct {
	Proposal       *PreparedProposal `json:"proposal,omitempty"`
	ChoiceRequired []ProposalChoice  `json:"choice_required,omitempty"`
}

type SubmitPreparedInput struct {
	ProposalID         string `json:"proposal_id" jsonschema:"prepared experiment proposal ID"`
	ConfirmationDigest string `json:"confirmation_digest" jsonschema:"exact digest shown in the approved proposal"`
}

type SubmitPreparedResult struct {
	Experiment View `json:"experiment"`
	Idempotent bool `json:"idempotent"`
}
