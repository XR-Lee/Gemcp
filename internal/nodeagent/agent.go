package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
)

type Agent struct {
	config    Config
	token     string
	client    *Client
	store     *Store
	collector InventoryCollector
	workloads *WorkloadManager
	version   string
	now       func() time.Time
}

type AgentOption func(*Agent)

func WithWorkloadManager(manager *WorkloadManager) AgentOption {
	return func(agent *Agent) { agent.workloads = manager }
}

func NewAgent(config Config, token string, client *Client, store *Store, collector InventoryCollector, version string, options ...AgentOption) (*Agent, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if token == "" || client == nil || store == nil || collector == nil {
		return nil, fmt.Errorf("node Agent dependencies are required")
	}
	agent := &Agent{config: config, token: token, client: client, store: store, collector: collector, version: version, now: time.Now}
	for _, option := range options {
		option(agent)
	}
	if agent.workloads == nil {
		manager, err := NewWorkloadManager(config, token, client, store, DockerRuntime{})
		if err != nil {
			return nil, err
		}
		agent.workloads = manager
	}
	return agent, nil
}

func (a *Agent) Run(ctx context.Context) error {
	report, err := a.collector.Collect(ctx, a.config.InstallationID, a.config.StorageRoot, a.version)
	if err != nil {
		return fmt.Errorf("collect node inventory: %w", err)
	}
	delay := time.Duration(0)
	for {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
		delay = 15 * time.Second
		if err := a.workloads.Reconcile(ctx); err != nil {
			slog.Error("node workload reconciliation failed", "error", err)
		}
		if err := a.processCommands(ctx); err != nil {
			slog.Error("node command processing failed", "error", err)
		}
		observedState := a.observedState(ctx)
		if _, err := a.store.AppendEvent("heartbeat", map[string]any{"observed_state": observedState}, a.now().UTC()); err != nil {
			return err
		}
		events, err := a.store.PendingEvents(100)
		if err != nil {
			return err
		}
		acknowledgements, err := a.store.PendingAcknowledgements()
		if err != nil {
			return err
		}
		lastCommandSequence, err := a.store.LastCommandSequence()
		if err != nil {
			return err
		}
		capabilities, err := normalizeMap(map[string]any{
			"cpu_count": report.Inventory.CPUCount, "memory_bytes": report.Inventory.MemoryBytes, "gpus": report.Inventory.GPUs,
			"execution_modes": []string{"shell", "argv"}, "workspace_modes": []string{"trusted_rw"},
			"dataset_modes": []string{"workspace_env_v1"},
		})
		if err != nil {
			return err
		}
		storage, err := normalizeMap(report.Inventory.Storage)
		if err != nil {
			return err
		}
		response, err := a.client.Sync(ctx, a.token, nodeprotocol.SyncRequest{
			InstallationID: a.config.InstallationID, MachineFingerprint: report.Inventory.MachineFingerprint,
			AgentVersion: a.version, ProtocolVersion: nodeprotocol.Version, ObservedState: observedState,
			Capabilities: capabilities, Storage: storage, LastCommandSequence: lastCommandSequence,
			Acknowledgements: acknowledgements, Events: events,
		})
		if err != nil {
			slog.Warn("node synchronization failed", "error", err)
			continue
		}
		if err := a.store.AckEvents(response.AckedEventSequence); err != nil {
			return err
		}
		acknowledgedIDs := make([]string, 0, len(acknowledgements))
		for _, acknowledgement := range acknowledgements {
			acknowledgedIDs = append(acknowledgedIDs, acknowledgement.CommandID)
		}
		if err := a.store.AckCommands(acknowledgedIDs); err != nil {
			return err
		}
		if err := a.store.SaveCommands(response.Commands); err != nil {
			return err
		}
		if len(response.Commands) > 0 {
			delay = time.Second
		} else if response.NextSyncSeconds > 0 && response.NextSyncSeconds <= 60 {
			delay = time.Duration(response.NextSyncSeconds) * time.Second
		}
		slog.Debug("node synchronized", "desired_state", response.DesiredState, "commands", len(response.Commands), "acked_event_sequence", response.AckedEventSequence)
	}
}

func (a *Agent) processCommands(ctx context.Context) error {
	commands, err := a.store.UnprocessedCommands()
	if err != nil {
		return err
	}
	for _, command := range commands {
		acknowledgement := nodeprotocol.CommandAcknowledgement{CommandID: command.ID}
		var commandErr error
		switch command.Kind {
		case "reconcile":
			acknowledgement.Status = "completed"
			acknowledgement.Result = map[string]any{"state": "ready"}
		case "start_workload":
			if a.externalGPUProcesses(ctx) {
				commandErr = fmt.Errorf("GPU is occupied by a non-Gemcp compute process")
			} else {
				commandCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
				acknowledgement.Result, commandErr = a.workloads.Start(commandCtx, command.Payload)
				cancel()
			}
		case "stop_workload":
			acknowledgement.Result, commandErr = a.workloads.Stop(ctx, command.Payload)
		default:
			acknowledgement.Status = "failed"
			acknowledgement.Error = "unsupported command kind"
		}
		if commandErr != nil {
			acknowledgement.Status = "failed"
			acknowledgement.Error = boundedCommandError(commandErr)
		} else if acknowledgement.Status == "" {
			acknowledgement.Status = "completed"
		}
		if err := a.store.CompleteCommand(command, acknowledgement); err != nil {
			return err
		}
	}
	return nil
}

func boundedCommandError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 4096 {
		value = value[len(value)-4096:]
	}
	return value
}

func (a *Agent) observedState(ctx context.Context) string {
	busy, err := a.workloads.Busy()
	if err != nil {
		slog.Warn("read managed workload state", "error", err)
		return "reconciling"
	}
	if busy {
		return "online"
	}
	collector, ok := a.collector.(GPUActivityCollector)
	if !ok {
		return "online"
	}
	external, err := collector.ExternalGPUProcesses(ctx)
	if err != nil {
		slog.Warn("inspect GPU activity", "error", err)
		return "incompatible"
	}
	if external {
		return "externally_busy"
	}
	return "online"
}

func (a *Agent) externalGPUProcesses(ctx context.Context) bool {
	busy, err := a.workloads.Busy()
	if err != nil {
		return true
	}
	if busy {
		return false
	}
	collector, ok := a.collector.(GPUActivityCollector)
	if !ok {
		return false
	}
	external, err := collector.ExternalGPUProcesses(ctx)
	return err != nil || external
}

func normalizeMap(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode node state: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("normalize node state: %w", err)
	}
	return result, nil
}
