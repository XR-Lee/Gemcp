package experiment

import (
	"context"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/execution"
)

func TestPreparedExperimentQueuesWhenSchedulerDispatchIsDisabled(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithPreparedExperiments(
		git,
		git,
		proposalProvider{idle: 2},
		proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled:    false,
			SchedulerHealthy:    true,
			WatchdogHealthy:     true,
			PublicURLConfigured: true,
			PublicURLHTTPS:      true,
			GlobalConcurrency:   2,
		}},
		ProposalConfig{SourceMaxBytes: 1 << 20},
	))

	prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	var schedulerCheck *ProposalCheck
	for index := range prepared.Proposal.Checks {
		if prepared.Proposal.Checks[index].ID == "scheduler" {
			schedulerCheck = &prepared.Proposal.Checks[index]
			break
		}
	}
	if schedulerCheck == nil || schedulerCheck.Status != ProposalCheckWarn ||
		!strings.Contains(schedulerCheck.Detail, "GEMCP_SCHEDULER_ENABLED=true") {
		t.Fatalf("scheduler check = %+v", schedulerCheck)
	}

	submitted, err := service.SubmitPrepared(context.Background(), f.principal, SubmitPreparedInput{
		ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest,
	})
	if err != nil || submitted.Experiment.State != "queued" {
		t.Fatalf("SubmitPrepared() = %+v, %v", submitted, err)
	}
}

func TestPreparedExperimentBlocksWhenEnabledSchedulerHeartbeatIsStale(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithPreparedExperiments(
		git,
		git,
		proposalProvider{idle: 2},
		proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled:    true,
			SchedulerHealthy:    false,
			WatchdogHealthy:     true,
			PublicURLConfigured: true,
			PublicURLHTTPS:      true,
			GlobalConcurrency:   2,
		}},
		ProposalConfig{SourceMaxBytes: 1 << 20},
	))

	prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
	if err != nil || prepared.Proposal == nil || prepared.Proposal.Eligible {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	for _, check := range prepared.Proposal.Checks {
		if check.ID == "scheduler" && check.Status == ProposalCheckFail && strings.Contains(check.Summary, "heartbeat") {
			return
		}
	}
	t.Fatalf("scheduler heartbeat was not reported as a blocking failure: %+v", prepared.Proposal.Checks)
}
