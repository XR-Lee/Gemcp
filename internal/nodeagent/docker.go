package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
)

var ErrContainerNotFound = errors.New("managed workload container not found")

type ContainerSpec struct {
	AssignmentID      string
	Image             string
	ExecutionMode     string
	Command           string
	Argv              []string
	GPUUUID           string
	CPULimit          int
	MemoryLimitBytes  int64
	SourcePath        string
	OutputPath        string
	WorkspaceMode     string
	WorkspacePath     string
	WorkspaceDatasets []nodeprotocol.WorkspaceDataset
}

type ContainerState struct {
	Running    bool
	ExitCode   int
	OOMKilled  bool
	StartedAt  time.Time
	FinishedAt time.Time
}

type ContainerRuntime interface {
	Start(context.Context, ContainerSpec) (string, error)
	Find(context.Context, string) (string, bool, error)
	Inspect(context.Context, string) (ContainerState, error)
	Stop(context.Context, string, int) error
	Logs(context.Context, string, int) (string, error)
	SaveLogs(context.Context, string, string) error
	Remove(context.Context, string) error
	Image(context.Context, string) (string, error)
}

type DockerRuntime struct{ Binary string }

func (d DockerRuntime) Find(ctx context.Context, assignmentID string) (string, bool, error) {
	output, err := commandOutput(ctx, 32<<10, d.binary(), "ps", "-a", "--filter", "label=io.gemcp.managed=true", "--filter", "label=io.gemcp.assignment="+assignmentID, "--format", "{{.ID}}")
	if err != nil {
		return "", false, err
	}
	lines := strings.Fields(output)
	if len(lines) == 0 {
		return "", false, nil
	}
	if len(lines) != 1 {
		return "", false, fmt.Errorf("multiple managed containers match Assignment %s", assignmentID)
	}
	return lines[0], true, nil
}

func (d DockerRuntime) Start(ctx context.Context, spec ContainerSpec) (string, error) {
	binary := d.binary()
	if err := validateContainerSpec(spec); err != nil {
		return "", err
	}
	if _, err := commandOutput(ctx, 16<<10, binary, "pull", spec.Image); err != nil {
		return "", fmt.Errorf("pull workload image: %w", err)
	}
	digests, err := commandOutput(ctx, 32<<10, binary, "image", "inspect", "--format", "{{json .RepoDigests}}", spec.Image)
	if err != nil {
		return "", fmt.Errorf("inspect pulled workload image: %w", err)
	}
	resolvedImage, err := resolvedImageReference(spec.Image, digests)
	if err != nil {
		return "", err
	}
	name := "gemcp-" + strings.ReplaceAll(spec.AssignmentID, "-", "")[:16]
	args := []string{
		"create", "--name", name,
		"--label", "io.gemcp.managed=true",
		"--label", "io.gemcp.assignment=" + spec.AssignmentID,
		"--label", "io.gemcp.image=" + resolvedImage,
		"--runtime", "nvidia", "--gpus", "device=" + spec.GPUUUID,
		"--network", "bridge", "--ipc", "private",
		"--cpus", strconv.Itoa(spec.CPULimit), "--memory", strconv.FormatInt(spec.MemoryLimitBytes, 10),
		"--pids-limit", "4096", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--read-only", "--tmpfs", "/tmp:rw,nosuid,nodev,size=8589934592", "--shm-size", "1g",
		"--mount", "type=bind,src=" + spec.SourcePath + ",dst=/workspace",
		"--mount", "type=bind,src=" + spec.OutputPath + ",dst=/outputs",
		"--workdir", "/workspace", "--env", "GEMCP_OUTPUT_DIR=/outputs",
	}
	if spec.WorkspaceMode == "trusted_rw" {
		args = append(args, "--mount", "type=bind,src="+spec.WorkspacePath+",dst=/gemcp/workspace", "--env", "GEMCP_TRUSTED_WORKSPACE=/gemcp/workspace")
		for _, dataset := range spec.WorkspaceDatasets {
			args = append(args, "--env", dataset.EnvironmentVariable+"=/gemcp/workspace/"+dataset.RelativePath)
		}
	}
	if spec.ExecutionMode == executioncmd.ModeArgv {
		args = append(args, "--entrypoint", spec.Argv[0], resolvedImage)
		args = append(args, spec.Argv[1:]...)
	} else {
		args = append(args, resolvedImage, "/bin/sh", "-lc", spec.Command)
	}
	containerID, err := commandOutput(ctx, 16<<10, binary, args...)
	if err != nil {
		return "", fmt.Errorf("create workload container: %w", err)
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", fmt.Errorf("Docker returned no workload container ID")
	}
	if _, err := commandOutput(ctx, 16<<10, binary, "start", containerID); err != nil {
		_, _ = commandOutput(context.Background(), 16<<10, binary, "rm", "-f", containerID)
		return "", fmt.Errorf("start workload container: %w", err)
	}
	return containerID, nil
}

func (d DockerRuntime) Image(ctx context.Context, containerID string) (string, error) {
	value, err := commandOutput(ctx, 4<<10, d.binary(), "inspect", "--format", "{{index .Config.Labels \"io.gemcp.image\"}}", containerID)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if !pinnedOCIImage.MatchString(value) {
		return "", fmt.Errorf("managed workload image label is invalid")
	}
	return value, nil
}

func (d DockerRuntime) Inspect(ctx context.Context, containerID string) (ContainerState, error) {
	var result ContainerState
	output, err := commandOutput(ctx, 32<<10, d.binary(), "inspect", "--format", "{{json .State}}", containerID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such") {
			return result, ErrContainerNotFound
		}
		return result, err
	}
	var state struct {
		Running    bool   `json:"Running"`
		ExitCode   int    `json:"ExitCode"`
		OOMKilled  bool   `json:"OOMKilled"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &state); err != nil {
		return result, fmt.Errorf("decode Docker workload state: %w", err)
	}
	result.Running, result.ExitCode, result.OOMKilled = state.Running, state.ExitCode, state.OOMKilled
	result.StartedAt, _ = time.Parse(time.RFC3339Nano, state.StartedAt)
	result.FinishedAt, _ = time.Parse(time.RFC3339Nano, state.FinishedAt)
	return result, nil
}

func (d DockerRuntime) Stop(ctx context.Context, containerID string, graceSeconds int) error {
	if graceSeconds < 0 || graceSeconds > 3600 {
		return fmt.Errorf("termination grace is invalid")
	}
	if _, err := commandOutput(ctx, 16<<10, d.binary(), "stop", "--time", strconv.Itoa(graceSeconds), containerID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such") {
			return ErrContainerNotFound
		}
		if _, killErr := commandOutput(ctx, 16<<10, d.binary(), "kill", containerID); killErr != nil {
			return fmt.Errorf("stop workload container: %w", err)
		}
	}
	return nil
}

func (d DockerRuntime) Logs(ctx context.Context, containerID string, maximum int) (string, error) {
	if maximum <= 0 {
		return "", nil
	}
	return combinedCommandOutput(ctx, maximum, d.binary(), "logs", "--tail", "10000", containerID)
}

func (d DockerRuntime) SaveLogs(ctx context.Context, containerID, filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("create workload log directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".run-*.log")
	if err != nil {
		return fmt.Errorf("create workload log: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	command := exec.CommandContext(ctx, d.binary(), "logs", containerID)
	command.Stdout = temporary
	command.Stderr = temporary
	commandErr := command.Run()
	if syncErr := temporary.Sync(); commandErr == nil && syncErr != nil {
		commandErr = syncErr
	}
	if closeErr := temporary.Close(); commandErr == nil && closeErr != nil {
		commandErr = closeErr
	}
	if commandErr != nil {
		return fmt.Errorf("persist complete workload logs: %w", commandErr)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("install workload log: %w", err)
	}
	return nil
}

func (d DockerRuntime) Remove(ctx context.Context, containerID string) error {
	_, err := commandOutput(ctx, 16<<10, d.binary(), "rm", "-f", containerID)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such") {
		return err
	}
	return nil
}

func (d DockerRuntime) binary() string {
	if strings.TrimSpace(d.Binary) == "" {
		return "docker"
	}
	return d.Binary
}

func validateContainerSpec(spec ContainerSpec) error {
	if _, err := executioncmd.Validate(executioncmd.Spec{Mode: spec.ExecutionMode, Command: spec.Command, Argv: spec.Argv}); err != nil {
		return fmt.Errorf("workload execution specification is invalid: %w", err)
	}
	imageValid := pinnedOCIImage.MatchString(spec.Image)
	if spec.WorkspaceMode == "trusted_rw" {
		imageValid = workspaceOCIImage.MatchString(spec.Image) && validTrustedWorkspacePath(spec.WorkspacePath)
	} else if spec.WorkspaceMode != "" || spec.WorkspacePath != "" {
		imageValid = false
	}
	if len(strings.ReplaceAll(spec.AssignmentID, "-", "")) < 16 || !nvidiaGPUUUID.MatchString(spec.GPUUUID) ||
		spec.CPULimit <= 0 || spec.MemoryLimitBytes <= 0 || strings.ContainsAny(spec.SourcePath+spec.OutputPath, ",\x00") ||
		!imageValid || !validWorkspaceDatasets(spec.WorkspaceMode, spec.WorkspaceDatasets) {
		return fmt.Errorf("workload container specification is invalid")
	}
	return nil
}

func resolvedImageReference(requested, raw string) (string, error) {
	var digests []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &digests); err != nil {
		return "", fmt.Errorf("decode pulled image digests: %w", err)
	}
	requestedDigest := ""
	if index := strings.LastIndex(requested, "@sha256:"); index >= 0 {
		requestedDigest = requested[index:]
	}
	for _, candidate := range digests {
		candidate = strings.TrimSpace(candidate)
		if !pinnedOCIImage.MatchString(candidate) || (requestedDigest != "" && !strings.HasSuffix(candidate, requestedDigest)) {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("pulled image did not resolve to a repository digest")
}

func commandOutput(ctx context.Context, maximum int, binary string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, args...)
	stdout := &tailBuffer{maximum: maximum}
	stderr := &tailBuffer{maximum: maximum}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	value := strings.TrimSpace(stdout.String())
	if err != nil {
		diagnostics := strings.TrimSpace(stderr.String())
		if diagnostics == "" {
			diagnostics = value
		}
		return value, fmt.Errorf("%w: %s", err, diagnostics)
	}
	return value, nil
}

func combinedCommandOutput(ctx context.Context, maximum int, binary string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, args...)
	output := &tailBuffer{maximum: maximum}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	value := strings.TrimSpace(output.String())
	if err != nil {
		return value, fmt.Errorf("%w: %s", err, value)
	}
	return value, nil
}

type tailBuffer struct {
	maximum int
	buffer  bytes.Buffer
}

func (w *tailBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if w.maximum <= 0 {
		return original, nil
	}
	if len(value) >= w.maximum {
		w.buffer.Reset()
		_, _ = w.buffer.Write(value[len(value)-w.maximum:])
		return original, nil
	}
	if w.buffer.Len()+len(value) > w.maximum {
		current := w.buffer.Bytes()
		keep := w.maximum - len(value)
		trimmed := append([]byte(nil), current[len(current)-keep:]...)
		w.buffer.Reset()
		_, _ = w.buffer.Write(trimmed)
	}
	_, _ = w.buffer.Write(value)
	return original, nil
}

func (w *tailBuffer) String() string { return w.buffer.String() }

var _ io.Writer = (*tailBuffer)(nil)
