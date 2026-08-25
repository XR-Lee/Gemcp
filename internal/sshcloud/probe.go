package sshcloud

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var nvidiaGPUUUID = regexp.MustCompile(`^GPU-[A-Za-z0-9-]{1,116}$`)

func (s *Service) probeConn(ctx context.Context, conn Conn) (Inventory, error) {
	logger := probeLoggerFrom(ctx)
	logger.step(ctx, StepOS, "Checking remote identity")
	uname, err := conn.Run(ctx, "uname -s -m", 4<<10)
	if err != nil {
		return Inventory{}, fmt.Errorf("probe uname: %w", err)
	}
	fields := strings.Fields(uname)
	osName, arch := "", ""
	if len(fields) > 0 {
		osName = fields[0]
	}
	if len(fields) > 1 {
		arch = fields[1]
	}
	if osName != "" {
		logger.step(ctx, StepOS, "Remote host is "+strings.TrimSpace(osName+" "+arch))
	}
	hint := collectHostHint(ctx, conn)
	if hint.Hostname != "" || hint.PID1 != "" {
		logger.step(ctx, StepOS, "Remote identity "+strings.TrimSpace(strings.Join([]string{hint.Hostname, "pid1=" + hint.PID1}, " ")))
	}
	gpus, _ := inspectVisibleGPU(ctx, conn)
	logger.step(ctx, StepInventory, "Recording optional GPU inventory")
	inventory := Inventory{
		OS: osName, Arch: arch, NvidiaReady: len(gpus) > 0, GPUs: gpus, HostKind: hint.hostKind(),
	}
	if nproc, nprocErr := conn.Run(ctx, "nproc", 1024); nprocErr == nil {
		if count, parseErr := strconv.Atoi(strings.TrimSpace(nproc)); parseErr == nil {
			inventory.CPUCount = count
		}
	}
	if mem, memErr := conn.Run(ctx, "awk '/MemTotal/ {print $2}' /proc/meminfo", 1024); memErr == nil {
		if kib, parseErr := strconv.ParseInt(strings.TrimSpace(mem), 10, 64); parseErr == nil {
			inventory.MemoryBytes = kib * 1024
		}
	}
	return inventory, nil
}

func (s *Service) openNode(ctx context.Context, node Target, credential Credential, expected string) (Conn, string, error) {
	dial := s.dial
	if dial == nil {
		dial = Dial
	}
	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	conn, fingerprint, err := dial(probeCtx, node, credential, expected)
	if err != nil {
		if err == ErrHostKeyChanged {
			return nil, fingerprint, ErrHostKeyChanged
		}
		return nil, fingerprint, err
	}
	return conn, fingerprint, nil
}

type hostHint struct {
	Hostname        string
	PID1            string
	Systemd         string
	Nvidia0         string
	AutoDLContainer bool
}

func collectHostHint(ctx context.Context, conn Conn) hostHint {
	output, _ := conn.Run(ctx, "sh -lc "+shellQuote(hostHintScript), 4<<10)
	return parseHostHint(output)
}

func parseHostHint(raw string) hostHint {
	hint := hostHint{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "hostname":
			hint.Hostname = value
			hint.AutoDLContainer = strings.Contains(strings.ToLower(value), "autodl-container")
		case "pid1":
			hint.PID1 = value
		case "systemd":
			hint.Systemd = value
		case "nvidia0":
			hint.Nvidia0 = value
		}
	}
	return hint
}

func (h hostHint) containerWithoutSystemd() bool {
	return h.Systemd == "no" || h.AutoDLContainer || strings.Contains(strings.ToLower(h.Hostname), "autodl-container")
}

func (h hostHint) hostKind() string {
	if h.containerWithoutSystemd() {
		return "autodl_container"
	}
	return "host"
}

func (h hostHint) dockerNetwork() string {
	if h.containerWithoutSystemd() {
		return "none"
	}
	return "bridge"
}

func (h hostHint) storageDriver() string {
	if h.containerWithoutSystemd() {
		return "vfs"
	}
	return ""
}

func inspectVisibleGPU(ctx context.Context, conn Conn) ([]GPU, error) {
	logger := probeLoggerFrom(ctx)
	logger.step(ctx, StepNvidiaDriver, "Checking nvidia-smi for a visible GPU")
	queryCtx, cancel := context.WithTimeout(withStreamOutput(ctx), 30*time.Second)
	defer cancel()
	output, err := conn.Run(queryCtx, nvidiaSMIQuery, 16<<10)
	if strings.TrimSpace(output) != "" {
		logger.output(queryCtx, strings.TrimSpace(output)+"\n")
	}
	if err != nil || strings.TrimSpace(output) == "" || strings.Contains(strings.ToLower(output), "no devices were found") {
		logger.step(ctx, StepNvidiaDriver, "nvidia-smi listed no GPU; observer continues")
		return nil, nil
	}
	gpus, parseErr := parseNVIDIASMI(output)
	if parseErr != nil {
		logger.step(ctx, StepNvidiaDriver, "nvidia-smi output was not used: "+parseErr.Error())
		return nil, nil
	}
	logger.step(ctx, StepNvidiaDriver, "NVIDIA GPU "+gpus[0].Name+" is visible")
	return gpus, nil
}

const nvidiaSMIQuery = "nvidia-smi --query-gpu=name,uuid,memory.total --format=csv,noheader,nounits"

const hostHintScript = `
echo gemcp-host-hint
echo hostname=$(hostname 2>/dev/null || uname -n)
echo pid1=$(ps -p 1 -o comm= 2>/dev/null | tr -d ' ')
echo systemd=$(test -d /run/systemd/system && echo yes || echo no)
echo nvidia0=$(test -e /dev/nvidia0 && echo yes || echo no)
`

func parseNVIDIASMI(raw string) ([]GPU, error) {
	lines := strings.Split(raw, "\n")
	gpus := make([]GPU, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := splitCSV(line)
		if len(parts) < 3 {
			return nil, invalid("nvidia-smi inventory is invalid")
		}
		name := strings.TrimSpace(parts[0])
		uuid := strings.TrimSpace(parts[1])
		memoryMiB, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		if name == "" || !nvidiaGPUUUID.MatchString(uuid) || err != nil || memoryMiB <= 0 {
			return nil, invalid("nvidia-smi inventory is invalid")
		}
		gpus = append(gpus, GPU{Name: name, UUID: uuid, MemoryBytes: memoryMiB << 20})
	}
	return gpus, nil
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		result = append(result, strings.TrimSpace(part))
	}
	return result
}

func linuxAMD64(arch string) bool {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64", "amd64":
		return true
	default:
		return false
	}
}

func inventoryMap(inventory Inventory) map[string]any {
	gpus := make([]any, 0, len(inventory.GPUs))
	for _, gpu := range inventory.GPUs {
		gpus = append(gpus, map[string]any{"name": gpu.Name, "uuid": gpu.UUID, "memory_bytes": gpu.MemoryBytes})
	}
	result := map[string]any{
		"os": inventory.OS, "arch": inventory.Arch, "docker_version": inventory.DockerVersion,
		"nvidia_ready": inventory.NvidiaReady, "gpus": gpus, "cpu_count": inventory.CPUCount, "memory_bytes": inventory.MemoryBytes,
		"probed_at": time.Now().UTC().Format(time.RFC3339),
	}
	if inventory.HostKind != "" {
		result["host_kind"] = inventory.HostKind
	}
	if inventory.DockerNetwork != "" {
		result["docker_network"] = inventory.DockerNetwork
	}
	if inventory.StorageDriver != "" {
		result["storage_driver"] = inventory.StorageDriver
	}
	return result
}

func inventoryFromMap(raw map[string]any) Inventory {
	var inventory Inventory
	inventory.OS, _ = raw["os"].(string)
	inventory.Arch, _ = raw["arch"].(string)
	inventory.DockerVersion, _ = raw["docker_version"].(string)
	inventory.NvidiaReady, _ = raw["nvidia_ready"].(bool)
	inventory.HostKind, _ = raw["host_kind"].(string)
	inventory.DockerNetwork, _ = raw["docker_network"].(string)
	inventory.StorageDriver, _ = raw["storage_driver"].(string)
	inventory.CPUCount = anyInt(raw["cpu_count"])
	inventory.MemoryBytes = anyInt64(raw["memory_bytes"])
	switch values := raw["gpus"].(type) {
	case []any:
		for _, value := range values {
			gpu, ok := value.(map[string]any)
			if !ok {
				continue
			}
			name, _ := gpu["name"].(string)
			uuid, _ := gpu["uuid"].(string)
			inventory.GPUs = append(inventory.GPUs, GPU{Name: name, UUID: uuid, MemoryBytes: anyInt64(gpu["memory_bytes"])})
		}
	}
	return inventory
}

func inventoryGPUNames(raw map[string]any) []string {
	inventory := inventoryFromMap(raw)
	names := make([]string, 0, len(inventory.GPUs))
	for _, gpu := range inventory.GPUs {
		if strings.TrimSpace(gpu.Name) != "" {
			names = append(names, gpu.Name)
		}
	}
	return names
}

func inventoryGPUUUID(raw map[string]any) string {
	inventory := inventoryFromMap(raw)
	if len(inventory.GPUs) == 0 {
		return ""
	}
	return inventory.GPUs[0].UUID
}

func inventoryDockerNetwork(raw map[string]any) string {
	if inventoryFromMap(raw).DockerNetwork == "none" {
		return "none"
	}
	return "bridge"
}

func anyInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

func anyInt64(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	default:
		return 0
	}
}
