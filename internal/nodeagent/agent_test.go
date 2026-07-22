package nodeagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
)

type fixedCollector struct{ report DoctorReport }

func (f fixedCollector) Collect(context.Context, string, string, string) (DoctorReport, error) {
	return f.report, nil
}

type activityCollector struct {
	fixedCollector
	external bool
	err      error
}

func (c activityCollector) ExternalGPUProcesses(context.Context) (bool, error) {
	return c.external, c.err
}

func TestAgentPersistsCommandBeforeAcknowledging(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	installationID := uuid.NewString()
	fingerprint := strings.Repeat("a", 64)
	var calls atomic.Int32
	commandID := uuid.NewString()
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input nodeprotocol.SyncRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		call := calls.Add(1)
		result := nodeprotocol.SyncResponse{NodeID: uuid.NewString(), DesiredState: "active", NextSyncSeconds: 1}
		if len(input.Events) > 0 {
			result.AckedEventSequence = input.Events[len(input.Events)-1].Sequence
		}
		if call == 1 {
			result.Commands = []nodeprotocol.Command{{ID: commandID, Sequence: 1, Kind: "reconcile"}}
		} else {
			if len(input.Acknowledgements) != 1 || input.Acknowledgements[0].CommandID != commandID || input.Acknowledgements[0].Status != "completed" {
				t.Errorf("second sync acknowledgements=%+v", input.Acknowledgements)
			}
			cancel()
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"data": result})
	}))
	defer server.Close()
	directory := t.TempDir()
	config := Config{
		ServerURL: server.URL, NodeID: uuid.NewString(), InstallationID: installationID, MachineFingerprint: fingerprint,
		CredentialPath: directory + "/credential", StatePath: directory + "/state.db", StorageRoot: directory + "/storage",
	}
	client, err := NewClient(server.URL, "test", WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(config.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	collector := fixedCollector{report: DoctorReport{Inventory: nodeprotocol.Inventory{
		InstallationID: installationID, MachineFingerprint: fingerprint, AgentVersion: "test", ProtocolVersion: nodeprotocol.Version,
		CPUCount: 1, MemoryBytes: 1, GPUs: []nodeprotocol.GPU{{UUID: "GPU-test", Name: "GPU", MemoryBytes: 1}},
		Storage: nodeprotocol.Storage{Root: config.StorageRoot, TotalBytes: 1, AvailableBytes: 1},
	}}}
	agent, err := NewAgent(config, "gmn_1234567_secret", client, store, collector, "test")
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- agent.Run(ctx) }()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not synchronize command completion")
	}
	if calls.Load() < 2 {
		t.Fatalf("sync calls=%d", calls.Load())
	}
}

func TestAgentReportsExternalGPUUseButNotItsManagedWorkload(t *testing.T) {
	directory := t.TempDir()
	config := Config{
		ServerURL: "https://gemcp.example.com", NodeID: uuid.NewString(), InstallationID: uuid.NewString(),
		MachineFingerprint: strings.Repeat("a", 64), CredentialPath: directory + "/credential",
		StatePath: directory + "/state.db", StorageRoot: directory + "/storage",
	}
	client, _ := NewClient(config.ServerURL, "test")
	store, err := OpenStore(config.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewWorkloadManager(config, "gmn_1234567_secret", client, store, &fakeContainerRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	collector := activityCollector{external: true}
	agent, err := NewAgent(config, "gmn_1234567_secret", client, store, collector, "test", WithWorkloadManager(manager))
	if err != nil {
		t.Fatal(err)
	}
	if state := agent.observedState(context.Background()); state != "externally_busy" {
		t.Fatalf("observed state=%q", state)
	}
	if err := store.SaveWorkload(WorkloadRecord{
		AssignmentID: uuid.NewString(), ExperimentID: uuid.NewString(), AttemptID: uuid.NewString(),
		ContainerID: "container-id", OutputRef: "experiments/test/outputs", State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	if state := agent.observedState(context.Background()); state != "online" {
		t.Fatalf("managed workload observed state=%q", state)
	}
}
