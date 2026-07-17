package phasezero

import (
	"math"
	"testing"
)

func validJobSpec() JobSpec {
	return JobSpec{
		Region:                  "westDC2",
		GPUNames:                []string{"RTX 4090"},
		GPUNum:                  1,
		CUDAFrom:                118,
		CUDATo:                  128,
		CPUFrom:                 1,
		CPUTo:                   64,
		MemoryFromGB:            1,
		MemoryToGB:              256,
		PriceFromMilliPerHour:   10,
		PriceToMilliPerHour:     2000,
		ImageUUID:               "image-test",
		MaxRuntimeSeconds:       60,
		ProvisionTimeoutSeconds: 300,
		OutputRoot:              "/root/autodl-fs/gemcp-phase0",
	}
}

func TestJobSpecEstimate(t *testing.T) {
	spec := validJobSpec()
	if got, want := spec.EstimatedMaximumSpendMilli(), int64(34); got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
}

func TestJobSpecMinimumCharge(t *testing.T) {
	spec := validJobSpec()
	spec.PriceToMilliPerHour = 100
	spec.MaxRuntimeSeconds = 5
	if got, want := spec.EstimatedMaximumSpendMilli(), int64(10); got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
}

func TestPrivateJobSpecValidation(t *testing.T) {
	spec := validJobSpec()
	spec.Backend = "private"
	spec.Region = ""
	spec.CUDAFrom = 0
	spec.CUDATo = 0
	spec.CUDAVersion = 118
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	spec.CUDAVersion = 115
	if err := spec.Validate(); err == nil {
		t.Fatal("Validate() accepted an unsupported Private Cloud CUDA version")
	}
}

func TestJobSpecRejectsEstimateOverflow(t *testing.T) {
	spec := validJobSpec()
	spec.PriceToMilliPerHour = math.MaxInt64
	if err := spec.Validate(); err == nil {
		t.Fatal("Validate() accepted an overflowing spend estimate")
	}
}

func TestJobSpecRejectsUnsafeOutputPath(t *testing.T) {
	spec := validJobSpec()
	spec.OutputRoot = "/tmp/results"
	if err := spec.Validate(); err == nil {
		t.Fatal("Validate() succeeded for unsafe output path")
	}
}

func TestPhaseZeroGlobalSpendCap(t *testing.T) {
	if MaxPhaseZeroSpendMilli != 20_000 {
		t.Fatalf("MaxPhaseZeroSpendMilli = %d", MaxPhaseZeroSpendMilli)
	}
}
