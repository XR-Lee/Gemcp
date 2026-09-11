package experiment

import (
	"context"
	"errors"
	"testing"
)

func TestAgentActivityAndProposalFeed(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	f.principal.TokenPrefix = "gmc_visible"
	ctx := context.Background()
	reported, err := f.service.ReportActivity(ctx, f.principal, ReportActivityInput{
		Phase: "inspecting_repository", RepositoryRemote: f.repository.SSHURL, Ref: "main",
	})
	if err != nil || reported.Activity.Phase != "inspecting_repository" || reported.Activity.AgentLabel != "test" {
		t.Fatalf("ReportActivity() = %+v, %v", reported, err)
	}
	service := preparedService(t, f, 2)
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil || prepared.Proposal == nil {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	feed, err := service.OwnerOperations(ctx, f.principal.TenantID, f.project.PublicID.String(), 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Activities) != 3 || feed.Activities[0].Phase != "awaiting_confirmation" || len(feed.Proposals) != 1 ||
		feed.Proposals[0].ID != prepared.Proposal.ID || !feed.Proposals[0].Eligible || feed.Proposals[0].EnvironmentName != "default" ||
		feed.Proposals[0].RepositoryAccess == "" || feed.Proposals[0].RepositoryURL == "" {
		t.Fatalf("operations feed = %+v", feed)
	}
	if _, err := service.ReportActivity(ctx, f.principal, ReportActivityInput{Phase: "thinking freely"}); err == nil {
		t.Fatal("unsupported activity phase was accepted")
	}
	if _, err := service.ReportActivity(ctx, f.principal, ReportActivityInput{Phase: "inspecting_repository", Ref: "main\nsecret"}); err == nil {
		t.Fatal("activity context with a control character was accepted")
	}
	if _, err := service.ReportActivity(ctx, f.principal, ReportActivityInput{Phase: "monitoring"}); err == nil {
		t.Fatal("monitoring activity without an Experiment was accepted")
	}
	submitted, err := f.service.Submit(ctx, f.principal, validSubmit(f, "activity-monitoring"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportActivity(ctx, f.principal, ReportActivityInput{Phase: "monitoring", ExperimentID: submitted.Experiment.ID}); err != nil {
		t.Fatalf("monitor owned Experiment: %v", err)
	}
	otherToken, _, err := createAgentToken(ctx, f.client, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherPrincipal := f.principal
	otherPrincipal.TokenID, otherPrincipal.TokenPublicID = otherToken.ID, otherToken.PublicID.String()
	if _, err := service.ReportActivity(ctx, otherPrincipal, ReportActivityInput{Phase: "reviewing_results", ExperimentID: submitted.Experiment.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-Agent Experiment activity error = %v, want not found", err)
	}
}
