package nodeagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRuntimeUsesFixedIsolationBoundary(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "docker-args.log")
	binary := filepath.Join(directory, "docker")
	image := "registry.example/train@sha256:" + strings.Repeat("c", 64)
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$GEMCP_TEST_DOCKER_LOG"
case "$1" in
  image) printf '["` + image + `"]\n' ;;
  create) printf 'container-id\n' ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEMCP_TEST_DOCKER_LOG", logPath)
	runtime := DockerRuntime{Binary: binary}
	_, err := runtime.Start(context.Background(), ContainerSpec{
		AssignmentID: "11111111-2222-4333-8444-555555555555", Image: image, Command: "python train.py",
		GPUUUID: "GPU-test", CPULimit: 8, MemoryLimitBytes: 32 << 30,
		SourcePath: directory + "/source", OutputPath: directory + "/output",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := os.ReadFile(logPath)
	args := string(payload)
	for _, required := range []string{
		"--runtime\nnvidia", "--gpus\ndevice=GPU-test", "--network\nbridge", "--ipc\nprivate",
		"--cap-drop\nALL", "--security-opt\nno-new-privileges", "--read-only", "--pids-limit\n4096",
	} {
		if !strings.Contains(args, required) {
			t.Fatalf("Docker arguments do not contain %q:\n%s", required, args)
		}
	}
	for _, forbidden := range []string{"--privileged", "--network=host", "docker.sock", "gmn_", "GEMCP_RUNNER_TOKEN"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("Docker arguments contain forbidden value %q:\n%s", forbidden, args)
		}
	}
}

func TestDockerRuntimeLaunchesArgvWithoutShell(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "docker-argv.log")
	binary := filepath.Join(directory, "docker")
	image := "registry.example/train@sha256:" + strings.Repeat("d", 64)
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$GEMCP_TEST_DOCKER_LOG"
case "$1" in
  image) printf '["` + image + `"]\n' ;;
  create) printf 'container-id\n' ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEMCP_TEST_DOCKER_LOG", logPath)
	runtime := DockerRuntime{Binary: binary}
	_, err := runtime.Start(context.Background(), ContainerSpec{
		AssignmentID: "11111111-2222-4333-8444-666666666666", Image: image, ExecutionMode: "argv",
		Argv: []string{"python", "train.py", "--label", "value with spaces"}, GPUUUID: "GPU-test",
		CPULimit: 8, MemoryLimitBytes: 32 << 30, SourcePath: directory + "/source", OutputPath: directory + "/output",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := os.ReadFile(logPath)
	args := string(payload)
	if !strings.Contains(args, "--entrypoint\npython\n"+image+"\ntrain.py\n--label\nvalue with spaces") {
		t.Fatalf("Docker argv was not preserved:\n%s", args)
	}
	if strings.Contains(args, "/bin/sh\n-lc") {
		t.Fatalf("Docker argv used a shell:\n%s", args)
	}
}
