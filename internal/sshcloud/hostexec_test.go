package sshcloud

import (
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/executioncmd"
)

func TestHostStartScriptExportsDatasetBindings(t *testing.T) {
	script := hostStartScript(remoteWorkload{
		AssignmentID:  "11111111-1111-1111-1111-111111111111",
		ExecutionMode: executioncmd.ModeArgv,
		Argv:          []string{"python", "train.py"},
		WorkingDir:    "/opt/exp",
		RemoteDir:     "/var/tmp/gemcp/assignment",
		DatasetEnv: []datasetEnv{{
			Name: "GEMCP_DATASET_SCANOBJECTNN_OBJBG", Value: "/root/autodl-fs/datasets/ScanObjectNN",
		}},
	})
	if !strings.Contains(script, `export GEMCP_OUTPUT_DIR="$dir/outputs"`) {
		t.Fatalf("missing output dir export: %s", script)
	}
	if !strings.Contains(script, "export GEMCP_DATASET_SCANOBJECTNN_OBJBG='/root/autodl-fs/datasets/ScanObjectNN'") {
		t.Fatalf("missing dataset export: %s", script)
	}
	if err := validateHostSpec(remoteWorkload{
		AssignmentID: "11111111-1111-1111-1111-111111111111", ExecutionMode: executioncmd.ModeArgv,
		Argv: []string{"python"}, RemoteDir: "/var/tmp/gemcp/assignment",
		DatasetEnv: []datasetEnv{{Name: "PATH", Value: "/tmp"}},
	}); err == nil {
		t.Fatal("expected invalid dataset environment variable")
	}
}

func TestSnapshotDatasetEnvIgnoresUnsafeBindings(t *testing.T) {
	envs := snapshotDatasetEnv(map[string]any{
		"dataset_bindings": []map[string]any{
			{"environment_variable": "GEMCP_DATASET_SCANOBJECTNN_OBJBG", "canonical_root": "/root/data/ScanObjectNN"},
			{"environment_variable": "LD_PRELOAD", "canonical_root": "/tmp/evil"},
			{"environment_variable": "GEMCP_DATASET_BAD", "canonical_root": "relative"},
		},
	})
	if len(envs) != 1 || envs[0].Name != "GEMCP_DATASET_SCANOBJECTNN_OBJBG" || envs[0].Value != "/root/data/ScanObjectNN" {
		t.Fatalf("snapshotDatasetEnv() = %+v", envs)
	}
}
