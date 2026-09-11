package workload

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type DraftInput struct {
	Name             string
	Argv             []string
	RuntimePreset    string
	Dataset          string
	WorkingDirectory string
}

func DraftFromArgv(input DraftInput) (Manifest, []byte, error) {
	name := strings.TrimSpace(input.Name)
	if !ValidName(name) {
		return Manifest{}, nil, fmt.Errorf("workload name %q is invalid", name)
	}
	item := Workload{Entrypoint: append([]string{}, input.Argv...)}
	if preset := strings.ToLower(strings.TrimSpace(input.RuntimePreset)); preset != "" {
		item.RuntimePreset = preset
	}
	if dataset := strings.TrimSpace(input.Dataset); dataset != "" {
		item.Datasets = []string{dataset}
	}
	if cwd := strings.TrimSpace(input.WorkingDirectory); cwd != "" {
		item.WorkingDirectory = cwd
	}
	if err := validateWorkload(name, item); err != nil {
		return Manifest{}, nil, err
	}
	manifest := Manifest{
		Version:   Version1,
		Workloads: map[string]Workload{name: item},
	}
	raw, err := Encode(manifest)
	if err != nil {
		return Manifest{}, nil, err
	}
	parsed, err := Parse(raw)
	if err != nil {
		return Manifest{}, nil, err
	}
	return parsed, raw, nil
}

func Encode(manifest Manifest) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(manifest); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
