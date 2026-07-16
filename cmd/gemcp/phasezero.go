package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/phasezero"
)

const liveSpendConfirmation = "I_ACCEPT_AUTODL_CHARGES"

func runPhaseZero(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("phase0 requires a subcommand: read or job")
	}
	switch args[0] {
	case "read":
		return runPhaseZeroRead(args[1:])
	case "job":
		return runPhaseZeroJob(args[1:])
	default:
		return fmt.Errorf("unknown phase0 subcommand %q (expected read or job)", args[0])
	}
}

func runPhaseZeroRead(args []string) error {
	flags := flag.NewFlagSet("phase0 read", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	backend := flags.String("backend", "elastic", "provider backend: elastic or pro")
	region := flags.String("region", "", "AutoDL region for Elastic inventory")
	timeout := flags.Duration("timeout", 90*time.Second, "overall read probe timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, baseURL, err := phaseZeroClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := phasezero.RunReadProbe(ctx, client, strings.ToLower(*backend), baseURL, *region)
	if encodeErr := writeJSON(os.Stdout, report); encodeErr != nil {
		return encodeErr
	}
	return err
}

func runPhaseZeroJob(args []string) error {
	flags := flag.NewFlagSet("phase0 job", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	specPath := flags.String("spec", "", "path to phase-zero JSON job specification")
	spendCapMilli := flags.Int64("spend-cap-milli", 0, "maximum real spend in milli-CNY, never above 20000")
	confirmation := flags.String("confirm-live-spend", "", "required exact live-spend confirmation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *specPath == "" {
		return fmt.Errorf("--spec is required")
	}
	if *confirmation != liveSpendConfirmation {
		return fmt.Errorf("live job blocked: pass --confirm-live-spend=%s after reviewing the specification", liveSpendConfirmation)
	}

	specFile, err := os.Open(*specPath)
	if err != nil {
		return fmt.Errorf("open phase-zero spec: %w", err)
	}
	defer specFile.Close()
	var spec phasezero.JobSpec
	decoder := json.NewDecoder(io.LimitReader(specFile, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return fmt.Errorf("decode phase-zero spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("validate phase-zero spec: %w", err)
	}

	client, _, err := phaseZeroClient()
	if err != nil {
		return err
	}
	runner := phasezero.NewJobRunner(client)
	provisionTimeoutSeconds := spec.ProvisionTimeoutSeconds
	if provisionTimeoutSeconds == 0 {
		provisionTimeoutSeconds = 600
	}
	totalTimeout := time.Duration(provisionTimeoutSeconds+spec.MaxRuntimeSeconds+60) * time.Second
	if totalTimeout < 2*time.Minute {
		totalTimeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()
	report, runErr := runner.Run(ctx, spec, *spendCapMilli)
	if encodeErr := writeJSON(os.Stdout, report); encodeErr != nil {
		return encodeErr
	}
	return runErr
}

func phaseZeroClient() (*autodl.Client, string, error) {
	token := strings.TrimSpace(os.Getenv("GEMCP_PHASE0_AUTODL_TOKEN"))
	if token == "" {
		return nil, "", fmt.Errorf("GEMCP_PHASE0_AUTODL_TOKEN is required")
	}
	baseURL := strings.TrimSpace(os.Getenv("GEMCP_PHASE0_AUTODL_BASE_URL"))
	if baseURL == "" {
		baseURL = autodl.DefaultBaseURL
	}
	client, err := autodl.NewClient(baseURL, token, autodl.WithUserAgent("Gemcp/"+version+" phase-zero"))
	if err != nil {
		return nil, "", err
	}
	return client, baseURL, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}
