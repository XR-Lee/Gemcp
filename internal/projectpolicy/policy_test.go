package projectpolicy

import "testing"

func TestValidateAcceptsSetupBounds(t *testing.T) {
	if err := Validate(Limits{
		MonthlyBudgetMilli: 100000, MaxExperimentMilli: 20000, MaxConcurrency: 1,
		MaxRuntimeSeconds: 57600, TimeoutExtensionSeconds: 3600, TerminationGraceSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsCapAboveMonthlyBudget(t *testing.T) {
	err := Validate(Limits{
		MonthlyBudgetMilli: 10000, MaxExperimentMilli: 20000, MaxConcurrency: 1,
		MaxRuntimeSeconds: 3600, TimeoutExtensionSeconds: 3600, TerminationGraceSeconds: 60,
	})
	if err == nil {
		t.Fatal("expected budget inversion to fail")
	}
}

func TestValidateRejectsRuntimeBeyondThirtyDays(t *testing.T) {
	err := Validate(Limits{
		MonthlyBudgetMilli: 100000, MaxExperimentMilli: 20000, MaxConcurrency: 1,
		MaxRuntimeSeconds: 30*24*3600 - 10, TimeoutExtensionSeconds: 20, TerminationGraceSeconds: 0,
	})
	if err == nil {
		t.Fatal("expected 30-day overflow to fail")
	}
}

func TestValidateUpdateRequiresAChange(t *testing.T) {
	limits := Limits{
		MonthlyBudgetMilli: 100000, MaxExperimentMilli: 20000, MaxConcurrency: 1,
		MaxRuntimeSeconds: 3600, TimeoutExtensionSeconds: 3600, TerminationGraceSeconds: 60,
	}
	if err := ValidateUpdate(limits, limits); err == nil {
		t.Fatal("expected unchanged policy to fail")
	}
}
