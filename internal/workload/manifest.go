package workload

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/XR-Lee/Gemcp/internal/workspacecatalog"
)

const (
	Version1          = 1
	maxManifestBytes  = 64 << 10
	maxWorkloads      = 32
	maxNameLength     = 64
	maxEntrypoint     = 16
	maxArguments      = 32
	maxParameters     = 16
	maxOutputs        = 8
	maxDatasets       = 8
	maxParameterBytes = 512
)

type Manifest struct {
	Version   int                 `yaml:"version"`
	Workloads map[string]Workload `yaml:"workloads"`
}

type Workload struct {
	Entrypoint       []string          `yaml:"entrypoint"`
	Arguments        []ArgumentMapping `yaml:"arguments,omitempty"`
	RuntimePreset    string            `yaml:"runtime_preset,omitempty"`
	Datasets         []string          `yaml:"datasets,omitempty"`
	Parameters       map[string]Param  `yaml:"parameters,omitempty"`
	Outputs          map[string]string `yaml:"outputs,omitempty"`
	WorkingDirectory string            `yaml:"working_directory,omitempty"`
}

type ArgumentMapping struct {
	Flag      string `yaml:"flag"`
	Parameter string `yaml:"parameter"`
}

type Param struct {
	Type          string `yaml:"type"`
	AllowedPrefix string `yaml:"allowed_prefix"`
	Required      bool   `yaml:"required"`
	Minimum       *int   `yaml:"minimum"`
	Maximum       *int   `yaml:"maximum"`
	Default       any    `yaml:"default"`
}

type Resolved struct {
	Name             string
	Argv             []string
	RuntimePreset    string
	Dataset          string
	Parameters       map[string]string
	WorkingDirectory string
}

func Parse(raw []byte) (Manifest, error) {
	var manifest Manifest
	if len(raw) == 0 || len(raw) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("gemcp.yaml must contain 1 to %d bytes", maxManifestBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse gemcp.yaml: %w", err)
	}
	if manifest.Version != Version1 {
		return Manifest{}, fmt.Errorf("gemcp.yaml version must be %d", Version1)
	}
	if len(manifest.Workloads) == 0 {
		return Manifest{}, fmt.Errorf("gemcp.yaml must define at least one workload")
	}
	if len(manifest.Workloads) > maxWorkloads {
		return Manifest{}, fmt.Errorf("gemcp.yaml may define at most %d workloads", maxWorkloads)
	}
	for name, workload := range manifest.Workloads {
		if err := validateWorkload(name, workload); err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

func Resolve(manifest Manifest, name string, values map[string]string) (Resolved, error) {
	name = strings.TrimSpace(name)
	workload, ok := manifest.Workloads[name]
	if !ok {
		return Resolved{}, fmt.Errorf("gemcp.yaml has no workload %q", name)
	}
	resolvedValues, err := resolveParameters(workload.Parameters, values)
	if err != nil {
		return Resolved{}, err
	}
	argv := append([]string{}, workload.Entrypoint...)
	for _, mapping := range workload.Arguments {
		value, present := resolvedValues[mapping.Parameter]
		if !present {
			continue
		}
		if mapping.Flag != "" {
			argv = append(argv, mapping.Flag)
		}
		argv = append(argv, value)
	}
	dataset := ""
	if len(workload.Datasets) > 0 {
		dataset = workload.Datasets[0]
	}
	return Resolved{
		Name:             name,
		Argv:             argv,
		RuntimePreset:    strings.TrimSpace(workload.RuntimePreset),
		Dataset:          dataset,
		Parameters:       resolvedValues,
		WorkingDirectory: strings.TrimSpace(workload.WorkingDirectory),
	}, nil
}

func validateWorkload(name string, workload Workload) error {
	if !validWorkloadName(name) {
		return fmt.Errorf("workload name %q is invalid", name)
	}
	if len(workload.Entrypoint) == 0 || len(workload.Entrypoint) > maxEntrypoint {
		return fmt.Errorf("workload %q entrypoint must contain 1 to %d arguments", name, maxEntrypoint)
	}
	for _, part := range workload.Entrypoint {
		if strings.TrimSpace(part) == "" || strings.ContainsRune(part, 0) {
			return fmt.Errorf("workload %q entrypoint is invalid", name)
		}
	}
	if len(workload.Arguments) > maxArguments {
		return fmt.Errorf("workload %q may declare at most %d argument mappings", name, maxArguments)
	}
	if len(workload.Parameters) > maxParameters {
		return fmt.Errorf("workload %q may declare at most %d parameters", name, maxParameters)
	}
	if len(workload.Datasets) > maxDatasets {
		return fmt.Errorf("workload %q may declare at most %d datasets", name, maxDatasets)
	}
	if len(workload.Outputs) > maxOutputs {
		return fmt.Errorf("workload %q may declare at most %d outputs", name, maxOutputs)
	}
	if preset := strings.TrimSpace(workload.RuntimePreset); preset != "" {
		switch strings.ToLower(preset) {
		case "smoke", "probe", "train":
		default:
			return fmt.Errorf("workload %q runtime_preset must be smoke, probe, or train", name)
		}
	}
	if dir := strings.TrimSpace(workload.WorkingDirectory); dir != "" {
		if _, err := workspacecatalog.NormalizeRelativePath(dir); err != nil {
			return fmt.Errorf("workload %q working_directory is unsafe", name)
		}
	}
	for _, dataset := range workload.Datasets {
		if strings.TrimSpace(dataset) == "" || len(dataset) > maxNameLength {
			return fmt.Errorf("workload %q dataset name is invalid", name)
		}
	}
	parameters := workload.Parameters
	if parameters == nil {
		parameters = map[string]Param{}
	}
	seen := map[string]bool{}
	for _, mapping := range workload.Arguments {
		parameter := strings.TrimSpace(mapping.Parameter)
		if parameter == "" || !validWorkloadName(parameter) {
			return fmt.Errorf("workload %q argument mapping is missing a parameter", name)
		}
		if _, ok := parameters[parameter]; !ok {
			return fmt.Errorf("workload %q argument maps unknown parameter %q", name, parameter)
		}
		if seen[parameter] {
			return fmt.Errorf("workload %q maps parameter %q more than once", name, parameter)
		}
		seen[parameter] = true
		if flag := strings.TrimSpace(mapping.Flag); flag != "" && !validFlag(flag) {
			return fmt.Errorf("workload %q flag %q is invalid", name, flag)
		}
	}
	for parameter, spec := range parameters {
		if !validWorkloadName(parameter) {
			return fmt.Errorf("workload %q parameter %q is invalid", name, parameter)
		}
		switch spec.Type {
		case "integer", "config_path":
		default:
			return fmt.Errorf("workload %q parameter %q has unsupported type %q", name, parameter, spec.Type)
		}
		if spec.Type == "config_path" {
			prefix := strings.TrimSuffix(strings.TrimSpace(spec.AllowedPrefix), "/")
			if prefix == "" {
				return fmt.Errorf("workload %q parameter %q requires allowed_prefix", name, parameter)
			}
			if _, err := workspacecatalog.NormalizeRelativePath(prefix); err != nil {
				return fmt.Errorf("workload %q parameter %q allowed_prefix is unsafe", name, parameter)
			}
		}
	}
	return nil
}

func resolveParameters(specs map[string]Param, values map[string]string) (map[string]string, error) {
	if specs == nil {
		specs = map[string]Param{}
	}
	result := map[string]string{}
	for key := range values {
		if _, ok := specs[key]; !ok {
			return nil, fmt.Errorf("unknown workload parameter %q", key)
		}
	}
	for name, spec := range specs {
		raw, present := values[name]
		raw = strings.TrimSpace(raw)
		if raw == "" {
			present = false
		}
		if !present {
			if spec.Default != nil {
				raw = fmt.Sprint(spec.Default)
				present = true
			} else if spec.Required {
				return nil, fmt.Errorf("workload parameter %q is required", name)
			} else {
				continue
			}
		}
		value, err := coerceParameter(name, spec, raw)
		if err != nil {
			return nil, err
		}
		result[name] = value
	}
	return result, nil
}

func coerceParameter(name string, spec Param, raw string) (string, error) {
	if len(raw) > maxParameterBytes {
		return "", fmt.Errorf("workload parameter %q exceeds %d bytes", name, maxParameterBytes)
	}
	for _, character := range raw {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("workload parameter %q must not contain control characters", name)
		}
	}
	switch spec.Type {
	case "integer":
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return "", fmt.Errorf("workload parameter %q must be an integer", name)
		}
		if spec.Minimum != nil && parsed < *spec.Minimum {
			return "", fmt.Errorf("workload parameter %q is below the minimum", name)
		}
		if spec.Maximum != nil && parsed > *spec.Maximum {
			return "", fmt.Errorf("workload parameter %q is above the maximum", name)
		}
		return strconv.Itoa(parsed), nil
	case "config_path":
		normalized, err := workspacecatalog.NormalizeRelativePath(raw)
		if err != nil {
			return "", fmt.Errorf("workload parameter %q must be a safe relative path", name)
		}
		prefix := strings.TrimSpace(spec.AllowedPrefix)
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix != "" && normalized != prefix && !strings.HasPrefix(normalized, prefix+"/") {
			return "", fmt.Errorf("workload parameter %q must stay under %s", name, prefix)
		}
		return normalized, nil
	default:
		return "", fmt.Errorf("workload parameter %q has unsupported type", name)
	}
}

func ValidName(value string) bool {
	return validWorkloadName(value)
}

func validWorkloadName(value string) bool {
	if value == "" || len(value) > maxNameLength {
		return false
	}
	for index, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '-' || character == '_':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validFlag(value string) bool {
	if !strings.HasPrefix(value, "-") || len(value) > 64 {
		return false
	}
	for _, character := range value[1:] {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-' || character == '_':
		default:
			return false
		}
	}
	return true
}
