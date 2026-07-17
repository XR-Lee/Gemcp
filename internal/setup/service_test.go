package setup

import (
	"testing"

	"github.com/XR-Lee/Gemcp/internal/secrets"
)

func validInput() Input {
	return Input{
		OrganizationName: "Research Lab",
		Owner:            OwnerInput{Email: "owner@example.com", Password: "correct horse battery staple"},
		Provider:         ProviderInput{Name: "AutoDL", BaseURL: "https://api.autodl.com", Token: "provider-token"},
		Project: ProjectInput{
			Name: "First Project", Slug: "first-project",
			MonthlyBudgetMilli: 100_000, MaxExperimentMilli: 20_000,
			MaxConcurrency: 1, MaxRuntimeSeconds: 3600,
			TimeoutExtensionSeconds: 3600, TerminationGraceSeconds: 60,
		},
		Environment: EnvironmentInput{Name: "default", ImageUUID: "image-test"},
		ResourceProfile: ResourceProfileInput{
			Name: "default", Region: "westDC2", GPUNames: []string{"RTX 4090"}, GPUNum: 1,
			CUDAFrom: 118, CUDATo: 128, CPUFrom: 1, CPUTo: 128,
			MemoryFromGB: 1, MemoryToGB: 512, PriceFromMilli: 10, PriceToMilli: 3000,
		},
	}
}

func TestValidateInput(t *testing.T) {
	if err := validateInput(validInput()); err != nil {
		t.Fatalf("validateInput() error = %v", err)
	}
}

func TestValidateInputAcceptsPrivateCloud(t *testing.T) {
	input := validInput()
	input.Provider.BaseURL = "https://private.autodl.com"
	input.ResourceProfile.Region = "private"
	input.ResourceProfile.GPUNames = []string{"NVIDIA GeForce RTX 3090"}
	input.ResourceProfile.CUDATo = input.ResourceProfile.CUDAFrom
	if err := validateInput(input); err != nil {
		t.Fatalf("validateInput() error = %v", err)
	}
}

func TestValidateInputRejectsUnknownProviderHost(t *testing.T) {
	input := validInput()
	input.Provider.BaseURL = "https://autodl.attacker.example"
	if err := validateInput(input); err == nil {
		t.Fatal("validateInput() accepted an unknown Provider host")
	}
}

func TestValidateInputRejectsBudgetInversion(t *testing.T) {
	input := validInput()
	input.Project.MaxExperimentMilli = input.Project.MonthlyBudgetMilli + 1
	if err := validateInput(input); err == nil {
		t.Fatal("validateInput() accepted inverted budgets")
	}
}

func TestProviderTokenAADRoundTrip(t *testing.T) {
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	ciphertext, err := box.Encrypt([]byte("provider-token"), providerTokenAAD)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	plaintext, err := box.Decrypt(ciphertext, providerTokenAAD)
	if err != nil || string(plaintext) != "provider-token" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
}
