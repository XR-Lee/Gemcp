package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
)

var pinnedOCIImage = regexp.MustCompile(`^[^[:space:]@]+@sha256:[0-9a-f]{64}$`)
var workspaceOCIImage = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*(?:@sha256:[0-9a-f]{64})?$`)
var nvidiaGPUUUID = regexp.MustCompile(`^GPU-[A-Za-z0-9-]{1,116}$`)
var datasetEnvironmentVariable = regexp.MustCompile(`^GEMCP_DATASET_[A-Z0-9_]{1,112}$`)

const (
	maxLogTailBytes = 64 << 10
	maxMetricsBytes = 64 << 10
)

type WorkloadManager struct {
	config  Config
	token   string
	client  *Client
	store   *Store
	runtime ContainerRuntime
	now     func() time.Time
}

func NewWorkloadManager(config Config, token string, client *Client, store *Store, runtime ContainerRuntime) (*WorkloadManager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if token == "" || client == nil || store == nil || runtime == nil {
		return nil, fmt.Errorf("workload manager dependencies are required")
	}
	return &WorkloadManager{config: config, token: token, client: client, store: store, runtime: runtime, now: time.Now}, nil
}

func (m *WorkloadManager) Busy() (bool, error) {
	records, err := m.store.Workloads()
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if !record.CleanupReported {
			return true, nil
		}
	}
	return false, nil
}

func (m *WorkloadManager) Start(ctx context.Context, payload map[string]any) (map[string]any, error) {
	spec, err := decodeStartWorkload(payload)
	if err != nil {
		return nil, err
	}
	if spec.WorkspaceMode == "trusted_rw" {
		spec.WorkspacePath, err = resolvedTrustedWorkspacePath(spec.WorkspacePath)
		if err != nil {
			return nil, err
		}
		if pathContains(m.config.StorageRoot, spec.WorkspacePath) || pathContains(spec.WorkspacePath, m.config.StorageRoot) {
			return nil, fmt.Errorf("trusted workspace must be separate from Gemcp managed storage")
		}
		if err := validateWorkspaceDatasetsOnHost(spec.WorkspacePath, spec.WorkspaceDatasets); err != nil {
			return nil, err
		}
	}
	records, err := m.store.Workloads()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.AssignmentID == spec.AssignmentID {
			if err := m.reportStarted(record); err != nil {
				return nil, err
			}
			return map[string]any{"workload_id": record.ContainerID, "state": record.State}, nil
		}
		if !record.CleanupReported {
			return nil, fmt.Errorf("node already has an active workload")
		}
	}

	sourcePath := filepath.Join(m.config.StorageRoot, "workloads", spec.AssignmentID, "source")
	outputPath, err := managedOutputPath(m.config.StorageRoot, spec.OutputRef)
	if err != nil {
		return nil, err
	}
	containerID, found, err := m.runtime.Find(ctx, spec.AssignmentID)
	if err != nil {
		return nil, fmt.Errorf("reconcile existing workload container: %w", err)
	}
	if !found {
		if err := m.prepareSource(ctx, spec, sourcePath); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(outputPath, 0o700); err != nil {
			return nil, fmt.Errorf("create managed output directory: %w", err)
		}
		containerID, err = m.runtime.Start(ctx, ContainerSpec{
			AssignmentID: spec.AssignmentID, Image: spec.Image, ExecutionMode: spec.ExecutionMode,
			Command: spec.Command, Argv: append([]string(nil), spec.Argv...), GPUUUID: spec.GPUUUID,
			CPULimit: spec.CPULimit, MemoryLimitBytes: spec.MemoryLimitBytes, SourcePath: sourcePath, OutputPath: outputPath,
			WorkspaceMode: spec.WorkspaceMode, WorkspacePath: spec.WorkspacePath, WorkspaceDatasets: append([]nodeprotocol.WorkspaceDataset(nil), spec.WorkspaceDatasets...),
		})
		if err != nil {
			return nil, err
		}
	}
	resolvedImage, err := m.runtime.Image(ctx, containerID)
	if err != nil {
		_ = m.runtime.Stop(context.Background(), containerID, 0)
		_ = m.runtime.Remove(context.Background(), containerID)
		return nil, fmt.Errorf("read managed workload image: %w", err)
	}
	now := m.now().UTC()
	record := WorkloadRecord{
		AssignmentID: spec.AssignmentID, ExperimentID: spec.ExperimentID, AttemptID: spec.AttemptID,
		ContainerID: containerID, GPUUUID: spec.GPUUUID, GPUName: spec.GPUName, ResolvedImage: resolvedImage,
		OutputRef: spec.OutputRef, State: "running", StartedAt: now,
		DeadlineAt:              now.Add(time.Duration(spec.MaxRuntimeSeconds+spec.TimeoutExtensionSeconds) * time.Second),
		TerminationGraceSeconds: spec.TerminationGraceSeconds,
	}
	if err := m.store.SaveWorkload(record); err != nil {
		return nil, err
	}
	if err := m.reportStarted(record); err != nil {
		return nil, err
	}
	return map[string]any{"workload_id": containerID, "state": "running"}, nil
}

func (m *WorkloadManager) Stop(ctx context.Context, payload map[string]any) (map[string]any, error) {
	var spec nodeprotocol.StopWorkload
	if err := decodePayload(payload, &spec); err != nil || uuid.Validate(spec.AssignmentID) != nil || !validStopReason(spec.Reason) || spec.TerminationGraceSeconds < 0 || spec.TerminationGraceSeconds > 3600 {
		return nil, fmt.Errorf("stop workload command is invalid")
	}
	records, err := m.store.Workloads()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.AssignmentID != spec.AssignmentID {
			continue
		}
		record.State = "stopping"
		record.StopReason = spec.Reason
		if err := m.store.SaveWorkload(record); err != nil {
			return nil, err
		}
		err := m.runtime.Stop(ctx, record.ContainerID, spec.TerminationGraceSeconds)
		if err != nil && !errors.Is(err, ErrContainerNotFound) {
			return nil, err
		}
		return map[string]any{"workload_id": record.ContainerID, "state": "stopping"}, nil
	}
	containerID, found, err := m.runtime.Find(ctx, spec.AssignmentID)
	if err != nil {
		return nil, err
	}
	if found {
		if err := m.runtime.Stop(ctx, containerID, spec.TerminationGraceSeconds); err != nil && !errors.Is(err, ErrContainerNotFound) {
			return nil, err
		}
	}
	return map[string]any{"workload_id": containerID, "state": "absent"}, nil
}

func (m *WorkloadManager) Reconcile(ctx context.Context) error {
	records, err := m.store.Workloads()
	if err != nil {
		return err
	}
	for _, record := range records {
		state, err := m.runtime.Inspect(ctx, record.ContainerID)
		if errors.Is(err, ErrContainerNotFound) {
			if !record.FinishedReported {
				if err := m.finish(record, 70, "runner_error", "managed workload container disappeared"); err != nil {
					return err
				}
				record.State, record.FinishedReported = "finished", true
				if err := m.store.SaveWorkload(record); err != nil {
					return err
				}
			}
			if err := m.reportCleanup(&record); err != nil {
				return err
			}
			if err := m.store.DeleteWorkload(record.AssignmentID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect managed workload: %w", err)
		}
		now := m.now().UTC()
		if state.Running {
			if record.StopReason == "" && !now.Before(record.DeadlineAt) {
				record.State, record.StopReason = "stopping", "timeout"
				if err := m.store.SaveWorkload(record); err != nil {
					return err
				}
				if err := m.runtime.Stop(ctx, record.ContainerID, record.TerminationGraceSeconds); err != nil && !errors.Is(err, ErrContainerNotFound) {
					return err
				}
				continue
			}
			if record.LastHeartbeatAt.IsZero() || !now.Before(record.LastHeartbeatAt.Add(15*time.Second)) {
				outputPath, outputErr := managedOutputPath(m.config.StorageRoot, record.OutputRef)
				if outputErr != nil {
					return outputErr
				}
				logTail, _ := m.runtime.Logs(ctx, record.ContainerID, maxLogTailBytes)
				payload := map[string]any{
					"assignment_id": record.AssignmentID, "log_tail": boundedUTF8Tail(logTail, maxLogTailBytes),
					"metrics": readMetrics(outputPath),
				}
				if _, err := m.store.AppendEvent("workload_heartbeat", payload, now); err != nil {
					return err
				}
				record.LastHeartbeatAt = now
				if err := m.store.SaveWorkload(record); err != nil {
					return err
				}
			}
			continue
		}
		reason := record.StopReason
		if reason == "" {
			reason = "completed"
			if state.OOMKilled {
				reason = "oom"
			}
		}
		if !record.FinishedReported {
			outputPath, err := managedOutputPath(m.config.StorageRoot, record.OutputRef)
			if err != nil {
				return err
			}
			logPath := filepath.Join(outputPath, "run.log")
			if err := m.runtime.SaveLogs(ctx, record.ContainerID, logPath); err != nil {
				return err
			}
			logs := fileTail(logPath, maxLogTailBytes)
			if err := m.finish(record, state.ExitCode, reason, logs); err != nil {
				return err
			}
			record.State, record.FinishedReported = "finished", true
			if err := m.store.SaveWorkload(record); err != nil {
				return err
			}
		}
		if err := m.runtime.Remove(ctx, record.ContainerID); err != nil {
			return err
		}
		if err := m.reportCleanup(&record); err != nil {
			return err
		}
		if err := m.store.DeleteWorkload(record.AssignmentID); err != nil {
			return err
		}
	}
	return nil
}

func (m *WorkloadManager) reportStarted(record WorkloadRecord) error {
	if record.StartedReported {
		return nil
	}
	payload := map[string]any{"assignment_id": record.AssignmentID, "workload_id": record.ContainerID}
	if nvidiaGPUUUID.MatchString(record.GPUUUID) && validGPUName(record.GPUName) {
		payload["runtime_info"] = map[string]any{
			"source": "node_binding", "working_directory": "/workspace", "output_directory": "/outputs",
			"cuda_visible_devices": record.GPUUUID,
			"gpu_devices":          []map[string]any{{"index": 0, "uuid": record.GPUUUID, "name": record.GPUName}},
		}
	}
	if _, err := m.store.AppendEvent("workload_started", payload, m.now().UTC()); err != nil {
		return err
	}
	record.StartedReported = true
	return m.store.SaveWorkload(record)
}

func (m *WorkloadManager) reportCleanup(record *WorkloadRecord) error {
	if record.CleanupReported {
		return nil
	}
	if _, err := m.store.AppendEvent("workload_cleanup_complete", map[string]any{
		"assignment_id": record.AssignmentID, "workload_id": record.ContainerID,
	}, m.now().UTC()); err != nil {
		return err
	}
	record.CleanupReported = true
	return m.store.SaveWorkload(*record)
}

func (m *WorkloadManager) finish(record WorkloadRecord, exitCode int, reason, logTail string) error {
	outputPath, err := managedOutputPath(m.config.StorageRoot, record.OutputRef)
	if err != nil {
		return err
	}
	logTail = boundedUTF8Tail(logTail, maxLogTailBytes)
	payload := map[string]any{
		"assignment_id": record.AssignmentID, "exit_code": exitCode, "reason": reason,
		"log_tail": logTail, "metrics": readMetrics(outputPath), "resolved_image": record.ResolvedImage,
	}
	if _, err := m.store.AppendEvent("workload_finished", payload, m.now().UTC()); err != nil {
		return err
	}
	return nil
}

func (m *WorkloadManager) prepareSource(ctx context.Context, spec nodeprotocol.StartWorkload, destination string) error {
	if info, err := os.Stat(destination); err == nil && info.IsDir() {
		return nil
	}
	root := filepath.Dir(destination)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	archivePath := filepath.Join(root, "source.tar.gz")
	if err := m.client.DownloadSource(ctx, m.token, spec.SourcePath, archivePath, spec.SourceMaxBytes); err != nil {
		return err
	}
	temporary := destination + ".extracting"
	_ = os.RemoveAll(temporary)
	if err := extractSourceArchive(archivePath, temporary, spec.SourceMaxBytes); err != nil {
		_ = os.RemoveAll(temporary)
		return err
	}
	_ = os.Remove(archivePath)
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("install extracted source: %w", err)
	}
	return nil
}

func decodeStartWorkload(payload map[string]any) (nodeprotocol.StartWorkload, error) {
	var spec nodeprotocol.StartWorkload
	if err := decodePayload(payload, &spec); err != nil {
		return spec, err
	}
	execution, executionErr := executioncmd.Validate(executioncmd.Spec{Mode: spec.ExecutionMode, Command: spec.Command, Argv: spec.Argv})
	if executionErr != nil || uuid.Validate(spec.AssignmentID) != nil || uuid.Validate(spec.ExperimentID) != nil || uuid.Validate(spec.AttemptID) != nil ||
		!validWorkloadImage(spec) || !validWorkspaceDatasets(spec.WorkspaceMode, spec.WorkspaceDatasets) ||
		spec.SourcePath != "/api/v1/node-assignments/"+spec.AssignmentID+"/source" || spec.SourceMaxBytes <= 0 || spec.SourceMaxBytes > 1<<30 ||
		!validOutputRef(spec.OutputRef) || spec.MaxRuntimeSeconds <= 0 || spec.MaxRuntimeSeconds > 30*24*3600 ||
		spec.TimeoutExtensionSeconds < 0 || spec.TerminationGraceSeconds < 0 || spec.TerminationGraceSeconds > 3600 ||
		!nvidiaGPUUUID.MatchString(spec.GPUUUID) || !validGPUName(spec.GPUName) ||
		spec.CPULimit <= 0 || spec.CPULimit > 1024 || spec.MemoryLimitBytes <= 0 {
		return spec, fmt.Errorf("start workload command is invalid")
	}
	spec.ExecutionMode, spec.Command, spec.Argv = execution.Mode, execution.Command, execution.Argv
	return spec, nil
}

func validWorkloadImage(spec nodeprotocol.StartWorkload) bool {
	if spec.WorkspaceMode == "" && spec.WorkspacePath == "" && len(spec.WorkspaceDatasets) == 0 {
		return pinnedOCIImage.MatchString(spec.Image)
	}
	return spec.WorkspaceMode == "trusted_rw" && validTrustedWorkspacePath(spec.WorkspacePath) && workspaceOCIImage.MatchString(spec.Image)
}

func validWorkspaceDatasets(workspaceMode string, datasets []nodeprotocol.WorkspaceDataset) bool {
	if len(datasets) == 0 {
		return true
	}
	if workspaceMode != "trusted_rw" || len(datasets) > 32 {
		return false
	}
	names, paths, variables := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, dataset := range datasets {
		if dataset.Name == "" || len(dataset.Name) > 100 || dataset.RelativePath == "" || len(dataset.RelativePath) > 1024 ||
			path.IsAbs(dataset.RelativePath) || path.Clean(dataset.RelativePath) != dataset.RelativePath || dataset.RelativePath == "." ||
			dataset.RelativePath == ".." || strings.HasPrefix(dataset.RelativePath, "../") ||
			strings.Contains(dataset.RelativePath, "\\") || !datasetEnvironmentVariable.MatchString(dataset.EnvironmentVariable) ||
			names[dataset.Name] || paths[dataset.RelativePath] || variables[dataset.EnvironmentVariable] {
			return false
		}
		for _, value := range []string{dataset.Name, dataset.RelativePath} {
			for _, character := range value {
				if unicode.IsControl(character) {
					return false
				}
			}
		}
		names[dataset.Name], paths[dataset.RelativePath], variables[dataset.EnvironmentVariable] = true, true, true
	}
	return true
}

func validateWorkspaceDatasetsOnHost(workspaceRoot string, datasets []nodeprotocol.WorkspaceDataset) error {
	resolvedRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return fmt.Errorf("approved workspace root is unavailable")
	}
	for _, dataset := range datasets {
		candidate := filepath.Join(resolvedRoot, filepath.FromSlash(dataset.RelativePath))
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return fmt.Errorf("workspace dataset %s is unavailable", dataset.Name)
		}
		if !pathContains(resolvedRoot, resolved) {
			return fmt.Errorf("workspace dataset %s escapes the approved root", dataset.Name)
		}
		if _, err := os.Stat(resolved); err != nil {
			return fmt.Errorf("workspace dataset %s is unavailable", dataset.Name)
		}
	}
	return nil
}

func validTrustedWorkspacePath(value string) bool {
	if value == "" || len(value) > 4096 || !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsAny(value, ",\x00") {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	for _, protected := range []string{"/", "/boot", "/dev", "/etc", "/proc", "/run", "/sys", "/usr", "/var/lib/docker"} {
		if value == protected || (protected != "/" && strings.HasPrefix(value, protected+string(filepath.Separator))) {
			return false
		}
	}
	return true
}

func resolvedTrustedWorkspacePath(value string) (string, error) {
	if !validTrustedWorkspacePath(value) {
		return "", fmt.Errorf("trusted workspace path is invalid")
	}
	return value, nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validGPUName(value string) bool {
	if strings.TrimSpace(value) == "" || len(value) > 120 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func decodePayload(payload map[string]any, target any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("decode workload command: %w", err)
	}
	return nil
}

func managedOutputPath(root, outputRef string) (string, error) {
	if !validOutputRef(outputRef) {
		return "", fmt.Errorf("managed output reference is invalid")
	}
	root = filepath.Clean(root)
	result := filepath.Clean(filepath.Join(root, filepath.FromSlash(outputRef)))
	if result == root || !strings.HasPrefix(result, root+string(filepath.Separator)) {
		return "", fmt.Errorf("managed output reference escapes storage root")
	}
	return result, nil
}

func validOutputRef(value string) bool {
	return strings.HasPrefix(value, "experiments/") && !strings.Contains(value, "\\") && filepath.Clean(filepath.FromSlash(value)) == filepath.FromSlash(value)
}

func validStopReason(value string) bool {
	switch value {
	case "cancelled", "timeout", "emergency", "completed", "provider_error":
		return true
	default:
		return false
	}
}

func boundedUTF8Tail(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "\ufffd")
	bytes := []byte(value)
	if len(bytes) <= maximum {
		return value
	}
	bytes = bytes[len(bytes)-maximum:]
	for !utf8.Valid(bytes) && len(bytes) > 0 {
		bytes = bytes[1:]
	}
	return string(bytes)
}

func readMetrics(outputPath string) map[string]any {
	filename := filepath.Join(outputPath, "metrics.json")
	info, err := os.Stat(filename)
	if err != nil || info.Size() > maxMetricsBytes {
		return map[string]any{}
	}
	payload, err := os.ReadFile(filename)
	if err != nil || !utf8.Valid(payload) {
		return map[string]any{}
	}
	var result map[string]any
	if json.Unmarshal(payload, &result) != nil || result == nil {
		return map[string]any{}
	}
	return result
}

func fileTail(filename string, maximum int) string {
	file, err := os.Open(filename)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	start := info.Size() - int64(maximum)
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, 0); err != nil {
		return ""
	}
	payload, err := io.ReadAll(io.LimitReader(file, int64(maximum)))
	if err != nil {
		return ""
	}
	return boundedUTF8Tail(string(payload), maximum)
}
