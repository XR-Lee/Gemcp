package selfhosted

import (
	"context"
	"errors"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/repository"
)

const Backend = "self_hosted"

var (
	ErrDisabled       = errors.New("Self-hosted execution is disabled")
	ErrAssignment     = errors.New("Self-hosted assignment not found")
	ErrAssignmentGone = errors.New("Self-hosted assignment is no longer active")
	ErrSourceLimit    = errors.New("Self-hosted source download limit reached")
	ErrInvalidEvent   = errors.New("invalid Self-hosted workload event")
	ErrProject        = errors.New("Self-hosted Project not found")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

type Config struct {
	Enabled          bool
	InstanceID       string
	ProvisionTimeout time.Duration
	NodeStaleAfter   time.Duration
	NodeLostAfter    time.Duration
	MaxAttempts      int
	SourceMaxBytes   int64
}

func DefaultConfig() Config {
	return Config{
		ProvisionTimeout: 10 * time.Minute,
		NodeStaleAfter:   time.Minute,
		NodeLostAfter:    30 * time.Minute,
		MaxAttempts:      3,
		SourceMaxBytes:   256 << 20,
	}
}

type SourceArchiver interface {
	ArchiveCommit(context.Context, int, string, int64) (repository.Archive, error)
}

type Service struct {
	client   *ent.Client
	archiver SourceArchiver
	config   Config
	now      func() time.Time
}

func NewService(client *ent.Client, archiver SourceArchiver, config Config) (*Service, error) {
	if client == nil {
		return nil, errors.New("Self-hosted service database is required")
	}
	if config.ProvisionTimeout <= 0 || config.NodeStaleAfter <= 0 || config.NodeLostAfter <= config.NodeStaleAfter {
		return nil, errors.New("Self-hosted service timing configuration is invalid")
	}
	if config.MaxAttempts <= 0 || config.MaxAttempts > 10 || config.SourceMaxBytes <= 0 {
		return nil, errors.New("Self-hosted service limits are invalid")
	}
	return &Service{client: client, archiver: archiver, config: config, now: time.Now}, nil
}

type EventProjector interface {
	ProjectNodeEvent(context.Context, *ent.Tx, *ent.SelfHostedNode, nodeprotocol.Event, time.Time) error
	ProjectCommandAcknowledgement(context.Context, *ent.Tx, *ent.SelfHostedNode, *ent.NodeCommand, nodeprotocol.CommandAcknowledgement, time.Time) error
}
