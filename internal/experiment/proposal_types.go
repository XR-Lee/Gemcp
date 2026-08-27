package experiment

import (
	"context"
	"errors"
	"time"

	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
)

const (
	ProposalCheckPass = "pass"
	ProposalCheckWarn = "warn"
	ProposalCheckFail = "fail"
)

var (
	ErrProposalNotFound     = errors.New("experiment proposal not found")
	ErrProposalExpired      = errors.New("experiment proposal expired")
	ErrProposalChanged      = errors.New("experiment proposal changed after confirmation")
	ErrProposalBlocked      = errors.New("experiment proposal preflight did not pass")
	ErrConfirmationRequired = errors.New("review and explicitly confirm the immutable experiment proposal")
)

type ProposalRefResolver interface {
	ResolveRef(context.Context, int, string) (string, error)
}

type ProposalArchiveReader interface {
	ArchiveCommit(context.Context, int, string, int64) (gitrepository.Archive, error)
}

type ProposalProviderReader interface {
	QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error)
}

type ProposalRuntimeReader interface {
	Status(context.Context) (execution.RuntimeStatus, error)
}

type ProposalConfig struct {
	SourceMaxBytes    int64
	SelfHostedEnabled bool
	SSHCloudEnabled   bool
	NodeStaleAfter    time.Duration
	Lifetime          time.Duration
}

type PrepareInput struct {
	Repository        string   `json:"repository,omitempty" jsonschema:"repository name or ID; omit when the Project has exactly one active repository"`
	RepositoryRemote  string   `json:"repository_remote,omitempty" jsonschema:"registered Git remote; omit when the Project has exactly one active repository"`
	Ref               string   `json:"ref,omitempty" jsonschema:"branch, tag, or full commit; omit to use the repository default branch"`
	Argv                 []string `json:"argv,omitempty" jsonschema:"ordered program argument vector; omit for the provision preset. Shell interpreters are rejected"`
	RuntimePreset        string   `json:"runtime_preset,omitempty" jsonschema:"runtime preset: smoke (300s), probe (3600s), train, or provision"`
	MaxRuntimeSeconds    int      `json:"max_runtime_seconds,omitempty" jsonschema:"requested runtime; must stay within the selected preset and Project max_runtime_seconds"`
	Environment          string   `json:"environment,omitempty" jsonschema:"approved environment name or ID; omit to resolve a compatible default"`
	ResourceProfile      string   `json:"resource_profile,omitempty" jsonschema:"active resource profile name or ID; omit to resolve a compatible default"`
	Image                string   `json:"image,omitempty" jsonschema:"omit for Cloud SSH. Trusted Self-hosted workspace may pass a public name, tag, or digest"`
	Cwd                  string   `json:"cwd,omitempty" jsonschema:"optional remote absolute working directory for Cloud SSH; defaults to the login home"`
	Dataset              string   `json:"dataset,omitempty" jsonschema:"optional dataset binding or catalog name to provision or inject"`
	InstallDependencies  bool     `json:"install_dependencies,omitempty" jsonschema:"install requirements.gemcp.txt or requirements.txt from the verified commit with python -m pip install --user"`
	RequirementsFile     string   `json:"requirements_file,omitempty" jsonschema:"optional relative requirements file; defaults to requirements.gemcp.txt"`
	FromNodeID           string   `json:"from_node_id,omitempty" jsonschema:"Graph hypothesis or plan node ID; required when the Project has an active Study"`
	ExpectedMetric       string   `json:"expected_metric,omitempty" jsonschema:"optional metric name the Owner should expect after close_run"`
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
	EnvironmentID       string                          `json:"environment_id"`
	EnvironmentName     string                          `json:"environment_name"`
	ResourceProfileID   string                          `json:"resource_profile_id"`
	ResourceProfileName string                          `json:"resource_profile_name"`
	Backend             string                          `json:"backend"`
	Image               string                          `json:"image"`
	GPUModels           []string                        `json:"gpu_models"`
	GPUNum              int                             `json:"gpu_num"`
	Region              string                          `json:"region"`
	CUDAFrom            int                             `json:"cuda_from"`
	CUDATo              int                             `json:"cuda_to"`
	CPUFrom             int                             `json:"cpu_from"`
	CPUTo               int                             `json:"cpu_to"`
	MemoryFromGB        int                             `json:"memory_from_gb"`
	MemoryToGB          int                             `json:"memory_to_gb"`
	PriceFromMilli      int64                           `json:"price_from_milli"`
	PriceToMilli        int64                           `json:"price_to_milli"`
	ReuseContainer      bool                            `json:"reuse_container"`
	Billable            bool                            `json:"billable"`
	ExecutionPolicy     string                          `json:"execution_policy,omitempty"`
	WorkspacePath       string                          `json:"workspace_path,omitempty"`
	NodeID              string                          `json:"node_id,omitempty"`
	NodeLabel           string                          `json:"node_label,omitempty"`
	Host                string                          `json:"host,omitempty"`
	User                string                          `json:"user,omitempty"`
	WorkingDirectory    string                          `json:"working_directory,omitempty"`
	Isolation           string                          `json:"isolation,omitempty"`
	ImageMutable        bool                            `json:"image_mutable,omitempty"`
	WorkspaceDatasets   []nodeprotocol.WorkspaceDataset `json:"workspace_datasets,omitempty"`
	DatasetBindings     []ProposalDatasetBinding        `json:"dataset_bindings,omitempty"`
}

type ProposalDatasetBinding struct {
	ID                  string                    `json:"id"`
	Name                string                    `json:"name"`
	Backend             string                    `json:"backend"`
	CanonicalRoot       string                    `json:"canonical_root"`
	EnvironmentVariable string                    `json:"environment_variable"`
	RequiredMarkers     []string                  `json:"required_markers,omitempty"`
	Sources             []datasetcatalog.SourceFile `json:"sources,omitempty"`
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
	FromNodeID              string             `json:"from_node_id,omitempty"`
	ExpectedMetric          string             `json:"expected_metric,omitempty"`
	InstallDependencies     bool               `json:"install_dependencies,omitempty"`
	RequirementsFile        string             `json:"requirements_file,omitempty"`
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

type OwnerSubmitPreparedInput struct {
	ConfirmationDigest string `json:"confirmation_digest"`
	Confirmed          bool   `json:"confirmed"`
}

type SubmitPreparedResult struct {
	Experiment View   `json:"experiment"`
	RunNodeID  string `json:"run_node_id,omitempty"`
	Idempotent bool   `json:"idempotent"`
}
