package executionmeta

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
)

const MaxGPUDevices = 16

var (
	gpuUUIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	visiblePattern = regexp.MustCompile(`^[A-Za-z0-9,._:-]{0,255}$`)
	validSources   = map[string]struct{}{"runner_observed": {}, "node_binding": {}}
)

type GPUDevice struct {
	Index int    `json:"index"`
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
}

type RuntimeInfo struct {
	Source             string      `json:"source"`
	WorkingDirectory   string      `json:"working_directory"`
	OutputDirectory    string      `json:"output_directory"`
	CUDAVisibleDevices string      `json:"cuda_visible_devices,omitempty"`
	GPUDevices         []GPUDevice `json:"gpu_devices"`
}

func Validate(input RuntimeInfo, expectedOutputs ...string) (RuntimeInfo, error) {
	result := RuntimeInfo{
		Source: strings.TrimSpace(input.Source), WorkingDirectory: strings.TrimSpace(input.WorkingDirectory),
		OutputDirectory: strings.TrimSpace(input.OutputDirectory), CUDAVisibleDevices: strings.TrimSpace(input.CUDAVisibleDevices),
		GPUDevices: append([]GPUDevice(nil), input.GPUDevices...),
	}
	if _, ok := validSources[result.Source]; !ok {
		return RuntimeInfo{}, fmt.Errorf("runtime observation source is invalid")
	}
	if !safeAbsolutePath(result.WorkingDirectory) || !safeAbsolutePath(result.OutputDirectory) {
		return RuntimeInfo{}, fmt.Errorf("runtime observation paths are invalid")
	}
	if len(expectedOutputs) > 0 {
		matched := false
		for _, candidate := range expectedOutputs {
			if result.OutputDirectory == candidate {
				matched = true
				break
			}
		}
		if !matched {
			return RuntimeInfo{}, fmt.Errorf("runtime output directory does not match the execution specification")
		}
	}
	if !visiblePattern.MatchString(result.CUDAVisibleDevices) {
		return RuntimeInfo{}, fmt.Errorf("CUDA_VISIBLE_DEVICES observation is invalid")
	}
	if len(result.GPUDevices) > MaxGPUDevices {
		return RuntimeInfo{}, fmt.Errorf("runtime observation has too many GPU devices")
	}
	seen := map[string]struct{}{}
	for index := range result.GPUDevices {
		device := &result.GPUDevices[index]
		device.UUID = strings.TrimSpace(device.UUID)
		device.Name = strings.TrimSpace(device.Name)
		if device.Index < 0 || device.Index >= MaxGPUDevices || !gpuUUIDPattern.MatchString(device.UUID) || !safeText(device.Name, 120) {
			return RuntimeInfo{}, fmt.Errorf("runtime GPU observation is invalid")
		}
		if _, exists := seen[device.UUID]; exists {
			return RuntimeInfo{}, fmt.Errorf("runtime GPU observation contains a duplicate device")
		}
		seen[device.UUID] = struct{}{}
	}
	return result, nil
}

func safeAbsolutePath(value string) bool {
	if len(value) > 1024 || !strings.HasPrefix(value, "/") || !safeText(value, 1024) {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value || (cleaned != "/" && cleaned+"/" == value)
}

func safeText(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
