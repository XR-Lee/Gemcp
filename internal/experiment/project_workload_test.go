package experiment

import (
	"context"
	"errors"
	"strings"
	"testing"

	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/internal/workload"
)

func TestOwnerSaveOneShotAsProjectWorkload(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	service, view := succeededOneShot(t, f)
	preview, err := service.OwnerPreviewWorkload(ctx, f.principal.TenantID, f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: view.ID, Name: "oneshot-smoke",
	})
	if err != nil || preview.Name != "oneshot-smoke" || !strings.Contains(preview.ManifestYAML, "python") {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if _, err := workload.Parse([]byte(preview.ManifestYAML)); err != nil {
		t.Fatal(err)
	}
	saved, err := service.OwnerSaveWorkload(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: view.ID, Name: "oneshot-smoke",
	})
	if err != nil || saved.Name != "oneshot-smoke" || saved.SourceExperimentID != view.ID {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	listed, err := service.OwnerListWorkloads(ctx, f.principal.TenantID, f.project.PublicID.String())
	if err != nil || len(listed.Workloads) != 1 || listed.Workloads[0].Name != "oneshot-smoke" {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	detail, err := service.OwnerGet(ctx, f.principal.TenantID, f.project.PublicID.String(), view.ID)
	if err != nil || detail.SavableWorkload || detail.SavedWorkload != "oneshot-smoke" {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	if _, err := service.OwnerSaveWorkload(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: view.ID, Name: "oneshot-other",
	}); err == nil || !strings.Contains(err.Error(), "already saved") {
		t.Fatalf("duplicate source error = %v", err)
	}
}

func TestOwnerSaveWorkloadRejectsFailedNamedAndInvalid(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	service := preparedService(t, f, 2)
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{
		ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.OwnerSaveWorkload(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: submitted.Experiment.ID, Name: "oneshot-smoke",
	}); err == nil || !strings.Contains(err.Error(), "succeeded") {
		t.Fatalf("queued save error = %v", err)
	}
	named := preparedService(t, f, 2)
	namedGit := &proposalGit{resolved: proposalCommit, archive: namedWorkloadArchive(t)}
	named = NewService(
		f.client, f.box, namedGit,
		WithPreparedExperiments(namedGit, namedGit, proposalProvider{idle: 2}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20}),
	)
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("scanobjectnn-objbg").SetBackend("autodl_private").
		SetCanonicalRoot("/root/autodl-fs/datasets/ScanObjectNN").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	namedPrepared, err := named.Prepare(ctx, f.principal, PrepareInput{
		Workload: "objbg-smoke", Parameters: map[string]string{"config": "cfgs/scanobjectnn.yaml"},
	})
	if err != nil {
		t.Fatal(err)
	}
	namedSubmitted, err := named.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{
		ProposalID: namedPrepared.Proposal.ID, ConfirmationDigest: namedPrepared.Proposal.ConfirmationDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Experiment.Update().Where(entexperiment.IDEQ(mustExperimentID(t, f, namedSubmitted.Experiment.ID))).
		SetState("succeeded").Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := named.OwnerSaveWorkload(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: namedSubmitted.Experiment.ID, Name: "copy-named",
	}); err == nil || !strings.Contains(err.Error(), "named workload") {
		t.Fatalf("named save error = %v", err)
	}
}

func TestPrepareUsesSavedProjectWorkloadWhenManifestMissing(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	service, view := succeededOneShot(t, f)
	if _, err := service.OwnerSaveWorkload(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: view.ID, Name: "oneshot-smoke",
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(ctx, f.principal, PrepareInput{Workload: "oneshot-smoke"})
	if err != nil || prepared.Proposal == nil || prepared.Proposal.Workload != "oneshot-smoke" {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	if prepared.Proposal.Execution.DisplayCommand != "python smoke.py --label 'value with spaces'" {
		t.Fatalf("display command = %q", prepared.Proposal.Execution.DisplayCommand)
	}
}

func TestOwnerSaveWorkloadNameConflict(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	service, first := succeededOneShot(t, f)
	if _, err := service.OwnerSaveWorkload(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: first.ID, Name: "oneshot-smoke",
	}); err != nil {
		t.Fatal(err)
	}
	secondService, second := succeededOneShot(t, f)
	if _, err := secondService.OwnerSaveWorkload(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), SaveWorkloadInput{
		ExperimentID: second.ID, Name: "oneshot-smoke",
	}); !errors.Is(err, ErrWorkloadConflict) {
		t.Fatalf("name conflict = %v", err)
	}
}

func succeededOneShot(t *testing.T, f fixture) (*Service, View) {
	t.Helper()
	service := preparedService(t, f, 2)
	ctx := context.Background()
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{
		ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	recordID := mustExperimentID(t, f, submitted.Experiment.ID)
	if _, err := f.client.Experiment.Update().Where(entexperiment.IDEQ(recordID)).SetState("succeeded").Save(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := service.OwnerGet(ctx, f.principal.TenantID, f.project.PublicID.String(), submitted.Experiment.ID)
	if err != nil || !view.SavableWorkload {
		t.Fatalf("succeeded one-shot = %+v, %v", view, err)
	}
	return service, view
}

func mustExperimentID(t *testing.T, f fixture, publicID string) int {
	t.Helper()
	record, err := f.service.getRecord(context.Background(), f.project.ID, publicID)
	if err != nil {
		t.Fatal(err)
	}
	return record.ID
}
