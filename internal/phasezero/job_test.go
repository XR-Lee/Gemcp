package phasezero

import "testing"

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
