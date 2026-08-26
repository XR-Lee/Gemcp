package projectpolicy

import (
	"fmt"
	"time"

	"github.com/XR-Lee/Gemcp/internal/validation"
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type Limits struct {
	MonthlyBudgetMilli      int64
	MaxExperimentMilli      int64
	MaxConcurrency          int
	MaxRuntimeSeconds       int
	TimeoutExtensionSeconds int
	TerminationGraceSeconds int
}

func Validate(limits Limits) error {
	if limits.MonthlyBudgetMilli <= 0 || limits.MaxExperimentMilli <= 0 || limits.MaxExperimentMilli > limits.MonthlyBudgetMilli {
		return invalid("project budgets must be positive and the experiment cap cannot exceed the monthly budget")
	}
	if limits.MaxConcurrency <= 0 || limits.MaxRuntimeSeconds <= 0 || limits.TimeoutExtensionSeconds < 0 || limits.TerminationGraceSeconds < 0 {
		return invalid("project concurrency and runtime limits are invalid")
	}
	totalRuntime := int64(limits.MaxRuntimeSeconds) + int64(limits.TimeoutExtensionSeconds) + int64(limits.TerminationGraceSeconds)
	if totalRuntime > int64((30*24*time.Hour)/time.Second) || limits.TerminationGraceSeconds > 3600 {
		return invalid("project runtime, extension, and grace must fit within 30 days and grace must not exceed one hour")
	}
	return nil
}

func ValidateUpdate(current Limits, next Limits) error {
	if err := Validate(next); err != nil {
		return err
	}
	if next == current {
		return invalid("project policy update must change at least one limit")
	}
	return nil
}

func FormatCNY(milli int64) string {
	if milli < 0 {
		return fmt.Sprintf("-%d.%03d", (-milli)/1000, (-milli)%1000)
	}
	return fmt.Sprintf("%d.%03d", milli/1000, milli%1000)
}
