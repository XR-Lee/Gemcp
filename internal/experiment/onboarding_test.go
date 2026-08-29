package experiment

import "testing"

func TestLocalCPUOnboardingWhenSSHEnabledAndNoAutoDL(t *testing.T) {
	got := executionOnboarding(ProjectOptions{}, true)
	if got == nil || got.LocalCPU == nil || len(got.LocalCPU.NextSteps) < 3 {
		t.Fatalf("local CPU onboarding = %+v", got)
	}
	if got.PublicCloud.NextSteps[0].Tool != "register_ssh_cloud_node" {
		t.Fatalf("first step = %+v", got.PublicCloud.NextSteps[0])
	}
	if got.LocalCPU.NextSteps[1].Tool != "register_environment" || got.LocalCPU.NextSteps[1].Example["image_uuid"] != "host" {
		t.Fatalf("environment step = %+v", got.LocalCPU.NextSteps[1])
	}
	if got.LocalCPU.NextSteps[2].Tool != "register_dataset_binding" || got.LocalCPU.NextSteps[2].Example["catalog"] != "modelnet40-mini" {
		t.Fatalf("dataset step = %+v", got.LocalCPU.NextSteps[2])
	}
}

func TestPublicCloudOnboardingWhenAutoDLEnvironmentExists(t *testing.T) {
	got := executionOnboarding(ProjectOptions{
		Environments: []EnvironmentOption{{Backend: "autodl_elastic", ImageUUID: "image-uuid"}},
	}, true)
	if got == nil || got.LocalCPU != nil || got.PublicCloud.NextSteps[0].Tool != "register_dataset_binding" {
		t.Fatalf("public-cloud onboarding = %+v", got)
	}
}
