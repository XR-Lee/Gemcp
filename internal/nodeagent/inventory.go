package nodeagent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"golang.org/x/sys/unix"
)

type DoctorReport struct {
	OperatingSystem string                 `json:"operating_system"`
	Architecture    string                 `json:"architecture"`
	DockerVersion   string                 `json:"docker_version,omitempty"`
	Inventory       nodeprotocol.Inventory `json:"inventory,omitempty"`
	Checks          map[string]string      `json:"checks"`
}

type InventoryCollector interface {
	Collect(context.Context, string, string, string) (DoctorReport, error)
}

type GPUActivityCollector interface {
	ExternalGPUProcesses(context.Context) (bool, error)
}

type SystemCollector struct{}

func (SystemCollector) ExternalGPUProcesses(ctx context.Context) (bool, error) {
	output, err := exec.CommandContext(ctx, "nvidia-smi", "--query-compute-apps=pid", "--format=csv,noheader,nounits").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("inspect NVIDIA compute processes: %s", boundedOutput(output))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		value := strings.TrimSpace(line)
		if value != "" && !strings.Contains(strings.ToLower(value), "no running") {
			return true, nil
		}
	}
	return false, nil
}

func (SystemCollector) Collect(ctx context.Context, installationID, storageRoot, version string) (DoctorReport, error) {
	report := DoctorReport{OperatingSystem: runtime.GOOS, Architecture: runtime.GOARCH, Checks: map[string]string{}}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return report, fmt.Errorf("the initial gemcp-node release requires linux/amd64")
	}
	report.Checks["platform"] = "ok"
	dockerOutput, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		return report, fmt.Errorf("Docker Engine check failed: %s", boundedOutput(dockerOutput))
	}
	report.DockerVersion = strings.TrimSpace(string(dockerOutput))
	report.Checks["docker"] = "ok"
	runtimeOutput, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{json .Runtimes}}").CombinedOutput()
	if err != nil || !strings.Contains(strings.ToLower(string(runtimeOutput)), "nvidia") {
		return report, fmt.Errorf("NVIDIA Container Toolkit is not registered with Docker")
	}
	report.Checks["nvidia_container_toolkit"] = "ok"
	gpus, err := collectGPUs(ctx)
	if err != nil {
		return report, err
	}
	if len(gpus) != 1 {
		return report, fmt.Errorf("the initial gemcp-node release requires exactly one NVIDIA GPU")
	}
	report.Checks["nvidia"] = "ok"
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return report, fmt.Errorf("read node hostname: %w", err)
	}
	memoryBytes, err := hostMemoryBytes()
	if err != nil {
		return report, err
	}
	storage, err := storageCapacity(storageRoot)
	if err != nil {
		return report, err
	}
	report.Checks["storage"] = "ok"
	machineID, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return report, fmt.Errorf("read /etc/machine-id: %w", err)
	}
	fingerprintInput := strings.Join([]string{strings.TrimSpace(string(machineID)), hostname, runtime.GOARCH, gpus[0].UUID}, "\x00")
	fingerprint := sha256.Sum256([]byte(fingerprintInput))
	report.Inventory = nodeprotocol.Inventory{
		InstallationID: installationID, MachineFingerprint: hex.EncodeToString(fingerprint[:]),
		Hostname: hostname, OperatingSystem: runtime.GOOS, Architecture: runtime.GOARCH,
		AgentVersion: version, ProtocolVersion: nodeprotocol.Version,
		CPUCount: runtime.NumCPU(), MemoryBytes: memoryBytes, GPUs: gpus, Storage: storage,
	}
	return report, nil
}

func collectGPUs(ctx context.Context) ([]nodeprotocol.GPU, error) {
	output, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=uuid,name,memory.total", "--format=csv,noheader,nounits").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("NVIDIA GPU check failed: %s", boundedOutput(output))
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	result := make([]nodeprotocol.GPU, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			return nil, fmt.Errorf("nvidia-smi returned an unexpected GPU row")
		}
		memoryMiB, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		if err != nil || memoryMiB <= 0 {
			return nil, fmt.Errorf("nvidia-smi returned invalid GPU memory")
		}
		result = append(result, nodeprotocol.GPU{
			UUID: strings.TrimSpace(parts[0]), Name: strings.TrimSpace(parts[1]), MemoryBytes: memoryMiB << 20,
		})
	}
	return result, nil
}

func hostMemoryBytes() (int64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read host memory: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kilobytes <= 0 {
			break
		}
		return kilobytes << 10, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan host memory: %w", err)
	}
	return 0, fmt.Errorf("host memory total is unavailable")
}

func storageCapacity(root string) (nodeprotocol.Storage, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return nodeprotocol.Storage{}, fmt.Errorf("managed storage root must be absolute")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nodeprotocol.Storage{}, fmt.Errorf("create managed storage root: %w", err)
	}
	var stats unix.Statfs_t
	if err := unix.Statfs(root, &stats); err != nil {
		return nodeprotocol.Storage{}, fmt.Errorf("inspect managed storage root: %w", err)
	}
	return nodeprotocol.Storage{
		Root: root, TotalBytes: int64(stats.Blocks) * int64(stats.Bsize), AvailableBytes: int64(stats.Bavail) * int64(stats.Bsize),
	}, nil
}

func boundedOutput(value []byte) string {
	const maximum = 512
	value = []byte(strings.TrimSpace(string(value)))
	if len(value) > maximum {
		value = value[len(value)-maximum:]
	}
	if len(value) == 0 {
		return "command did not return diagnostics"
	}
	return string(value)
}
