package runner

import (
	"errors"
	"time"
)

const (
	TokenDigestDomain  = "runner-token"
	TokenAADPrefix     = "gemcp:runner-token:v1:"
	MaxLogTailBytes    = 64 << 10
	MaxMetricsBytes    = 64 << 10
	MaxSourceDownloads = 3
)

var (
	ErrUnauthenticated = errors.New("Runner authentication failed")
	ErrExpired         = errors.New("Runner token expired")
	ErrSourceLimit     = errors.New("Runner source download limit reached")
	ErrInvalidEvent    = errors.New("invalid Runner event")
	ErrTerminal        = errors.New("experiment is already terminal")
)

type Spec struct {
	ExperimentID             string    `json:"experiment_id"`
	AttemptID                string    `json:"attempt_id"`
	Command                  string    `json:"command"`
	OutputPath               string    `json:"output_path"`
	MaxRuntimeSeconds        int       `json:"max_runtime_seconds"`
	TimeoutExtensionSeconds  int       `json:"timeout_extension_seconds"`
	TerminationGraceSeconds  int       `json:"termination_grace_seconds"`
	HeartbeatIntervalSeconds int       `json:"heartbeat_interval_seconds"`
	SourceMaxBytes           int64     `json:"source_max_bytes"`
	TokenExpiresAt           time.Time `json:"token_expires_at"`
}

type EventInput struct {
	Type     string         `json:"type"`
	ExitCode *int           `json:"exit_code,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	LogTail  string         `json:"log_tail,omitempty"`
	Metrics  map[string]any `json:"metrics,omitempty"`
}

type Control struct {
	StopRequested bool       `json:"stop_requested"`
	StopReason    string     `json:"stop_reason,omitempty"`
	DeadlineAt    *time.Time `json:"deadline_at,omitempty"`
}
