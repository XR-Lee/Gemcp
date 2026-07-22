package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
)

var pinnedOCIImage = regexp.MustCompile(`^[^[:space:]@]+@sha256:[0-9a-f]{64}$`)
var nvidiaGPUUUID = regexp.MustCompile(`^GPU-[A-Za-z0-9-]{1,116}$`)

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
		if record.State == "running" || record.State == "stopping" {
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
		if record.State == "running" || record.State == "stopping" {
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
			AssignmentID: spec.AssignmentID, Image: spec.Image, Command: spec.Command, GPUUUID: spec.GPUUUID,
			CPULimit: spec.CPULimit, MemoryLimitBytes: spec.MemoryLimitBytes, SourcePath: sourcePath, OutputPath: outputPath,
		})
		if err != nil {
			return nil, err
		}
	}
	now := m.now().UTC()
	record := WorkloadRecord{
		AssignmentID: spec.AssignmentID, ExperimentID: spec.ExperimentID, AttemptID: spec.AttemptID,
		ContainerID: containerID, OutputRef: spec.OutputRef, State: "running", StartedAt: now,
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
			if err := m.finish(record, 70, "runner_error", "managed workload container disappeared"); err != nil {
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
			if record.LastHeartbeatAt.IsZero() || !now.Before(record.LastHeartbeatAt.Add(time.Minute)) {
				if _, err := m.store.AppendEvent("workload_heartbeat", map[string]any{"assignment_id": record.AssignmentID}, now); err != nil {
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
		if err := m.runtime.Remove(ctx, record.ContainerID); err != nil {
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
	if _, err := m.store.AppendEvent("workload_started", map[string]any{
		"assignment_id": record.AssignmentID, "workload_id": record.ContainerID,
	}, m.now().UTC()); err != nil {
		return err
	}
	record.StartedReported = true
	return m.store.SaveWorkload(record)
}

func (m *WorkloadManager) finish(record WorkloadRecord, exitCode int, reason, logTail string) error {
	outputPath, err := managedOutputPath(m.config.StorageRoot, record.OutputRef)
	if err != nil {
		return err
	}
	logTail = boundedUTF8Tail(logTail, maxLogTailBytes)
	payload := map[string]any{
		"assignment_id": record.AssignmentID, "exit_code": exitCode, "reason": reason,
		"log_tail": logTail, "metrics": readMetrics(outputPath),
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
	if uuid.Validate(spec.AssignmentID) != nil || uuid.Validate(spec.ExperimentID) != nil || uuid.Validate(spec.AttemptID) != nil ||
		!pinnedOCIImage.MatchString(spec.Image) || spec.Command == "" || len(spec.Command) > 256<<10 ||
		spec.SourcePath != "/api/v1/node-assignments/"+spec.AssignmentID+"/source" || spec.SourceMaxBytes <= 0 || spec.SourceMaxBytes > 1<<30 ||
		!validOutputRef(spec.OutputRef) || spec.MaxRuntimeSeconds <= 0 || spec.MaxRuntimeSeconds > 30*24*3600 ||
		spec.TimeoutExtensionSeconds < 0 || spec.TerminationGraceSeconds < 0 || spec.TerminationGraceSeconds > 3600 ||
		!nvidiaGPUUUID.MatchString(spec.GPUUUID) || spec.CPULimit <= 0 || spec.CPULimit > 1024 || spec.MemoryLimitBytes <= 0 {
		return spec, fmt.Errorf("start workload command is invalid")
	}
	return spec, nil
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
