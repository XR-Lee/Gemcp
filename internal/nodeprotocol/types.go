package nodeprotocol

import "time"

const Version = "1"

type GPU struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	MemoryBytes int64  `json:"memory_bytes"`
}

type Storage struct {
	Root           string `json:"root"`
	TotalBytes     int64  `json:"total_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
	ManagedBytes   int64  `json:"managed_bytes"`
}

type Inventory struct {
	InstallationID     string  `json:"installation_id"`
	MachineFingerprint string  `json:"machine_fingerprint"`
	Hostname           string  `json:"hostname"`
	OperatingSystem    string  `json:"operating_system"`
	Architecture       string  `json:"architecture"`
	AgentVersion       string  `json:"agent_version"`
	ProtocolVersion    string  `json:"protocol_version"`
	CPUCount           int     `json:"cpu_count"`
	MemoryBytes        int64   `json:"memory_bytes"`
	GPUs               []GPU   `json:"gpus"`
	Storage            Storage `json:"storage"`
}

type EnrollmentClaimRequest struct {
	Code      string    `json:"code"`
	Inventory Inventory `json:"inventory"`
}

type EnrollmentClaimResponse struct {
	EnrollmentID   string    `json:"enrollment_id"`
	NodeID         string    `json:"node_id"`
	NodeToken      string    `json:"node_token"`
	TokenPrefix    string    `json:"token_prefix"`
	PairingCode    string    `json:"pairing_code"`
	Status         string    `json:"status"`
	SyncURL        string    `json:"sync_url"`
	SetupExpiresAt time.Time `json:"setup_expires_at"`
}

type Event struct {
	ID         string         `json:"id"`
	Sequence   int64          `json:"sequence"`
	Kind       string         `json:"kind"`
	OccurredAt time.Time      `json:"occurred_at"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type CommandAcknowledgement struct {
	CommandID string         `json:"command_id"`
	Status    string         `json:"status"`
	Result    map[string]any `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type SyncRequest struct {
	InstallationID      string                   `json:"installation_id"`
	MachineFingerprint  string                   `json:"machine_fingerprint"`
	AgentVersion        string                   `json:"agent_version"`
	ProtocolVersion     string                   `json:"protocol_version"`
	ObservedState       string                   `json:"observed_state"`
	Capabilities        map[string]any           `json:"capabilities,omitempty"`
	Storage             map[string]any           `json:"storage,omitempty"`
	LastCommandSequence int64                    `json:"last_command_sequence"`
	Acknowledgements    []CommandAcknowledgement `json:"acknowledgements,omitempty"`
	Events              []Event                  `json:"events,omitempty"`
}

type Command struct {
	ID       string         `json:"id"`
	Sequence int64          `json:"sequence"`
	Kind     string         `json:"kind"`
	Payload  map[string]any `json:"payload,omitempty"`
}

type StartWorkload struct {
	AssignmentID            string   `json:"assignment_id"`
	ExperimentID            string   `json:"experiment_id"`
	AttemptID               string   `json:"attempt_id"`
	Image                   string   `json:"image"`
	ExecutionMode           string   `json:"execution_mode,omitempty"`
	Command                 string   `json:"command,omitempty"`
	Argv                    []string `json:"argv,omitempty"`
	SourcePath              string   `json:"source_path"`
	SourceMaxBytes          int64    `json:"source_max_bytes"`
	OutputRef               string   `json:"output_ref"`
	MaxRuntimeSeconds       int      `json:"max_runtime_seconds"`
	TimeoutExtensionSeconds int      `json:"timeout_extension_seconds"`
	TerminationGraceSeconds int      `json:"termination_grace_seconds"`
	GPUUUID                 string   `json:"gpu_uuid"`
	GPUName                 string   `json:"gpu_name"`
	CPULimit                int      `json:"cpu_limit"`
	MemoryLimitBytes        int64    `json:"memory_limit_bytes"`
}

type StopWorkload struct {
	AssignmentID            string `json:"assignment_id"`
	WorkloadID              string `json:"workload_id,omitempty"`
	Reason                  string `json:"reason"`
	TerminationGraceSeconds int    `json:"termination_grace_seconds"`
}

type SyncResponse struct {
	NodeID             string    `json:"node_id"`
	DesiredState       string    `json:"desired_state"`
	ServerTime         time.Time `json:"server_time"`
	AckedEventSequence int64     `json:"acked_event_sequence"`
	NextSyncSeconds    int       `json:"next_sync_seconds"`
	Commands           []Command `json:"commands"`
}
