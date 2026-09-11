package workload

import (
	"strings"
	"testing"
)

const sampleManifest = `version: 1
workloads:
  objbg-smoke:
    entrypoint:
      - python
      - tools/gemcp_pointmamba_run.py
    arguments:
      - flag: --config
        parameter: config
      - flag: --seed
        parameter: seed
      - flag: --epochs
        parameter: epochs
    runtime_preset: smoke
    datasets:
      - scanobjectnn-objbg
    parameters:
      config:
        type: config_path
        allowed_prefix: cfgs/
        required: true
      seed:
        type: integer
        minimum: 0
        default: 2
      epochs:
        type: integer
        minimum: 1
        maximum: 30
    outputs:
      metrics: metrics.json
`

func TestParseAndResolveSampleWorkload(t *testing.T) {
	manifest, err := Parse([]byte(sampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(manifest, "objbg-smoke", map[string]string{"config": "cfgs/scanobjectnn.yaml", "epochs": "3"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"python", "tools/gemcp_pointmamba_run.py", "--config", "cfgs/scanobjectnn.yaml", "--seed", "2", "--epochs", "3"}
	if strings.Join(resolved.Argv, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %#v, want %#v", resolved.Argv, want)
	}
	if resolved.RuntimePreset != "smoke" || resolved.Dataset != "scanobjectnn-objbg" || resolved.Parameters["seed"] != "2" {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveOmitsOptionalParameterAndFlag(t *testing.T) {
	manifest, err := Parse([]byte(sampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(manifest, "objbg-smoke", map[string]string{"config": "cfgs/scanobjectnn.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(resolved.Argv, " ")
	if strings.Contains(joined, "--epochs") || !strings.Contains(joined, "--seed 2") {
		t.Fatalf("optional omit = %#v", resolved.Argv)
	}
}

func TestParseRejectsUnknownFieldsAndUnsafePaths(t *testing.T) {
	if _, err := Parse([]byte("version: 1\nworkloads:\n  a:\n    entrypoint: [python]\n    extra: true\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := Resolve(mustParse(t, sampleManifest), "objbg-smoke", map[string]string{"config": "../secret.yaml"}); err == nil {
		t.Fatal("path escape accepted")
	}
	if _, err := Resolve(mustParse(t, sampleManifest), "objbg-smoke", map[string]string{"config": "other/scan.yaml"}); err == nil {
		t.Fatal("prefix escape accepted")
	}
	if _, err := Resolve(mustParse(t, sampleManifest), "missing", nil); err == nil {
		t.Fatal("unknown workload accepted")
	}
}

func mustParse(t *testing.T, raw string) Manifest {
	t.Helper()
	manifest, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
