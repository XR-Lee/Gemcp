package experiment

import "strings"

func assessExperiment(view View) *Assessment {
	result := Assessment{CleanupComplete: observationCleanupComplete(view)}
	if _, terminal := terminalStates[view.State]; !terminal {
		result.Status = "running"
		switch view.State {
		case "queued":
			result.Classification = "waiting_for_scheduler"
			result.Summary = "Experiment is queued for an execution slot."
		case "provisioning":
			result.Classification = "backend_provisioning"
			result.Summary = "Backend provisioning is in progress."
			if view.RunnerStage != nil && strings.TrimSpace(*view.RunnerStage) != "" {
				result.Classification = "runner_bootstrap"
				result.Summary = "Runner bootstrap reached " + strings.TrimSpace(*view.RunnerStage) + "."
			}
		case "running":
			result.Classification = "workload_running"
			result.Summary = "The workload is running."
		case "cancelling":
			result.Classification = "cancelling"
			result.Summary = "Cancellation is in progress."
		case "collecting":
			result.Classification = "collecting"
			result.Summary = "The run is collecting results."
		default:
			result.Classification = "cleanup"
			result.Summary = "Execution finished and managed cleanup is in progress."
		}
		return &result
	}
	if view.State == "succeeded" && !result.CleanupComplete {
		result.Status = "running"
		result.Classification = "cleanup_pending"
		result.Summary = "The Experiment is terminal, but managed backend cleanup is not complete."
		result.Recommendations = []string{"Do not retry until managed backend cleanup completes."}
		return &result
	}
	switch view.State {
	case "succeeded":
		result.Status = "passed"
		result.Classification = "succeeded"
		result.Summary = "The Experiment succeeded."
	case "cancelled":
		result.Status = "cancelled"
		result.Classification = "cancelled"
		result.Summary = firstNonEmpty(pointerText(view.FailureReason), "The Experiment was cancelled.")
	case "timed_out":
		result.Status = "failed"
		result.Classification = "timed_out"
		result.Summary = firstNonEmpty(pointerText(view.FailureReason), "The Experiment exceeded its runtime limit.")
		result.Recommendations = []string{"Inspect the latest Attempt log tail and Runner stage history before increasing the runtime preset."}
	case "budget_stopped":
		result.Status = "failed"
		result.Classification = "budget_stopped"
		result.Summary = firstNonEmpty(pointerText(view.FailureReason), "The Experiment was stopped because the Project budget was exhausted.")
		result.Recommendations = []string{"Inspect settlement and available Project budget before preparing a replacement proposal."}
	case "provider_error":
		result.Status = "failed"
		result.Classification = "provider_error"
		result.Summary = firstNonEmpty(pointerText(view.FailureReason), "The backend reported a provider error.")
		result.Recommendations = []string{"Inspect backend observation, stop reason, and the latest Attempt log tail before retrying."}
	default:
		result.Status = "failed"
		result.Classification = firstNonEmpty(pointerText(view.FailureCode), "failed")
		result.Summary = firstNonEmpty(pointerText(view.FailureReason), "The Experiment failed.")
		result.Recommendations = failureRecommendations(view)
	}
	if !result.CleanupComplete {
		result.Recommendations = append(result.Recommendations, "Do not retry until managed backend cleanup completes.")
	}
	if len(view.Attempts) == 0 && result.Status != "cancelled" && result.Status != "passed" {
		result.Recommendations = append(result.Recommendations, "No Attempt was created; inspect scheduler, budget, and backend availability checks.")
	}
	return &result
}

func observationCleanupComplete(view View) bool {
	if view.BackendObservation != nil {
		return view.BackendObservation.CleanupComplete
	}
	return true
}

func failureRecommendations(view View) []string {
	errorType := firstNonEmpty(pointerText(view.RunnerErrorType), metricString(view.Metrics, "error_type"))
	stage := firstNonEmpty(pointerText(view.RunnerStage), "")
	switch {
	case strings.Contains(strings.ToLower(errorType), "torch") || errorType == "ModuleNotFoundError" || errorType == "ImportError":
		return []string{"Use an image that contains the required Python packages.", "Inspect the latest Attempt log tail for the missing import."}
	case strings.Contains(strings.ToLower(errorType), "cuda") || strings.Contains(strings.ToLower(pointerText(view.FailureReason)), "cuda"):
		return []string{"Verify image CUDA compatibility with the selected GPU driver.", "Inspect nvidia-smi output and the latest Attempt log tail."}
	case strings.Contains(stage, "source"):
		return []string{"Inspect source archive and transfer checks in the timeline.", "Check the public Runner route for interrupted response bodies."}
	case strings.Contains(stage, "started_callback"):
		return []string{"Check the public Runner event route, Cloudflare edge logs, and callback reachability."}
	case resultClassification(view) == "node_lost" || resultClassification(view) == "node_unavailable":
		return []string{"Check gemcp-node service health, outbound HTTPS, Docker, and NVIDIA Container Toolkit on the selected Node."}
	case resultClassification(view) == "provision_timeout":
		return []string{"Use the Runner stage history to identify the last completed bootstrap phase.", "Inspect gemcp-launch.log when shared storage is available."}
	default:
		return []string{"Inspect the latest Attempt log tail, backend observation, and timeline before retrying."}
	}
}

func resultClassification(view View) string {
	return firstNonEmpty(pointerText(view.FailureCode), "failed")
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func metricString(metrics map[string]any, key string) string {
	if metrics == nil {
		return ""
	}
	value, _ := metrics[key].(string)
	return strings.TrimSpace(value)
}
