package phasezero

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/google/uuid"
)

const MaxPhaseZeroSpendMilli int64 = 20_000

var safePath = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

type JobSpec struct {
	Backend                 string   `json:"backend,omitempty"`
	Region                  string   `json:"region,omitempty"`
	GPUNames                []string `json:"gpu_names"`
	GPUNum                  int      `json:"gpu_num"`
	CUDAVersion             int      `json:"cuda_version,omitempty"`
	CUDAFrom                int      `json:"cuda_from,omitempty"`
	CUDATo                  int      `json:"cuda_to,omitempty"`
	CPUFrom                 int      `json:"cpu_from"`
	CPUTo                   int      `json:"cpu_to"`
	MemoryFromGB            int      `json:"memory_from_gb"`
	MemoryToGB              int      `json:"memory_to_gb"`
	PriceFromMilliPerHour   int64    `json:"price_from_milli_per_hour"`
	PriceToMilliPerHour     int64    `json:"price_to_milli_per_hour"`
	ImageUUID               string   `json:"image_uuid"`
	MaxRuntimeSeconds       int      `json:"max_runtime_seconds"`
	ProvisionTimeoutSeconds int      `json:"provision_timeout_seconds"`
	OutputRoot              string   `json:"output_root"`
	ReuseContainer          bool     `json:"reuse_container"`
	Minimal                 bool     `json:"minimal,omitempty"`
}

type JobReport struct {
	ProbeID                    string                  `json:"probe_id"`
	Backend                    string                  `json:"backend"`
	StartedAt                  time.Time               `json:"started_at"`
	FinishedAt                 time.Time               `json:"finished_at"`
	DeploymentUUID             string                  `json:"deployment_uuid,omitempty"`
	OutputPath                 string                  `json:"output_path"`
	EstimatedMaximumSpendMilli int64                   `json:"estimated_maximum_spend_milli"`
	SpendCapMilli              int64                   `json:"spend_cap_milli"`
	TerminalStatus             string                  `json:"terminal_status,omitempty"`
	ProviderStatus             string                  `json:"provider_status,omitempty"`
	FinishedNum                int                     `json:"finished_num,omitempty"`
	FailedNum                  int                     `json:"failed_num,omitempty"`
	Containers                 []autodl.Container      `json:"containers,omitempty"`
	ReleasedContainers         []autodl.Container      `json:"released_containers,omitempty"`
	Events                     []autodl.ContainerEvent `json:"events,omitempty"`
	RequestIDs                 map[string]string       `json:"request_ids,omitempty"`
	ObservationWarnings        []string                `json:"observation_warnings,omitempty"`
	CleanupWarnings            []string                `json:"cleanup_warnings,omitempty"`
	CleanupConfirmed           bool                    `json:"cleanup_confirmed"`
}

type jobAPI interface {
	CreateElasticDeployment(context.Context, autodl.ElasticDeploymentCreate) (autodl.DeploymentCreateResult, string, error)
	CreatePrivateElasticDeployment(context.Context, autodl.PrivateElasticDeploymentCreate) (autodl.DeploymentCreateResult, string, error)
	ElasticDeployments(context.Context, int, int, string) (autodl.Page[autodl.Deployment], string, error)
	ElasticContainers(context.Context, string, int, int) (autodl.Page[autodl.Container], string, error)
	ElasticContainersWithReleased(context.Context, string, bool, int, int) (autodl.Page[autodl.Container], string, error)
	ElasticEvents(context.Context, string, int, int, int) (autodl.Page[autodl.ContainerEvent], string, error)
	StopElasticDeployment(context.Context, string) (string, error)
	DeleteElasticDeployment(context.Context, string) (string, error)
}

type JobRunner struct {
	API          jobAPI
	PollInterval time.Duration
	Now          func() time.Time
	Sleep        func(context.Context, time.Duration) error
}

func NewJobRunner(api jobAPI) *JobRunner {
	return &JobRunner{
		API:          api,
		PollInterval: 5 * time.Second,
		Now:          time.Now,
		Sleep: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (r *JobRunner) Run(ctx context.Context, spec JobSpec, spendCapMilli int64) (report JobReport, resultErr error) {
	if err := spec.Validate(); err != nil {
		return report, err
	}
	if spendCapMilli <= 0 || spendCapMilli > MaxPhaseZeroSpendMilli {
		return report, fmt.Errorf("spend cap must be between 1 and %d milli-CNY", MaxPhaseZeroSpendMilli)
	}
	estimate := spec.EstimatedMaximumSpendMilli()
	if estimate > spendCapMilli {
		return report, fmt.Errorf("estimated maximum spend %d milli-CNY exceeds cap %d", estimate, spendCapMilli)
	}

	now := r.Now().UTC()
	probeID := fmt.Sprintf("gemcp-phase0-%s-%s", now.Format("20060102t150405z"), uuid.NewString()[:8])
	outputPath := strings.TrimRight(spec.OutputRoot, "/") + "/" + probeID
	report = JobReport{
		ProbeID:                    probeID,
		Backend:                    spec.BackendName(),
		StartedAt:                  now,
		OutputPath:                 outputPath,
		EstimatedMaximumSpendMilli: estimate,
		SpendCapMilli:              spendCapMilli,
		RequestIDs:                 map[string]string{},
	}

	var created autodl.DeploymentCreateResult
	var requestID string
	var err error
	if spec.BackendName() == "private" {
		created, requestID, err = r.API.CreatePrivateElasticDeployment(ctx, spec.privateDeployment(probeID, outputPath))
	} else {
		created, requestID, err = r.API.CreateElasticDeployment(ctx, spec.deployment(probeID, outputPath))
	}
	setRequestID(report.RequestIDs, "create", requestID)
	if err != nil || created.DeploymentUUID == "" {
		createErr := err
		if createErr == nil {
			createErr = fmt.Errorf("create response contained no deployment UUID")
		}
		recovered, recoveryRequestID, recoveryErr := r.recoverDeploymentByName(ctx, probeID)
		setRequestID(report.RequestIDs, "create_recovery", recoveryRequestID)
		if recoveryErr != nil {
			return report, errors.Join(fmt.Errorf("create phase-zero deployment: %w", createErr), fmt.Errorf("recover uncertain create result: %w", recoveryErr))
		}
		if recovered.UUID == "" {
			return report, fmt.Errorf("create phase-zero deployment: %w", createErr)
		}
		created.DeploymentUUID = recovered.UUID
		report.ObservationWarnings = append(report.ObservationWarnings, "create response was uncertain; deployment ownership recovered by unique name")
	}
	report.DeploymentUUID = created.DeploymentUUID

	defer func() {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 45*time.Second)
		stopRequestID, stopErr := r.API.StopElasticDeployment(stopCtx, report.DeploymentUUID)
		cancelStop()
		setRequestID(report.RequestIDs, "cleanup_stop", stopRequestID)
		if stopErr != nil {
			report.CleanupWarnings = append(report.CleanupWarnings, "stop deployment: "+stopErr.Error())
			resultErr = errors.Join(resultErr, fmt.Errorf("stop phase-zero deployment: %w", stopErr))
		}
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), 45*time.Second)
		deleteRequestID, deleteErr := r.API.DeleteElasticDeployment(deleteCtx, report.DeploymentUUID)
		cancelDelete()
		setRequestID(report.RequestIDs, "cleanup_delete", deleteRequestID)
		if deleteErr != nil {
			report.CleanupWarnings = append(report.CleanupWarnings, "delete deployment: "+deleteErr.Error())
			resultErr = errors.Join(resultErr, fmt.Errorf("delete phase-zero deployment: %w", deleteErr))
		}
		if deleteErr == nil {
			confirmCtx, cancelConfirm := context.WithTimeout(context.Background(), 30*time.Second)
			confirmRequestID, confirmErr := r.confirmDeleted(confirmCtx, report.DeploymentUUID)
			cancelConfirm()
			setRequestID(report.RequestIDs, "cleanup_confirm", confirmRequestID)
			if confirmErr != nil {
				report.CleanupWarnings = append(report.CleanupWarnings, "confirm deployment deletion: "+confirmErr.Error())
				resultErr = errors.Join(resultErr, fmt.Errorf("confirm phase-zero deployment deletion: %w", confirmErr))
			} else {
				report.CleanupConfirmed = true
			}
		}
		observationCtx, cancelObservation := context.WithTimeout(context.Background(), 30*time.Second)
		released, releasedRequestID, releasedErr := r.API.ElasticContainersWithReleased(observationCtx, report.DeploymentUUID, true, 1, 100)
		cancelObservation()
		setRequestID(report.RequestIDs, "released_containers", releasedRequestID)
		if releasedErr != nil {
			report.ObservationWarnings = append(report.ObservationWarnings, "read released containers: "+releasedErr.Error())
		} else {
			report.ReleasedContainers = released.List
		}
		report.FinishedAt = r.Now().UTC()
	}()

	provisionTimeout := time.Duration(spec.ProvisionTimeoutSeconds) * time.Second
	if provisionTimeout <= 0 {
		provisionTimeout = 10 * time.Minute
	}
	pollCtx, cancel := context.WithTimeout(ctx, provisionTimeout+time.Duration(spec.MaxRuntimeSeconds)*time.Second)
	defer cancel()

	for {
		deployments, deploymentRequestID, err := r.API.ElasticDeployments(pollCtx, 1, 10, report.DeploymentUUID)
		setRequestID(report.RequestIDs, "deployment_last", deploymentRequestID)
		if err != nil {
			return report, fmt.Errorf("poll phase-zero deployment: %w", err)
		}
		containers, containerRequestID, containerErr := r.API.ElasticContainers(pollCtx, report.DeploymentUUID, 1, 100)
		setRequestID(report.RequestIDs, "containers_last", containerRequestID)
		if containerErr == nil {
			report.Containers = containers.List
		}
		events, eventRequestID, eventErr := r.API.ElasticEvents(pollCtx, report.DeploymentUUID, 1, 100, 0)
		setRequestID(report.RequestIDs, "events_last", eventRequestID)
		if eventErr == nil {
			report.Events = events.List
		}

		if len(deployments.List) > 0 {
			deployment := deployments.List[0]
			report.ProviderStatus = deployment.Status
			report.FinishedNum = deployment.FinishedNum
			report.FailedNum = deployment.FailedNum
			status := strings.ToLower(deployment.Status)
			if deployment.FailedNum > 0 || status == "failed" {
				report.TerminalStatus = "failed"
				return report, fmt.Errorf("phase-zero deployment failed")
			}
			if deployment.FinishedNum >= 1 {
				report.TerminalStatus = "finished"
				return report, nil
			}
			if status == "finished" || status == "completed" {
				report.TerminalStatus = status
				return report, nil
			}
			if status == "stopped" || status == "shutdown" {
				report.TerminalStatus = status
				return report, fmt.Errorf("phase-zero deployment stopped before the Job finished")
			}
		}
		if err := r.Sleep(pollCtx, r.PollInterval); err != nil {
			return report, fmt.Errorf("phase-zero deployment did not finish before deadline: %w", err)
		}
	}
}

func (r *JobRunner) recoverDeploymentByName(ctx context.Context, name string) (autodl.Deployment, string, error) {
	lastRequestID := ""
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		deployment, requestID, err := r.findDeploymentByName(ctx, name)
		if requestID != "" {
			lastRequestID = requestID
		}
		if err == nil && deployment.UUID != "" {
			return deployment, lastRequestID, nil
		}
		if err != nil {
			lastErr = err
		}
		if attempt < 5 {
			if err := r.Sleep(ctx, 2*time.Second); err != nil {
				return autodl.Deployment{}, lastRequestID, errors.Join(lastErr, err)
			}
		}
	}
	return autodl.Deployment{}, lastRequestID, lastErr
}

func (r *JobRunner) findDeploymentByName(ctx context.Context, name string) (autodl.Deployment, string, error) {
	lastRequestID := ""
	for page := 1; page <= 10; page++ {
		deployments, requestID, err := r.API.ElasticDeployments(ctx, page, 100, "")
		if requestID != "" {
			lastRequestID = requestID
		}
		if err != nil {
			return autodl.Deployment{}, lastRequestID, err
		}
		for _, deployment := range deployments.List {
			if deployment.Name == name {
				return deployment, lastRequestID, nil
			}
		}
		if deployments.MaxPage <= page || len(deployments.List) == 0 {
			break
		}
	}
	return autodl.Deployment{}, lastRequestID, nil
}

func (r *JobRunner) confirmDeleted(ctx context.Context, deploymentUUID string) (string, error) {
	lastRequestID := ""
	for {
		deployments, requestID, err := r.API.ElasticDeployments(ctx, 1, 10, deploymentUUID)
		if requestID != "" {
			lastRequestID = requestID
		}
		if err != nil {
			return lastRequestID, err
		}
		if len(deployments.List) == 0 {
			return lastRequestID, nil
		}
		if err := r.Sleep(ctx, 2*time.Second); err != nil {
			return lastRequestID, err
		}
	}
}

func (s JobSpec) BackendName() string {
	backend := strings.ToLower(strings.TrimSpace(s.Backend))
	if backend == "" {
		return "elastic"
	}
	return backend
}

func (s JobSpec) Validate() error {
	if len(s.GPUNames) == 0 || s.GPUNum < 1 || s.GPUNum > 4 {
		return fmt.Errorf("GPU names and a GPU count between 1 and 4 are required")
	}
	switch s.BackendName() {
	case "elastic":
		if strings.TrimSpace(s.Region) == "" {
			return fmt.Errorf("region is required for the Elastic backend")
		}
		if s.CUDAFrom <= 0 || s.CUDATo < s.CUDAFrom {
			return fmt.Errorf("invalid CUDA range")
		}
	case "private":
		if !validPrivateCUDAVersion(s.CUDAVersion) {
			return fmt.Errorf("invalid Private Cloud CUDA version")
		}
	default:
		return fmt.Errorf("unsupported phase-zero backend %q", s.BackendName())
	}
	if s.CPUFrom <= 0 || s.CPUTo < s.CPUFrom || s.MemoryFromGB <= 0 || s.MemoryToGB < s.MemoryFromGB {
		return fmt.Errorf("invalid CPU or memory range")
	}
	if s.PriceFromMilliPerHour < 0 || s.PriceToMilliPerHour <= 0 || s.PriceToMilliPerHour < s.PriceFromMilliPerHour {
		return fmt.Errorf("invalid price range")
	}
	if strings.TrimSpace(s.ImageUUID) == "" {
		return fmt.Errorf("image UUID is required")
	}
	if s.MaxRuntimeSeconds < 5 || s.MaxRuntimeSeconds > 600 {
		return fmt.Errorf("max runtime must be between 5 and 600 seconds")
	}
	if s.ProvisionTimeoutSeconds < 0 || s.ProvisionTimeoutSeconds > 1800 {
		return fmt.Errorf("provision timeout must be between 0 and 1800 seconds")
	}
	if !strings.HasPrefix(s.OutputRoot, "/root/autodl-fs/") || !safePath.MatchString(s.OutputRoot) {
		return fmt.Errorf("output root must be a safe path below /root/autodl-fs")
	}
	if _, err := s.estimatedMaximumSpendMilli(); err != nil {
		return err
	}
	return nil
}

func validPrivateCUDAVersion(version int) bool {
	switch version {
	case 111, 113, 116, 117, 118, 120, 122:
		return true
	default:
		return false
	}
}

func (s JobSpec) EstimatedMaximumSpendMilli() int64 {
	estimate, _ := s.estimatedMaximumSpendMilli()
	return estimate
}

func (s JobSpec) estimatedMaximumSpendMilli() (int64, error) {
	gpuNum := int64(s.GPUNum)
	runtimeSeconds := int64(s.MaxRuntimeSeconds)
	if gpuNum <= 0 || runtimeSeconds <= 0 || s.PriceToMilliPerHour > math.MaxInt64/gpuNum {
		return 0, fmt.Errorf("estimated maximum spend overflows int64")
	}
	numerator := s.PriceToMilliPerHour * gpuNum
	if numerator > math.MaxInt64/runtimeSeconds {
		return 0, fmt.Errorf("estimated maximum spend overflows int64")
	}
	numerator *= runtimeSeconds
	estimate := numerator / 3600
	if numerator%3600 != 0 {
		estimate++
	}
	if estimate < 10 {
		return 10, nil
	}
	return estimate, nil
}

func (s JobSpec) deployment(probeID, outputPath string) autodl.ElasticDeploymentCreate {
	return autodl.ElasticDeploymentCreate{
		Name:                probeID,
		DeploymentType:      "Job",
		ReplicaNum:          1,
		ParallelismNum:      1,
		ReuseContainer:      s.ReuseContainer,
		ReuseContainerScope: "all",
		ContainerTemplate: autodl.ElasticContainerTemplate{
			DCList:         []string{s.Region},
			CUDAFrom:       s.CUDAFrom,
			CUDATo:         s.CUDATo,
			GPUNames:       s.GPUNames,
			GPUNum:         s.GPUNum,
			MemoryFromGB:   s.MemoryFromGB,
			MemoryToGB:     s.MemoryToGB,
			CPUFrom:        s.CPUFrom,
			CPUTo:          s.CPUTo,
			PriceFromMilli: s.PriceFromMilliPerHour,
			PriceToMilli:   s.PriceToMilliPerHour,
			ImageUUID:      s.ImageUUID,
			Command:        s.probeCommand(probeID, outputPath),
		},
	}
}

func (s JobSpec) privateDeployment(probeID, outputPath string) autodl.PrivateElasticDeploymentCreate {
	return autodl.PrivateElasticDeploymentCreate{
		Name:           probeID,
		DeploymentType: "Job",
		ReplicaNum:     1,
		ParallelismNum: 1,
		ReuseContainer: s.ReuseContainer,
		ContainerTemplate: autodl.PrivateElasticContainerTemplate{
			CUDAVersion:    s.CUDAVersion,
			GPUNames:       s.GPUNames,
			GPUNum:         s.GPUNum,
			MemoryFromGB:   s.MemoryFromGB,
			MemoryToGB:     s.MemoryToGB,
			CPUFrom:        s.CPUFrom,
			CPUTo:          s.CPUTo,
			PriceFromMilli: s.PriceFromMilliPerHour,
			PriceToMilli:   s.PriceToMilliPerHour,
			ImageUUID:      s.ImageUUID,
			Command:        s.probeCommand(probeID, outputPath),
		},
	}
}

func (s JobSpec) probeCommand(probeID, outputPath string) string {
	if s.Minimal {
		script := fmt.Sprintf("set -eu; mkdir -p %s; printf 'gemcp-autodl-smoke-ok\\n' > %s/probe.log", shellQuote(outputPath), shellQuote(outputPath))
		return fmt.Sprintf("timeout --signal=TERM --kill-after=5s %ds /bin/sh -lc %s", s.MaxRuntimeSeconds, shellQuote(script))
	}
	script := fmt.Sprintf("set -eu; mkdir -p %s; { date -Iseconds; echo probe_id=%s; test -d /root/autodl-fs; nvidia-smi --query-gpu=name,memory.total --format=csv,noheader; } > %s/probe.log 2>&1", shellQuote(outputPath), shellQuote(probeID), shellQuote(outputPath))
	return fmt.Sprintf("timeout --signal=TERM --kill-after=5s %ds /bin/sh -lc %s", s.MaxRuntimeSeconds, shellQuote(script))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func isTerminalDeploymentStatus(status string) bool {
	switch strings.ToLower(status) {
	case "stopped", "finished", "completed", "failed", "shutdown":
		return true
	default:
		return false
	}
}
