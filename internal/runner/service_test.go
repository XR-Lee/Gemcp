package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/executionmeta"
	"github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

type memoryArchive struct {
	data   []byte
	closed bool
}

func (a *memoryArchive) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.data)), nil
}
func (a *memoryArchive) Close() error     { a.closed = true; return nil }
func (a *memoryArchive) SizeBytes() int64 { return int64(len(a.data)) }

type fakeArchiver struct {
	calls        int
	repositoryID int
	sha          string
	maxBytes     int64
	archive      *memoryArchive
	err          error
}

func (a *fakeArchiver) ArchiveCommit(_ context.Context, repositoryID int, sha string, maxBytes int64) (repository.Archive, error) {
	a.calls++
	if a.err != nil {
		return nil, a.err
	}
	a.repositoryID = repositoryID
	a.sha = sha
	a.maxBytes = maxBytes
	a.archive = &memoryArchive{data: []byte("archive")}
	return a.archive, nil
}

type runnerFixture struct {
	client     *ent.Client
	box        *secrets.Box
	service    *Service
	archiver   *fakeArchiver
	experiment *ent.Experiment
	attempt    *ent.Attempt
	resource   *ent.ProviderResource
	token      string
	now        time.Time
}

func newRunnerFixture(t *testing.T) *runnerFixture {
	return newRunnerFixtureWithArgv(t, nil)
}

func newRunnerFixtureWithArgv(t *testing.T, argv []string) *runnerFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	tenant, _ := client.Tenant.Create().SetName("tenant").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("project").SetSlug("project").
		SetMonthlyBudgetMilli(10000).SetMaxExperimentMilli(1000).Save(ctx)
	repositoryRecord, _ := client.Repository.Create().SetProjectID(project.ID).SetName("repository").
		SetSSHURL("git@github.com:owner/repository.git").SetSSHHost("github.com").SetDefaultBranch("main").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("environment").SetImageUUID("image").Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("profile").SetRegion("private").
		SetGpuNames([]string{"RTX 3090"}).SetCudaFrom(118).SetCudaTo(118).SetCPUFrom(1).SetCPUTo(2).
		SetMemoryFromGB(1).SetMemoryToGB(2).SetPriceFromMilli(1).SetPriceToMilli(1000).Save(ctx)
	agentToken, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("agent").SetPrefix("gmc_test").SetTokenHash([]byte("hash")).Save(ctx)
	providerAccount, _ := client.ProviderAccount.Create().SetTenantID(tenant.ID).SetName("provider").SetBaseURL("https://private.autodl.com").
		SetBackend("private").SetStatus("active").SetCredentialCiphertext("ciphertext").Save(ctx)
	experimentCreate := client.Experiment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(agentToken.ID).
		SetRepositoryID(repositoryRecord.ID).SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).
		SetState("provisioning").SetCommitSha("0123456789012345678901234567890123456789").SetCommand("python train.py").
		SetMaxRuntimeSeconds(60).SetTimeoutExtensionSeconds(30).SetTerminationGraceSeconds(5).
		SetRepositorySnapshot(map[string]any{}).SetEnvironmentSnapshot(map[string]any{}).SetResourceSnapshot(map[string]any{}).
		SetOutputPath("/root/autodl-fs/projects/p/experiments/e/").SetReservedCostMilli(1000)
	if len(argv) > 0 {
		experimentCreate.SetExecutionMode("argv").SetArgv(argv)
	}
	experimentRecord, _ := experimentCreate.Save(ctx)
	token, _, _ := secrets.RandomToken("gmr", 32)
	attemptPublic := experimentRecord.PublicID
	ciphertext, _ := box.Encrypt([]byte(token), TokenAADPrefix+attemptPublic.String())
	now := time.Now().UTC().Truncate(time.Second)
	attemptRecord, _ := client.Attempt.Create().SetPublicID(attemptPublic).SetTenantID(tenant.ID).SetProjectID(project.ID).
		SetExperimentID(experimentRecord.ID).SetNumber(1).SetRunnerTokenHash(box.Digest(TokenDigestDomain, token)).
		SetRunnerTokenCiphertext(ciphertext).SetRunnerTokenExpiresAt(now.Add(time.Hour)).Save(ctx)
	resourceRecord, _ := client.ProviderResource.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetExperimentID(experimentRecord.ID).
		SetAttemptID(attemptRecord.ID).SetProviderAccountID(providerAccount.ID).SetName("gemcp-attempt").SetProviderID("deployment-1").
		SetHardDeadlineAt(now.Add(10 * time.Minute)).Save(ctx)
	archiver := &fakeArchiver{}
	fixture := &runnerFixture{client: client, box: box, archiver: archiver, experiment: experimentRecord, attempt: attemptRecord, resource: resourceRecord, token: token, now: now}
	fixture.service = NewService(client, box, archiver, WithSourceMaxBytes(1024), WithClock(func() time.Time { return fixture.now }))
	return fixture
}

func TestSpecUsesExactlyOneExecutionAuthority(t *testing.T) {
	f := newRunnerFixtureWithArgv(t, []string{"python", "train.py", "--seed", "2"})
	spec, err := f.service.Spec(context.Background(), f.token)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ExecutionMode != "argv" || spec.Command != "" || strings.Join(spec.Argv, "|") != "python|train.py|--seed|2" {
		t.Fatalf("argv Runner spec = %+v", spec)
	}
}

func TestSpecAndSourceAreScopedToRunnerToken(t *testing.T) {
	f := newRunnerFixture(t)
	ctx := context.Background()
	spec, err := f.service.Spec(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ExecutionMode != "shell" || spec.Command != "python train.py" || len(spec.Argv) != 0 || spec.SourceMaxBytes != sourceTransferLimit(1024) || spec.ExperimentID != f.experiment.PublicID.String() ||
		spec.ProvisioningSecondsRemaining != 600 {
		t.Fatalf("spec = %+v", spec)
	}
	archive, err := f.service.Source(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := archive.Open()
	payload, _ := io.ReadAll(reader)
	_ = reader.Close()
	_ = archive.Close()
	if string(payload) != "archive" || f.archiver.repositoryID != f.experiment.RepositoryID || f.archiver.sha != f.experiment.CommitSha {
		t.Fatalf("archive payload=%q archiver=%+v", payload, f.archiver)
	}
	for index := 1; index < MaxSourceDownloads; index++ {
		archive, err := f.service.Source(ctx, f.token)
		if err != nil {
			t.Fatal(err)
		}
		_ = archive.Close()
	}
	if _, err := f.service.Source(ctx, f.token); err != ErrSourceLimit {
		t.Fatalf("fourth Source() error = %v", err)
	}
	if _, err := f.service.Spec(ctx, "wrong-token-that-is-still-long-enough-0000"); err != ErrUnauthenticated {
		t.Fatalf("wrong token error = %v", err)
	}
}

func TestSourceFailureReleasesDownloadReservation(t *testing.T) {
	f := newRunnerFixture(t)
	f.archiver.err = errors.New("temporary Git failure")
	if _, err := f.service.Source(context.Background(), f.token); err == nil {
		t.Fatal("Source() accepted archive failure")
	}
	record, err := f.client.Attempt.Get(context.Background(), f.attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.SourceDownloads != 0 {
		t.Fatalf("source downloads = %d, want 0", record.SourceDownloads)
	}
	f.archiver.err = nil
	if archive, err := f.service.Source(context.Background(), f.token); err != nil {
		t.Fatal(err)
	} else {
		_ = archive.Close()
	}
}

func TestDiagnosticEventsAreValidatedAndPersisted(t *testing.T) {
	f := newRunnerFixture(t)
	ctx := context.Background()
	if _, err := f.service.Event(ctx, f.token, EventInput{Type: "diagnostic", Stage: "runner_entered"}); err != nil {
		t.Fatal(err)
	}
	control, err := f.service.Event(ctx, f.token, EventInput{
		Type: "diagnostic", Stage: "bootstrap_failed_during_source_extract", ErrorType: "tarfile.ReadError",
	})
	if err != nil || !control.StopRequested || control.StopReason != "runner_bootstrap_failed" {
		t.Fatalf("failure control=%+v err=%v", control, err)
	}
	events, err := f.client.AuditEvent.Query().All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != "runner.bootstrap_stage" || events[0].Metadata["stage"] != "runner_entered" ||
		events[1].Metadata["stage"] != "bootstrap_failed_during_source_extract" || events[1].Metadata["error_type"] != "tarfile.ReadError" {
		t.Fatalf("diagnostic events = %+v", events)
	}
	attemptRecord, _ := f.client.Attempt.Get(ctx, f.attempt.ID)
	experimentRecord, _ := f.client.Experiment.Get(ctx, f.experiment.ID)
	resourceRecord, _ := f.client.ProviderResource.Get(ctx, f.resource.ID)
	if attemptRecord.State != "failed" || len(attemptRecord.RunnerTokenHash) != 0 || experimentRecord.State != "collecting" ||
		experimentRecord.FailureCode == nil || *experimentRecord.FailureCode != "runner_bootstrap_failed" ||
		resourceRecord.StopReason == nil || *resourceRecord.StopReason != "runner_bootstrap_failed" {
		t.Fatalf("attempt=%+v experiment=%+v resource=%+v", attemptRecord, experimentRecord, resourceRecord)
	}
	invalid := []EventInput{
		{Type: "diagnostic", Stage: "arbitrary_stage"},
		{Type: "diagnostic", Stage: "spec_loaded", ErrorType: "RuntimeError"},
		{Type: "diagnostic", Stage: "bootstrap_failed_before_spec"},
		{Type: "diagnostic", Stage: "bootstrap_failed_before_spec", ErrorType: "error with spaces"},
		{Type: "started", Stage: "spec_loaded"},
	}
	for _, input := range invalid {
		if _, err := f.service.Event(ctx, f.token, input); err != ErrInvalidEvent {
			t.Fatalf("Event(%+v) error = %v", input, err)
		}
	}
}

func TestRunnerEventsStartExtendAndStopAtDeadline(t *testing.T) {
	f := newRunnerFixture(t)
	ctx := context.Background()
	runtimeInfo := &executionmeta.RuntimeInfo{
		Source: "runner_observed", WorkingDirectory: "/tmp/gemcp-attempt/source", OutputDirectory: f.experiment.OutputPath,
		CUDAVisibleDevices: "0", GPUDevices: []executionmeta.GPUDevice{{Index: 0, UUID: "GPU-test", Name: "RTX 3090"}},
	}
	control, err := f.service.Event(ctx, f.token, EventInput{Type: "started", RuntimeInfo: runtimeInfo})
	if err != nil || control.StopRequested {
		t.Fatalf("started control=%+v err=%v", control, err)
	}
	if _, err := f.service.Event(ctx, f.token, EventInput{Type: "heartbeat", LogTail: "epoch 1\n", Metrics: map[string]any{"loss": 1.25}}); err != nil {
		t.Fatal(err)
	}
	experimentRecord, _ := f.client.Experiment.Get(ctx, f.experiment.ID)
	attemptRecord, _ := f.client.Attempt.Get(ctx, f.attempt.ID)
	if experimentRecord.State != "running" || experimentRecord.DeadlineAt == nil || experimentRecord.LogTail == nil || *experimentRecord.LogTail != "epoch 1\n" ||
		attemptRecord.Metrics["loss"] != 1.25 {
		t.Fatalf("started experiment = %+v", experimentRecord)
	}
	events, _ := f.client.AuditEvent.Query().All(ctx)
	if len(events) == 0 || events[len(events)-1].Metadata["runtime_info"] == nil {
		t.Fatalf("started audit events = %+v", events)
	}
	initialDeadline := *experimentRecord.DeadlineAt
	f.now = initialDeadline
	control, err = f.service.Event(ctx, f.token, EventInput{Type: "heartbeat"})
	if err != nil || control.StopRequested || control.DeadlineAt == nil || !control.DeadlineAt.Equal(initialDeadline.Add(30*time.Second)) {
		t.Fatalf("extension control=%+v err=%v", control, err)
	}
	f.now = initialDeadline.Add(31 * time.Second)
	control, err = f.service.Event(ctx, f.token, EventInput{Type: "heartbeat"})
	if err != nil || !control.StopRequested || control.StopReason != "timeout" {
		t.Fatalf("timeout control=%+v err=%v", control, err)
	}
	resourceRecord, _ := f.client.ProviderResource.Get(ctx, f.resource.ID)
	if resourceRecord.StopReason == nil || *resourceRecord.StopReason != "timeout" {
		t.Fatalf("resource = %+v", resourceRecord)
	}
}

func TestFinishedEventStoresBoundedResultAndRequestsCleanup(t *testing.T) {
	f := newRunnerFixture(t)
	ctx := context.Background()
	if _, err := f.service.Event(ctx, f.token, EventInput{Type: "started"}); err != nil {
		t.Fatal(err)
	}
	exitCode := 3
	control, err := f.service.Event(ctx, f.token, EventInput{
		Type: "finished", ExitCode: &exitCode, Reason: "completed", LogTail: "failed\n", Metrics: map[string]any{"loss": 1.5},
	})
	if err != nil || !control.StopRequested || control.StopReason != "completed" {
		t.Fatalf("finished control=%+v err=%v", control, err)
	}
	experimentRecord, _ := f.client.Experiment.Get(ctx, f.experiment.ID)
	attemptRecord, _ := f.client.Attempt.Get(ctx, f.attempt.ID)
	if experimentRecord.State != "collecting" || experimentRecord.ExitCode == nil || *experimentRecord.ExitCode != 3 || experimentRecord.LogTail == nil || *experimentRecord.LogTail != "failed\n" {
		t.Fatalf("experiment = %+v", experimentRecord)
	}
	if attemptRecord.State != "collecting" || attemptRecord.Metrics["loss"] != 1.5 {
		t.Fatalf("attempt = %+v", attemptRecord)
	}
	if _, err := f.service.Spec(ctx, f.token); err != ErrTerminal {
		t.Fatalf("finished Spec() error = %v", err)
	}
	oversized := strings.Repeat("x", MaxLogTailBytes+1)
	if _, err := f.service.Event(ctx, f.token, EventInput{Type: "finished", ExitCode: &exitCode, Reason: "completed", LogTail: oversized}); err != ErrInvalidEvent {
		t.Fatalf("oversized event error = %v", err)
	}
}

func TestExpiredRunnerTokenIsRejected(t *testing.T) {
	f := newRunnerFixture(t)
	f.now = f.now.Add(2 * time.Hour)
	if _, err := f.service.Spec(context.Background(), f.token); err != ErrExpired {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestBootstrapNormalizesOwnerStopBeforeCompletionCallback(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	outputPath := t.TempDir()
	finishedEvents := make(chan EventInput, 1)
	var sourceRequests atomic.Int32
	var startedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 40) || request.Header.Get("User-Agent") != runnerUserAgent {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/api/v1/runner/spec":
			_ = json.NewEncoder(response).Encode(map[string]any{"data": Spec{
				ExperimentID: "experiment", AttemptID: "attempt", Command: "true", OutputPath: outputPath,
				MaxRuntimeSeconds: 60, TimeoutExtensionSeconds: 30, TerminationGraceSeconds: 1,
				HeartbeatIntervalSeconds: 5, SourceMaxBytes: 1 << 20, ProvisioningSecondsRemaining: 60,
				TokenExpiresAt: time.Now().Add(time.Hour),
			}})
		case "/api/v1/runner/source":
			if sourceRequests.Add(1) == 1 {
				connection, buffer, hijackErr := response.(http.Hijacker).Hijack()
				if hijackErr != nil {
					t.Errorf("hijack source response: %v", hijackErr)
					return
				}
				_, _ = fmt.Fprintf(buffer, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: application/gzip\r\nConnection: close\r\n\r\n", archive.Len())
				_, _ = buffer.Write(archive.Bytes()[:archive.Len()/2])
				_ = buffer.Flush()
				_ = connection.Close()
				return
			}
			response.Header().Set("Content-Length", fmt.Sprint(archive.Len()))
			_, _ = response.Write(archive.Bytes())
		case "/api/v1/runner/events":
			var event EventInput
			if err := json.NewDecoder(request.Body).Decode(&event); err != nil {
				http.Error(response, "bad event", http.StatusBadRequest)
				return
			}
			control := Control{}
			if event.Type == "started" {
				if startedRequests.Add(1) < 3 {
					connection, _, hijackErr := response.(http.Hijacker).Hijack()
					if hijackErr != nil {
						t.Errorf("hijack started response: %v", hijackErr)
						return
					}
					_ = connection.Close()
					return
				}
				control = Control{StopRequested: true, StopReason: "owner_stop"}
			}
			if event.Type == "finished" {
				finishedEvents <- event
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"data": control})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "bootstrap.py")
	if err := os.WriteFile(path, []byte(BootstrapScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	launchLog := filepath.Join(t.TempDir(), "gemcp-launch.log")
	command := exec.Command(python, path)
	command.Env = append(os.Environ(), "GEMCP_RUNNER_URL="+server.URL, "GEMCP_RUNNER_TOKEN="+strings.Repeat("t", 40), "GEMCP_LAUNCH_LOG="+launchLog)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap exit succeeded, want 143: %s", output)
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 143 {
		t.Fatalf("bootstrap exit = %v: %s", err, output)
	}
	select {
	case finished := <-finishedEvents:
		if finished.Type != "finished" || finished.Reason != "cancelled" || finished.ExitCode == nil || *finished.ExitCode != 143 {
			t.Fatalf("finished callback = %+v", finished)
		}
	default:
		t.Fatal("bootstrap sent no completion callback")
	}
	if sourceRequests.Load() != 2 {
		t.Fatalf("source requests = %d, want 2", sourceRequests.Load())
	}
	if startedRequests.Load() != 3 {
		t.Fatalf("started requests = %d, want 3", startedRequests.Load())
	}
	if content, err := os.ReadFile(launchLog); err != nil || !strings.Contains(string(content), "gemcp-launch-runner-entered") ||
		!strings.Contains(string(content), "gemcp-launch-source-download-retry-") || !strings.Contains(string(content), "gemcp-launch-stage-source_extracted") ||
		!strings.Contains(string(content), "gemcp-launch-started-callback-retry-RemoteDisconnected") {
		t.Fatalf("Runner launch log = %q, %v", content, err)
	}
}

func TestBootstrapExecutesArgvWithoutShellInterpolation(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	script := []byte("import os,pathlib,sys\npathlib.Path(os.environ['GEMCP_OUTPUT_DIR'],'argv.txt').write_text(sys.argv[1])\n")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "argv_test.py", Mode: 0o644, Size: int64(len(script)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(script); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	outputPath := t.TempDir()
	shellMarker := filepath.Join(t.TempDir(), "shell-marker")
	literal := "value; $(touch " + shellMarker + ")"
	finishedEvents := make(chan EventInput, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/runner/spec":
			_ = json.NewEncoder(response).Encode(map[string]any{"data": Spec{
				ExperimentID: "experiment", AttemptID: "attempt", ExecutionMode: "argv",
				Argv: []string{python, "argv_test.py", literal}, OutputPath: outputPath,
				MaxRuntimeSeconds: 60, TerminationGraceSeconds: 1, HeartbeatIntervalSeconds: 5,
				SourceMaxBytes: 1 << 20, ProvisioningSecondsRemaining: 60, TokenExpiresAt: time.Now().Add(time.Hour),
			}})
		case "/api/v1/runner/source":
			response.Header().Set("Content-Length", fmt.Sprint(archive.Len()))
			_, _ = response.Write(archive.Bytes())
		case "/api/v1/runner/events":
			var event EventInput
			if err := json.NewDecoder(request.Body).Decode(&event); err != nil {
				http.Error(response, "bad event", http.StatusBadRequest)
				return
			}
			if event.Type == "finished" {
				finishedEvents <- event
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"data": Control{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "bootstrap.py")
	if err := os.WriteFile(path, []byte(BootstrapScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, path)
	command.Env = append(os.Environ(), "GEMCP_RUNNER_URL="+server.URL, "GEMCP_RUNNER_TOKEN="+strings.Repeat("t", 40))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("argv bootstrap failed: %v: %s", err, output)
	}
	content, err := os.ReadFile(filepath.Join(outputPath, "argv.txt"))
	if err != nil || string(content) != literal {
		t.Fatalf("argv output = %q, %v", content, err)
	}
	if _, err := os.Stat(shellMarker); !os.IsNotExist(err) {
		t.Fatalf("shell interpolation marker exists: %v", err)
	}
	select {
	case finished := <-finishedEvents:
		if finished.ExitCode == nil || *finished.ExitCode != 0 || finished.Reason != "completed" {
			t.Fatalf("finished callback = %+v", finished)
		}
	default:
		t.Fatal("argv bootstrap sent no completion callback")
	}
}

func TestBootstrapDoesNotRetrySourceHTTPFailures(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	var sourceRequests atomic.Int32
	outputPath := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/runner/spec":
			_ = json.NewEncoder(response).Encode(map[string]any{"data": Spec{
				ExperimentID: "experiment", AttemptID: "attempt", Command: "true", OutputPath: outputPath,
				MaxRuntimeSeconds: 60, TerminationGraceSeconds: 1, HeartbeatIntervalSeconds: 5,
				SourceMaxBytes: 1 << 20, TokenExpiresAt: time.Now().Add(time.Hour),
			}})
		case "/api/v1/runner/source":
			sourceRequests.Add(1)
			http.Error(response, "source unavailable", http.StatusServiceUnavailable)
		case "/api/v1/runner/events":
			_ = json.NewEncoder(response).Encode(map[string]any{"data": Control{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "bootstrap.py")
	if err := os.WriteFile(path, []byte(BootstrapScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	launchLog := filepath.Join(t.TempDir(), "gemcp-launch.log")
	command := exec.Command(python, path)
	command.Env = append(os.Environ(), "GEMCP_RUNNER_URL="+server.URL, "GEMCP_RUNNER_TOKEN="+strings.Repeat("t", 40), "GEMCP_LAUNCH_LOG="+launchLog)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap accepted source HTTP failure: %s", output)
	}
	if sourceRequests.Load() != 1 {
		t.Fatalf("source HTTP failure requests = %d, want 1", sourceRequests.Load())
	}
	content, err := os.ReadFile(launchLog)
	if err != nil || !strings.Contains(string(content), "gemcp-launch-stage-bootstrap_failed_during_source_download-HTTPError") ||
		strings.Contains(string(content), "gemcp-launch-source-download-retry-") {
		t.Fatalf("Runner launch log = %q, %v", content, err)
	}
}

func TestBootstrapDoesNotRetryStartedHTTPFailures(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	var startedRequests atomic.Int32
	outputPath := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/runner/spec":
			_ = json.NewEncoder(response).Encode(map[string]any{"data": Spec{
				ExperimentID: "experiment", AttemptID: "attempt", Command: "true", OutputPath: outputPath,
				MaxRuntimeSeconds: 60, TerminationGraceSeconds: 1, HeartbeatIntervalSeconds: 5,
				SourceMaxBytes: 1 << 20, ProvisioningSecondsRemaining: 60, TokenExpiresAt: time.Now().Add(time.Hour),
			}})
		case "/api/v1/runner/source":
			response.Header().Set("Content-Length", fmt.Sprint(archive.Len()))
			_, _ = response.Write(archive.Bytes())
		case "/api/v1/runner/events":
			var event EventInput
			if err := json.NewDecoder(request.Body).Decode(&event); err != nil {
				http.Error(response, "bad event", http.StatusBadRequest)
				return
			}
			if event.Type == "started" {
				startedRequests.Add(1)
				http.Error(response, "permanent HTTP failure", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"data": Control{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "bootstrap.py")
	if err := os.WriteFile(path, []byte(BootstrapScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	launchLog := filepath.Join(t.TempDir(), "gemcp-launch.log")
	command := exec.Command(python, path)
	command.Env = append(os.Environ(), "GEMCP_RUNNER_URL="+server.URL, "GEMCP_RUNNER_TOKEN="+strings.Repeat("t", 40), "GEMCP_LAUNCH_LOG="+launchLog)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap accepted started HTTP failure: %s", output)
	}
	if startedRequests.Load() != 1 {
		t.Fatalf("started HTTP failure requests = %d, want 1", startedRequests.Load())
	}
	content, err := os.ReadFile(launchLog)
	if err != nil || !strings.Contains(string(content), "gemcp-launch-stage-bootstrap_failed_during_started_callback-HTTPError") ||
		strings.Contains(string(content), "gemcp-launch-started-callback-retry-") {
		t.Fatalf("Runner launch log = %q, %v", content, err)
	}
}

func TestDownloaderUsesCompleteOpenerAndRejectsRedirects(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	token := strings.Repeat("t", 40)
	marker := filepath.Join(t.TempDir(), "downloaded")
	launchLog := filepath.Join(t.TempDir(), "gemcp-launch.log")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/runner/bootstrap" || request.Header.Get("Authorization") != "Bearer "+token || request.Header.Get("User-Agent") != runnerUserAgent {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprintf(response, "import pathlib;pathlib.Path(%q).write_text('ok')", marker)
	}))
	command := exec.Command(python, "-c", downloader)
	command.Env = append(os.Environ(), "GEMCP_RUNNER_URL="+server.URL, "GEMCP_RUNNER_TOKEN="+token, "GEMCP_LAUNCH_LOG="+launchLog)
	if output, err := command.CombinedOutput(); err != nil {
		server.Close()
		t.Fatalf("downloader failed: %v: %s", err, output)
	}
	server.Close()
	if content, err := os.ReadFile(marker); err != nil || string(content) != "ok" {
		t.Fatalf("downloaded bootstrap result = %q, %v", content, err)
	}
	if content, err := os.ReadFile(launchLog); err != nil || !strings.Contains(string(content), "gemcp-launch-bootstrap-download-started") || !strings.Contains(string(content), "gemcp-launch-bootstrap-download-complete") || strings.Contains(string(content), token) {
		t.Fatalf("bootstrap download launch log = %q, %v", content, err)
	}

	var transientAttempts atomic.Int32
	retryMarker := filepath.Join(t.TempDir(), "retried")
	retryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if transientAttempts.Add(1) < 3 {
			connection, _, hijackErr := response.(http.Hijacker).Hijack()
			if hijackErr != nil {
				t.Errorf("hijack transient response: %v", hijackErr)
				return
			}
			_ = connection.Close()
			return
		}
		_, _ = fmt.Fprintf(response, "import pathlib;pathlib.Path(%q).write_text('retried')", retryMarker)
	}))
	command = exec.Command(python, "-c", downloader)
	command.Env = append(os.Environ(), "GEMCP_RUNNER_URL="+retryServer.URL, "GEMCP_RUNNER_TOKEN="+token)
	if output, err := command.CombinedOutput(); err != nil {
		retryServer.Close()
		t.Fatalf("downloader retry failed: %v: %s", err, output)
	}
	retryServer.Close()
	if transientAttempts.Load() != 3 {
		t.Fatalf("transient bootstrap attempts = %d, want 3", transientAttempts.Load())
	}
	if content, err := os.ReadFile(retryMarker); err != nil || string(content) != "retried" {
		t.Fatalf("retried bootstrap result = %q, %v", content, err)
	}

	var redirected atomic.Bool
	var redirectAttempts atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirectAttempts.Add(1)
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	command = exec.Command(python, "-c", downloader)
	command.Env = append(os.Environ(), "GEMCP_RUNNER_URL="+redirect.URL, "GEMCP_RUNNER_TOKEN="+token)
	if err := command.Run(); err == nil {
		t.Fatal("downloader followed or accepted a redirect")
	}
	if redirected.Load() {
		t.Fatal("downloader leaked the request to a redirect target")
	}
	if redirectAttempts.Load() != 1 {
		t.Fatalf("redirect response was retried %d times", redirectAttempts.Load())
	}
}

func TestBootstrapScriptParsesAndLaunchCommandRequiresHTTPSOrigin(t *testing.T) {
	if _, err := LaunchCommand("http://gemcp.example.com", strings.Repeat("x", 40)); err == nil {
		t.Fatal("LaunchCommand accepted HTTP")
	}
	token := strings.Repeat("x", 40)
	outputPath := "/root/autodl-fs/projects/11111111-1111-4111-8111-111111111111/experiments/22222222-2222-4222-8222-222222222222/"
	command, err := LaunchCommandForOutput("https://gemcp.example.com", token, outputPath)
	if err != nil || !strings.HasPrefix(command, "set -eu; ") || !strings.Contains(command, "/root/miniconda3/bin/python3") || !strings.Contains(command, "sleep 128") || strings.ContainsAny(command, `$'"`) || strings.Contains(command, token) || strings.Contains(command, "python train.py") {
		t.Fatalf("command=%q err=%v", command, err)
	}
	if _, err := LaunchCommandForOutput("https://gemcp.example.com", token, "/tmp/output"); err == nil {
		t.Fatal("LaunchCommandForOutput accepted an unmanaged output path")
	}
	if len(command) > maxProviderLaunchCommandBytes {
		t.Fatalf("Provider launch command is %d bytes, want at most %d", len(command), maxProviderLaunchCommandBytes)
	}
	if _, err := LaunchCommandForOutput("https://gemcp.example.com", strings.Repeat("x", 4096), outputPath); err == nil {
		t.Fatal("LaunchCommandForOutput accepted a command above the Provider byte limit")
	}
	if output, err := exec.Command("/bin/bash", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("Provider launch command syntax: %v: %s", err, output)
	}
	const payloadPrefix = "printf %s "
	payloadStart := strings.Index(command, payloadPrefix)
	if payloadStart < 0 {
		t.Fatalf("Provider launch command has no encoded payload: %q", command)
	}
	payloadStart += len(payloadPrefix)
	payloadEnd := strings.Index(command[payloadStart:], " | /usr/bin/base64 -d")
	if payloadEnd < 0 {
		t.Fatalf("Provider launch command has no payload terminator: %q", command)
	}
	payloadEnd += payloadStart
	program, err := base64.StdEncoding.DecodeString(command[payloadStart:payloadEnd])
	if err != nil || !strings.Contains(string(program), "https://gemcp.example.com") || !strings.Contains(string(program), token) || !strings.Contains(string(program), outputPath+"gemcp-launch.log") || !strings.Contains(string(program), downloader) || !strings.Contains(string(program), runnerUserAgent) {
		t.Fatalf("encoded program is invalid: err=%v program=%q", err, program)
	}
	if !strings.Contains(BootstrapScript(), "normalized_stop_reason") || !strings.Contains(BootstrapScript(), `value[-maximum:].decode("utf-8", errors="ignore")`) || !strings.Contains(BootstrapScript(), `"User-Agent": USER_AGENT`) || !strings.Contains(BootstrapScript(), runnerUserAgent) {
		t.Fatal("bootstrap is missing bounded completion normalization")
	}
	if len(BootstrapScript()) > 128<<10 {
		t.Fatalf("Bootstrap script is %d bytes, want at most 128 KiB", len(BootstrapScript()))
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	path := filepath.Join(t.TempDir(), "bootstrap.py")
	if err := os.WriteFile(path, []byte(BootstrapScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(python, "-m", "py_compile", path).CombinedOutput(); err != nil {
		t.Fatalf("bootstrap syntax: %v: %s", err, output)
	}
}
