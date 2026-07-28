package executionmeta

import "testing"

func TestValidateRuntimeInfo(t *testing.T) {
	input := RuntimeInfo{
		Source: "runner_observed", WorkingDirectory: "/tmp/gemcp-attempt/source", OutputDirectory: "/outputs/run",
		CUDAVisibleDevices: "0", GPUDevices: []GPUDevice{{Index: 0, UUID: "GPU-1234", Name: "NVIDIA RTX 3090"}},
	}
	result, err := Validate(input, "/outputs/run")
	if err != nil || result.GPUDevices[0].UUID != "GPU-1234" {
		t.Fatalf("Validate() = %+v, %v", result, err)
	}
	for _, invalid := range []RuntimeInfo{
		{Source: "unknown", WorkingDirectory: "/workspace", OutputDirectory: "/outputs"},
		{Source: "node_binding", WorkingDirectory: "relative", OutputDirectory: "/outputs"},
		{Source: "node_binding", WorkingDirectory: "/workspace", OutputDirectory: "/wrong"},
		{Source: "node_binding", WorkingDirectory: "/workspace", OutputDirectory: "/outputs", GPUDevices: []GPUDevice{{UUID: "bad value", Name: "GPU"}}},
	} {
		if _, err := Validate(invalid, "/outputs"); err == nil {
			t.Fatalf("invalid runtime info accepted: %+v", invalid)
		}
	}
}
