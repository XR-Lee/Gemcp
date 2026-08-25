package sshcloud

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/validation"
)

const (
	Backend              = "ssh_cloud"
	CredentialAAD        = "gemcp:ssh-cloud:v1"
	recipePrefix         = "ssh-cloud:"
	maxLogTailBytes      = 64 << 10
	maxMetricsBytes      = 64 << 10
	maxCommandOutput     = 64 << 10
	defaultSSHPort       = 22
	defaultCPULimit      = 8
	defaultMemoryGB      = 32
	DefaultImage         = "host"
	HostImage            = "host"
	HostRef              = "host"
	HostCommit           = "host"
	maxActiveNodes       = 20
	autoProbeQuietPeriod = 90 * time.Second
	ExperimentalNote     = "Cloud SSH is experimental. Gemcp stores host login credentials, starts the Agent's command over SSH, and observes logs and exit status. It does not require Docker or a pinned image."
	WarningOwner         = "Experimental observer: Gemcp stores an encrypted SSH password or private key and opens outbound SSH. Probe only checks connectivity and pins the host key. Experiments run as a host process in the login environment. Emergency Stop only kills the Gemcp-started process group. It does not delete host files or power off the instance. Cloud-vendor charges are outside Gemcp."
	WarningAgent         = "Cloud SSH is experimental. The control plane holds host login credentials and observes the process it starts. Emergency Stop only kills that process group."
)

var (
	ErrDisabled       = errors.New("Cloud SSH execution is disabled")
	ErrNotFound       = errors.New("Cloud SSH node not found")
	ErrProject        = errors.New("Cloud SSH Project not found")
	ErrHostKeyChanged = errors.New("Cloud SSH host key fingerprint changed")
	ErrBusy           = errors.New("Cloud SSH node has an active Assignment")
	ErrNodeLimit      = errors.New("Cloud SSH node limit reached")
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type Config struct {
	Enabled          bool
	InstanceID       string
	ProvisionTimeout time.Duration
	MaxAttempts      int
}

func DefaultConfig() Config {
	return Config{
		ProvisionTimeout: 10 * time.Minute,
		MaxAttempts:      3,
	}
}

type Conn interface {
	Run(context.Context, string, int) (string, error)
	Upload(context.Context, string, io.Reader) error
	Close() error
}

type DialFunc func(context.Context, Target, Credential, string) (Conn, string, error)

type Target struct {
	Host string
	Port int
	User string
}

type Credential struct {
	Method     string `json:"method"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type GPU struct {
	Name        string `json:"name"`
	UUID        string `json:"uuid"`
	MemoryBytes int64  `json:"memory_bytes"`
}

type Inventory struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	DockerVersion string `json:"docker_version"`
	NvidiaReady   bool   `json:"nvidia_ready"`
	GPUs          []GPU  `json:"gpus"`
	CPUCount      int    `json:"cpu_count,omitempty"`
	MemoryBytes   int64  `json:"memory_bytes,omitempty"`
	HostKind      string `json:"host_kind,omitempty"`
	DockerNetwork string `json:"docker_network,omitempty"`
	StorageDriver string `json:"storage_driver,omitempty"`
}

type CreateInput struct {
	Label      string `json:"label,omitempty" jsonschema:"optional node label"`
	SSH        string `json:"ssh,omitempty" jsonschema:"ssh command line such as ssh -p 22 user@host; a following Password line is also read"`
	Host       string `json:"host,omitempty" jsonschema:"SSH hostname or IP when ssh is omitted"`
	Port       int    `json:"port,omitempty" jsonschema:"SSH port; defaults to 22"`
	User       string `json:"user,omitempty" jsonschema:"SSH user when ssh is omitted"`
	AuthMethod string `json:"auth_method,omitempty" jsonschema:"password or private_key"`
	Password   string `json:"password,omitempty" jsonschema:"SSH password; write-only and never returned"`
	PrivateKey string `json:"private_key,omitempty" jsonschema:"SSH private key; write-only and never returned"`
	Passphrase string `json:"passphrase,omitempty" jsonschema:"optional private-key passphrase; write-only"`
	Probe      *bool  `json:"probe,omitempty" jsonschema:"ignored for Agent registration; probe always runs in the background"`
	ProjectID  string `json:"project_id,omitempty" jsonschema:"ignored for Agent registration; the Token Project is bound automatically"`
}

type RotateInput struct {
	AuthMethod string `json:"auth_method"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	Probe      *bool  `json:"probe,omitempty"`
}

type AuthorizeInput struct {
	ProjectIDs  []string `json:"project_ids"`
	Image       string   `json:"image"`
	MakeDefault bool     `json:"make_default"`
}

type NodeView struct {
	ID                 string               `json:"id"`
	Label              string               `json:"label"`
	Status             string               `json:"status"`
	Experimental       bool                 `json:"experimental"`
	Warning            string               `json:"warning"`
	Host               string               `json:"host"`
	Port               int                  `json:"port"`
	User               string               `json:"user"`
	AuthMethod         string               `json:"auth_method"`
	HostKeyFingerprint string               `json:"host_key_fingerprint,omitempty"`
	Inventory          map[string]any       `json:"inventory,omitempty"`
	ProbeLog           []ProbeStep          `json:"probe_log,omitempty"`
	ProjectIDs         []string             `json:"project_ids"`
	ProjectRuntimes    []ProjectRuntimeView `json:"project_runtimes"`
	CreatedActorType   string               `json:"created_actor_type,omitempty"`
	CreatedActorID     string               `json:"created_actor_id,omitempty"`
	LastProbedAt       *time.Time           `json:"last_probed_at,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

type ProjectRuntimeView struct {
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	EnvironmentID   string `json:"environment_id"`
	EnvironmentName string `json:"environment_name"`
	Image           string `json:"image"`
	IsDefault       bool   `json:"is_default"`
}

type ListResult struct {
	Experimental bool             `json:"experimental"`
	Warning      string           `json:"warning"`
	Enabled      bool             `json:"enabled"`
	Nodes        []NodeView       `json:"nodes"`
	Assignments  []AssignmentView `json:"assignments"`
}

type AssignmentView struct {
	ID            string     `json:"id"`
	ExperimentID  string     `json:"experiment_id"`
	NodeID        string     `json:"node_id"`
	NodeLabel     string     `json:"node_label"`
	State         string     `json:"state"`
	AttemptNumber int        `json:"attempt_number"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	LastHeartbeat *time.Time `json:"last_heartbeat_at,omitempty"`
	ExitCode      *int       `json:"exit_code,omitempty"`
	FailureCode   *string    `json:"failure_code,omitempty"`
}

type Service struct {
	client         *ent.Client
	box            *secrets.Box
	config         Config
	dial           DialFunc
	now            func() time.Time
	skipAsyncProbe bool
}

func NewService(client *ent.Client, box *secrets.Box, config Config) (*Service, error) {
	if client == nil || box == nil {
		return nil, errors.New("Cloud SSH service dependencies are required")
	}
	if config.ProvisionTimeout <= 0 || config.MaxAttempts <= 0 || config.MaxAttempts > 10 {
		return nil, errors.New("Cloud SSH service configuration is invalid")
	}
	return &Service{client: client, box: box, config: config, dial: Dial, now: time.Now}, nil
}

func (s *Service) Enabled() bool { return s != nil && s.config.Enabled }

func (s *Service) WithDial(dial DialFunc) *Service {
	if s != nil && dial != nil {
		s.dial = dial
	}
	return s
}
