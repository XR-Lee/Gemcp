package experiment

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/database"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
)

func TestPreparedConcurrentSubmissionCreatesOnePostgresExperiment(t *testing.T) {
	databaseURL := os.Getenv("GEMCP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GEMCP_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store, err := database.Open(ctx, databaseURL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := store.Client
	suffix := uuid.NewString()
	tenant, err := client.Tenant.Create().SetName("proposal-" + suffix).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projectRecord, err := client.Project.Create().SetTenantID(tenant.ID).SetName("proposal").SetSlug("proposal-" + suffix).
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		SetTimeoutExtensionSeconds(3600).SetTerminationGraceSeconds(60).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repositoryRecord, err := client.Repository.Create().SetProjectID(projectRecord.ID).SetName("repository").
		SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").SetDefaultBranch("main").
		SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = repositoryRecord
	if _, err := client.Environment.Create().SetProjectID(projectRecord.ID).SetName("default").SetImageUUID("image-uuid").SetIsDefault(true).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResourceProfile.Create().SetProjectID(projectRecord.ID).SetName("default").SetRegion("private").
		SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(118).
		SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx); err != nil {
		t.Fatal(err)
	}
	token, _, err := createAgentToken(ctx, client, projectRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(client, box, git, WithPreparedExperiments(
		git, git, proposalProvider{idle: 2}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	principal := agentauth.Principal{
		TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), ProjectID: projectRecord.ID,
		ProjectPublicID: projectRecord.PublicID.String(), TokenID: token.ID, TokenPublicID: token.PublicID.String(),
		TokenLabel: token.Label, Scopes: []string{"read", "submit", "cancel"},
	}
	prepared, err := service.Prepare(ctx, principal, validPrepare())
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	stored, err := client.ExperimentProposal.Query().Where(experimentproposal.PublicIDEQ(uuid.MustParse(prepared.Proposal.ID))).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := service.currentProposal(ctx, principal, stored)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedDigest := proposalDigest(reloaded); reloadedDigest != prepared.Proposal.ConfirmationDigest {
		t.Fatalf("persisted proposal digest drifted: prepared=%s reloaded=%s expires prepared=%s stored=%s", prepared.Proposal.ConfirmationDigest, reloadedDigest, prepared.Proposal.ExpiresAt.Format(time.RFC3339Nano), stored.ExpiresAt.Format(time.RFC3339Nano))
	}
	input := SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest}
	type response struct {
		result SubmitPreparedResult
		err    error
	}
	responses := make(chan response, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.SubmitPrepared(ctx, principal, input)
			responses <- response{result: result, err: err}
		}()
	}
	wait.Wait()
	close(responses)
	ids := map[string]bool{}
	idempotent := 0
	for response := range responses {
		if response.err != nil {
			t.Fatalf("concurrent SubmitPrepared() error = %v", response.err)
		}
		ids[response.result.Experiment.ID] = true
		if response.result.Idempotent {
			idempotent++
		}
	}
	if len(ids) != 1 || idempotent != 1 {
		t.Fatalf("concurrent results IDs=%v idempotent=%d", ids, idempotent)
	}
	if count, _ := client.Experiment.Query().Where(entexperiment.ProjectIDEQ(projectRecord.ID)).Count(ctx); count != 1 {
		t.Fatalf("concurrent Experiment count = %d", count)
	}
	if count, _ := client.BudgetEntry.Query().Where(budgetentry.ProjectIDEQ(projectRecord.ID), budgetentry.KindEQ("reservation")).Count(ctx); count != 1 {
		t.Fatalf("concurrent reservation count = %d", count)
	}
}
