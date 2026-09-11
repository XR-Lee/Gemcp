package experiment

import "testing"

func TestAssessExperimentCoversTerminalAndCleanupStates(t *testing.T) {
	stage := "bootstrap_failed_during_source_extract"
	errorType := "ReadError"
	failureCode := "provision_timeout"
	failureReason := "Provisioning exceeded the deadline."
	cases := []struct {
		name           string
		view           View
		status         string
		classification string
		wantCleanup    bool
		wantRecommend  bool
	}{
		{
			name:           "queued",
			view:           View{State: "queued"},
			status:         "running",
			classification: "waiting_for_scheduler",
			wantCleanup:    true,
		},
		{
			name: "provisioning with runner stage",
			view: View{
				State:              "provisioning",
				RunnerStage:        &stage,
				BackendObservation: &BackendObservationView{CleanupComplete: false},
			},
			status:         "running",
			classification: "runner_bootstrap",
		},
		{
			name:           "running workload",
			view:           View{State: "running"},
			status:         "running",
			classification: "workload_running",
			wantCleanup:    true,
		},
		{
			name: "succeeded cleanup pending",
			view: View{
				State:              "succeeded",
				BackendObservation: &BackendObservationView{CleanupComplete: false},
			},
			status:         "running",
			classification: "cleanup_pending",
			wantRecommend:  true,
		},
		{
			name: "succeeded cleaned up",
			view: View{
				State:              "succeeded",
				BackendObservation: &BackendObservationView{CleanupComplete: true},
				Attempts:           []AttemptView{{Number: 1, State: "succeeded"}},
			},
			status:         "passed",
			classification: "succeeded",
			wantCleanup:    true,
		},
		{
			name:           "cancelled queued",
			view:           View{State: "cancelled"},
			status:         "cancelled",
			classification: "cancelled",
			wantCleanup:    true,
		},
		{
			name: "timed out",
			view: View{
				State:              "timed_out",
				BackendObservation: &BackendObservationView{CleanupComplete: true},
				Attempts:           []AttemptView{{Number: 1, State: "timed_out"}},
			},
			status:         "failed",
			classification: "timed_out",
			wantCleanup:    true,
			wantRecommend:  true,
		},
		{
			name: "failed provision timeout",
			view: View{
				State:              "failed",
				FailureCode:        &failureCode,
				FailureReason:      &failureReason,
				RunnerStage:        &stage,
				RunnerErrorType:    &errorType,
				BackendObservation: &BackendObservationView{CleanupComplete: true},
				Attempts:           []AttemptView{{Number: 1, State: "failed"}},
			},
			status:         "failed",
			classification: "provision_timeout",
			wantCleanup:    true,
			wantRecommend:  true,
		},
		{
			name:           "failed without attempt",
			view:           View{State: "failed"},
			status:         "failed",
			classification: "failed",
			wantCleanup:    true,
			wantRecommend:  true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := assessExperiment(test.view)
			if got == nil || got.Status != test.status || got.Classification != test.classification || got.CleanupComplete != test.wantCleanup {
				t.Fatalf("assessment = %+v", got)
			}
			if got.Summary == "" {
				t.Fatal("assessment summary is empty")
			}
			if test.wantRecommend && len(got.Recommendations) == 0 {
				t.Fatalf("expected recommendations: %+v", got)
			}
		})
	}
}
