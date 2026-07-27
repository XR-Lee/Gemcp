package nodeagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeContainerRuntime struct {
	spec      ContainerSpec
	state     ContainerState
	started   int
	stopped   int
	removed   int
	container string
}

func (f *fakeContainerRuntime) Start(_ context.Context, spec ContainerSpec) (string, error) {
	f.spec = spec
	f.started++
	f.container = "container-id"
	return f.container, nil
}

func (f *fakeContainerRuntime) Find(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (f *fakeContainerRuntime) Inspect(context.Context, string) (ContainerState, error) {
	return f.state, nil
}

func (f *fakeContainerRuntime) Stop(context.Context, string, int) error {
	f.stopped++
	f.state.Running = false
	return nil
}

func (f *fakeContainerRuntime) Logs(context.Context, string, int) (string, error) {
	return "training complete\n", nil
}

func (f *fakeContainerRuntime) SaveLogs(_ context.Context, _ string, filename string) error {
	return os.WriteFile(filename, []byte("training complete\n"), 0o600)
}

func (f *fakeContainerRuntime) Remove(context.Context, string) error {
	f.removed++
	return nil
}

func TestWorkloadManagerRunsWithoutPassingNodeCredentialToContainer(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"train.py": "print('ok')\n"})
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || !strings.HasSuffix(request.URL.Path, "/source") {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer gmn_1234567_secret" {
			t.Errorf("Authorization=%q", request.Header.Get("Authorization"))
		}
		response.Header().Set("Content-Length", "")
		_, _ = response.Write(archive)
	}))
	defer server.Close()
	directory := t.TempDir()
	config := Config{
		ServerURL: server.URL, NodeID: uuid.NewString(), InstallationID: uuid.NewString(), MachineFingerprint: strings.Repeat("a", 64),
		CredentialPath: directory + "/credential", StatePath: directory + "/state.db", StorageRoot: directory + "/storage",
	}
	client, _ := NewClient(server.URL, "test", WithHTTPClient(server.Client()))
	store, err := OpenStore(config.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := &fakeContainerRuntime{state: ContainerState{Running: true}}
	manager, err := NewWorkloadManager(config, "gmn_1234567_secret", client, store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	manager.now = func() time.Time { return now }
	assignmentID, experimentID, attemptID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	payload := map[string]any{
		"assignment_id": assignmentID, "experiment_id": experimentID, "attempt_id": attemptID,
		"image": "registry.example/train@sha256:" + strings.Repeat("b", 64), "command": "python train.py",
		"source_path": "/api/v1/node-assignments/" + assignmentID + "/source", "source_max_bytes": int64(1 << 20),
		"output_ref":          "experiments/" + experimentID + "/attempts/" + attemptID + "/outputs",
		"max_runtime_seconds": 300, "timeout_extension_seconds": 60, "termination_grace_seconds": 5,
		"gpu_uuid": "GPU-test", "cpu_limit": 8, "memory_limit_bytes": int64(32 << 30),
	}
	result, err := manager.Start(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if result["workload_id"] != "container-id" || runtime.started != 1 {
		t.Fatalf("result=%v runtime=%+v", result, runtime)
	}
	if runtime.spec.Command != "python train.py" || runtime.spec.GPUUUID != "GPU-test" || strings.Contains(strings.ToLower(runtime.spec.Command), "gmn_") {
		t.Fatalf("container spec=%+v", runtime.spec)
	}
	if _, err := os.Stat(filepath.Join(runtime.spec.SourcePath, "train.py")); err != nil {
		t.Fatalf("source was not extracted: %v", err)
	}
	events, _ := store.PendingEvents(10)
	if len(events) != 1 || events[0].Kind != "workload_started" {
		t.Fatalf("start events=%+v", events)
	}

	outputPath, _ := managedOutputPath(config.StorageRoot, payload["output_ref"].(string))
	if err := os.WriteFile(filepath.Join(outputPath, "metrics.json"), []byte(`{"accuracy":0.9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime.state = ContainerState{Running: false, ExitCode: 0}
	manager.now = func() time.Time { return now.Add(time.Minute) }
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ = store.PendingEvents(10)
	if len(events) != 2 || events[1].Kind != "workload_finished" || events[1].Payload["reason"] != "completed" {
		t.Fatalf("completion events=%+v", events)
	}
	if metrics, ok := events[1].Payload["metrics"].(map[string]any); !ok || metrics["accuracy"] != 0.9 {
		t.Fatalf("completion metrics=%v", events[1].Payload["metrics"])
	}
	if workloads, _ := store.Workloads(); len(workloads) != 0 || runtime.removed != 1 {
		t.Fatalf("workloads=%+v removed=%d", workloads, runtime.removed)
	}
	completeLog, err := os.ReadFile(filepath.Join(outputPath, "run.log"))
	if err != nil || string(completeLog) != "training complete\n" {
		t.Fatalf("complete log=%q err=%v", completeLog, err)
	}
}

func TestSourceExtractionRejectsTraversal(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"../escape": "no"})
	filename := filepath.Join(t.TempDir(), "source.tar.gz")
	if err := os.WriteFile(filename, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "source")
	if err := extractSourceArchive(filename, destination, 1<<20); err == nil {
		t.Fatal("unsafe source archive was accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(destination), "escape")); !os.IsNotExist(err) {
		t.Fatalf("archive escaped destination: %v", err)
	}
}

func sourceArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": strings.Repeat("a", 40)},
	}); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tarWriter, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

var _ ContainerRuntime = (*fakeContainerRuntime)(nil)
