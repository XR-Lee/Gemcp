package nodeaccess

import (
	"errors"
	"time"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/validation"
)

const (
	defaultSetupMinutes  = 30
	minSetupMinutes      = 5
	maxSetupMinutes      = 24 * 60
	maxActiveEnrollments = 20
	maxListedNodes       = 500
	maxEventsPerSync     = 100
	maxAcksPerSync       = 100
	maxCommandsPerSync   = 20
)

var (
	ErrDisabled          = errors.New("Self-hosted nodes are disabled")
	ErrPublicURL         = errors.New("public URL is unavailable")
	ErrNotFound          = errors.New("node or enrollment not found")
	ErrEnrollmentInvalid = errors.New("node enrollment is invalid")
	ErrEnrollmentState   = errors.New("node enrollment is in the wrong state")
	ErrEnrollmentLimit   = errors.New("active node enrollment limit reached")
	ErrInvalidToken      = errors.New("invalid Node token")
	ErrProtocol          = errors.New("unsupported node protocol")
	ErrConflict          = errors.New("node synchronization conflict")
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type EnrollmentIssueInput struct {
	Label                 string `json:"label"`
	SetupExpiresInMinutes *int   `json:"setup_expires_in_minutes"`
}

type EnrollmentView struct {
	ID                 string         `json:"id"`
	Label              string         `json:"label"`
	Status             string         `json:"status"`
	ExpiresAt          time.Time      `json:"expires_at"`
	PairingCode        string         `json:"pairing_code,omitempty"`
	InstallationID     string         `json:"installation_id,omitempty"`
	MachineFingerprint string         `json:"machine_fingerprint,omitempty"`
	Report             map[string]any `json:"report,omitempty"`
	NodeID             string         `json:"node_id,omitempty"`
	ClaimedAt          *time.Time     `json:"claimed_at,omitempty"`
	ApprovedAt         *time.Time     `json:"approved_at,omitempty"`
	CompletedAt        *time.Time     `json:"completed_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type EnrollmentIssueResult struct {
	Enrollment EnrollmentView `json:"enrollment"`
	SetupURL   string         `json:"setup_url"`
	ClaimURL   string         `json:"claim_url"`
}

type ApprovalInput struct {
	PairingCode string   `json:"pairing_code"`
	ProjectIDs  []string `json:"project_ids"`
}

type NodeView struct {
	ID                 string         `json:"id"`
	Label              string         `json:"label"`
	TokenPrefix        string         `json:"token_prefix"`
	Status             string         `json:"status"`
	ObservedState      string         `json:"observed_state"`
	InstallationID     string         `json:"installation_id"`
	MachineFingerprint string         `json:"machine_fingerprint"`
	Hostname           string         `json:"hostname"`
	OperatingSystem    string         `json:"operating_system"`
	Architecture       string         `json:"architecture"`
	AgentVersion       string         `json:"agent_version"`
	ProtocolVersion    string         `json:"protocol_version"`
	Capabilities       map[string]any `json:"capabilities"`
	Storage            map[string]any `json:"storage"`
	ProjectIDs         []string       `json:"project_ids"`
	LastSeenAt         *time.Time     `json:"last_seen_at,omitempty"`
	ApprovedAt         *time.Time     `json:"approved_at,omitempty"`
	RevokedAt          *time.Time     `json:"revoked_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type ListResult struct {
	Nodes       []NodeView       `json:"nodes"`
	Enrollments []EnrollmentView `json:"enrollments"`
	Assignments []AssignmentView `json:"assignments"`
	Truncated   bool             `json:"truncated,omitempty"`
}

type AssignmentView struct {
	ID            string     `json:"id"`
	NodeID        string     `json:"node_id"`
	NodeLabel     string     `json:"node_label"`
	ProjectID     string     `json:"project_id"`
	ExperimentID  string     `json:"experiment_id"`
	AttemptID     string     `json:"attempt_id"`
	AttemptNumber int        `json:"attempt_number"`
	State         string     `json:"state"`
	OutputRef     string     `json:"output_ref"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	LastHeartbeat *time.Time `json:"last_heartbeat_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	ExitCode      *int       `json:"exit_code,omitempty"`
	FailureCode   *string    `json:"failure_code,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Principal struct {
	TenantID       int
	TenantPublicID string
	NodeID         int
	NodePublicID   string
	NodeLabel      string
	Status         string
}

type SyncResult = nodeprotocol.SyncResponse
