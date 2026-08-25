package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/serviceheartbeat"
	"github.com/XR-Lee/Gemcp/internal/runner"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/selfhosted"
	"github.com/XR-Lee/Gemcp/internal/servicehealth"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
)

var activeExperimentStates = []string{"provisioning", "running", "cancelling", "collecting"}

type LifecycleProvider interface {
	Create(context.Context, *ent.ProviderAccount, DeploymentSpec) (string, map[string]string, error)
	Observe(context.Context, *ent.ProviderAccount, string) (Observation, error)
	FindByName(context.Context, *ent.ProviderAccount, string) (Observation, error)
	Stop(context.Context, *ent.ProviderAccount, string) (map[string]string, error)
	Delete(context.Context, *ent.ProviderAccount, string) (map[string]string, error)
}

type Config struct {
	Enabled             bool
	PublicURL           string
	InstanceID          string
	GlobalConcurrency   int
	PollInterval        time.Duration
	LeaseDuration       time.Duration
	ProvisionTimeout    time.Duration
	ReconcileDelay      time.Duration
	CallbackGrace       time.Duration
	MaxAttempts         int
	RunnerTokenExtraTTL time.Duration
}

func DefaultConfig() Config {
	return Config{
		GlobalConcurrency: 1, PollInterval: 5 * time.Second, LeaseDuration: 2 * time.Minute,
		ProvisionTimeout: 10 * time.Minute, ReconcileDelay: 2 * time.Minute,
		CallbackGrace: 45 * time.Second, MaxAttempts: 3, RunnerTokenExtraTTL: 24 * time.Hour,
	}
}

type Engine struct {
	client     *ent.Client
	box        *secrets.Box
	provider   LifecycleProvider
	selfHosted *selfhosted.Service
	sshCloud   *sshcloud.Service
	config     Config
	now        func() time.Time
}

type EngineOption func(*Engine)

func WithSelfHostedService(service *selfhosted.Service) EngineOption {
	return func(engine *Engine) { engine.selfHosted = service }
}

func WithSSHCloudService(service *sshcloud.Service) EngineOption {
	return func(engine *Engine) { engine.sshCloud = service }
}

func NewEngine(client *ent.Client, box *secrets.Box, provider LifecycleProvider, config Config, options ...EngineOption) (*Engine, error) {
	if client == nil || box == nil || provider == nil {
		return nil, fmt.Errorf("execution engine dependencies are required")
	}
	if config.GlobalConcurrency <= 0 || config.GlobalConcurrency > 100 {
		return nil, fmt.Errorf("global concurrency must be between 1 and 100")
	}
	if config.PollInterval <= 0 || config.LeaseDuration < 30*time.Second || config.ProvisionTimeout <= 0 || config.ProvisionTimeout > 24*time.Hour || config.ReconcileDelay <= 0 || config.CallbackGrace <= 0 {
		return nil, fmt.Errorf("execution timing configuration is invalid")
	}
	if config.MaxAttempts <= 0 || config.MaxAttempts > 10 {
		return nil, fmt.Errorf("maximum attempts must be between 1 and 10")
	}
	if config.RunnerTokenExtraTTL < 0 || config.RunnerTokenExtraTTL > 24*time.Hour {
		return nil, fmt.Errorf("Runner token extra TTL must be between zero and 24 hours")
	}
	if strings.TrimSpace(config.InstanceID) == "" {
		return nil, fmt.Errorf("execution instance ID is required")
	}
	if config.Enabled && httpsPublicOrigin(config.PublicURL) {
		if _, err := runner.LaunchCommand(config.PublicURL, strings.Repeat("x", 40)); err != nil {
			return nil, err
		}
	}
	engine := &Engine{client: client, box: box, provider: provider, config: config, now: time.Now}
	for _, option := range options {
		option(engine)
	}
	return engine, nil
}

func (e *Engine) Run(ctx context.Context) error {
	if err := e.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("execution scheduler tick failed", "error", err)
	}
	ticker := time.NewTicker(e.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("execution scheduler tick failed", "error", err)
			}
		}
	}
}

func (e *Engine) Tick(ctx context.Context) error {
	now := e.now().UTC()
	heartbeatErr := servicehealth.Beat(ctx, e.client, serviceheartbeat.RoleScheduler, e.config.InstanceID, map[string]any{
		"dispatch_enabled": e.config.Enabled, "global_concurrency": e.config.GlobalConcurrency, "poll_interval_seconds": e.config.PollInterval.Seconds(),
	})
	var dispatched []int
	var dispatchErr error
	if e.config.Enabled {
		dispatched, dispatchErr = e.dispatchAvailable(ctx, now)
	}
	claimed, claimErr := e.claimRecoverable(ctx, now, e.config.GlobalConcurrency*4)
	ids := make([]int, 0, len(dispatched)+len(claimed))
	seen := map[int]struct{}{}
	for _, id := range append(dispatched, claimed...) {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	var stepErrors []error
	for _, id := range ids {
		if err := e.step(ctx, id); err != nil {
			stepErrors = append(stepErrors, fmt.Errorf("experiment %d: %w", id, err))
		}
		e.releaseExperimentLease(id)
	}
	return errors.Join(append([]error{heartbeatErr, dispatchErr, claimErr}, stepErrors...)...)
}

func (e *Engine) claimRecoverable(ctx context.Context, now time.Time, limit int) ([]int, error) {
	records, err := e.client.Experiment.Query().Where(
		entexperiment.StateIn(activeExperimentStates...),
		entexperiment.Or(entexperiment.LeaseExpiresAtIsNil(), entexperiment.LeaseExpiresAtLT(now)),
	).Order(ent.Asc(entexperiment.FieldUpdatedAt), ent.Asc(entexperiment.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	claimed := make([]int, 0, len(records))
	for _, record := range records {
		updated, err := e.client.Experiment.Update().Where(
			entexperiment.IDEQ(record.ID), entexperiment.StateIn(activeExperimentStates...),
			entexperiment.Or(entexperiment.LeaseExpiresAtIsNil(), entexperiment.LeaseExpiresAtLT(now)),
		).SetLeaseOwner(e.config.InstanceID).SetLeaseExpiresAt(now.Add(e.config.LeaseDuration)).Save(ctx)
		if err != nil {
			return claimed, err
		}
		if updated == 1 {
			claimed = append(claimed, record.ID)
		}
	}
	return claimed, nil
}

func (e *Engine) releaseExperimentLease(experimentID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = e.client.Experiment.Update().Where(
		entexperiment.IDEQ(experimentID), entexperiment.LeaseOwnerEQ(e.config.InstanceID),
	).ClearLeaseOwner().ClearLeaseExpiresAt().Save(ctx)
}

func httpsPublicOrigin(raw string) bool {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return false
	}
	return true
}
