package experiment

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestListArtifactsIncludesManifestAndBoundedReads(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	submitted, err := f.service.Submit(ctx, f.principal, validSubmit(f, "request-artifact-read"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.client.Experiment.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := record.Update().
		SetState("succeeded").
		SetProviderResourceID("deployment-1").
		SetLogTail("epoch 1\n").
		SetMetrics(map[string]any{"accuracy": 0.91}).
		SetExitCode(0).
		SetBudgetFinalizedAt(now).
		Save(ctx); err != nil {
		t.Fatal(err)
	}

	listed, err := f.service.Artifacts(ctx, f.principal, submitted.Experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Artifacts) != 4 || listed.Manifest == nil {
		t.Fatalf("artifacts=%+v", listed)
	}
	byName := map[string]ArtifactManifestEntry{}
	for _, entry := range listed.Manifest {
		byName[entry.Name] = entry
	}
	if !byName["metrics.json"].Available || !byName["metrics.json"].Readable || byName["metrics.json"].MediaType != "application/json" ||
		byName["metrics.json"].Checksum == "" || byName["metrics.json"].SizeBytes == nil {
		t.Fatalf("metrics manifest=%+v", byName["metrics.json"])
	}
	if byName["gemcp-launch.log"].Available || byName["gemcp-launch.log"].Readable || byName["gemcp-launch.log"].Availability != "shared_storage" {
		t.Fatalf("launch manifest=%+v", byName["gemcp-launch.log"])
	}

	metrics, err := f.service.ReadArtifact(ctx, f.principal, ArtifactReadInput{ExperimentID: submitted.Experiment.ID, Name: "metrics.json"})
	if err != nil || !metrics.Available || metrics.JSON == nil || metrics.Checksum != byName["metrics.json"].Checksum {
		t.Fatalf("read metrics=%+v err=%v", metrics, err)
	}
	decoded, _ := metrics.JSON.(map[string]any)
	if decoded["accuracy"] != 0.91 {
		t.Fatalf("metrics json=%v", metrics.JSON)
	}

	logs, err := f.service.ReadArtifact(ctx, f.principal, ArtifactReadInput{ExperimentID: submitted.Experiment.ID, Name: "run.log"})
	if err != nil || logs.Text != "epoch 1\n" || logs.MediaType != "text/plain" {
		t.Fatalf("read log=%+v err=%v", logs, err)
	}

	result, err := f.service.ReadArtifact(ctx, f.principal, ArtifactReadInput{ExperimentID: submitted.Experiment.ID, Name: "gemcp-result.json"})
	if err != nil || !result.Available || result.JSON == nil {
		t.Fatalf("read result=%+v err=%v", result, err)
	}

	launch, err := f.service.ReadArtifact(ctx, f.principal, ArtifactReadInput{ExperimentID: submitted.Experiment.ID, Name: "gemcp-launch.log"})
	if err != nil || launch.Available || launch.Text != "" || launch.JSON != nil || launch.UnavailableReason == "" {
		t.Fatalf("read launch=%+v err=%v", launch, err)
	}

	if _, err := f.service.ReadArtifact(ctx, f.principal, ArtifactReadInput{ExperimentID: submitted.Experiment.ID, Name: "../secret"}); err == nil || !strings.Contains(err.Error(), "registered filename") {
		t.Fatalf("path escape error = %v", err)
	}
	if _, err := f.service.ReadArtifact(ctx, f.principal, ArtifactReadInput{ExperimentID: submitted.Experiment.ID, Name: "weights.bin"}); err == nil || !strings.Contains(err.Error(), "registered filename") {
		t.Fatalf("unknown name error = %v", err)
	}

	view, err := f.service.Get(ctx, f.principal, submitted.Experiment.ID)
	if err != nil || len(view.ArtifactManifest) != 4 {
		t.Fatalf("Get manifest=%+v err=%v", view.ArtifactManifest, err)
	}
	compact, err := f.service.List(ctx, f.principal, ListInput{})
	if err != nil || len(compact.Experiments) != 1 || len(compact.Experiments[0].ArtifactManifest) != 0 ||
		len(compact.Experiments[0].Artifacts) != 0 || len(compact.Experiments[0].DatasetBindings) != 0 {
		t.Fatalf("List expanded artifacts: %+v, %v", compact, err)
	}
}

func TestGetExposesImmutableDatasetBindingSnapshot(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("scanobjectnn-objbg").SetBackend("autodl_private").
		SetCanonicalRoot("/root/autodl-fs/datasets/ScanObjectNN").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").
		SetRequiredMarkers([]string{"main_split/train.h5"}).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	service := preparedService(t, f, 2)
	input := validPrepare()
	input.RuntimePreset = "train"
	input.MaxRuntimeSeconds = 3600
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("Prepare()=%+v err=%v", prepared, err)
	}
	submitted, err := service.OwnerSubmitPrepared(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), prepared.Proposal.ID, OwnerSubmitPreparedInput{
		ConfirmationDigest: prepared.Proposal.ConfirmationDigest, Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, f.principal, submitted.Experiment.ID)
	if err != nil || len(view.DatasetBindings) != 1 || view.DatasetBindings[0].Name != "scanobjectnn-objbg" ||
		view.DatasetBindings[0].CanonicalRoot != "/root/autodl-fs/datasets/ScanObjectNN" ||
		len(view.DatasetBindings[0].RequiredMarkers) != 1 {
		t.Fatalf("Get bindings=%+v err=%v", view.DatasetBindings, err)
	}
	if _, err := f.client.DatasetBinding.Delete().Exec(ctx); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := service.Get(ctx, f.principal, submitted.Experiment.ID)
	if err != nil || len(afterDelete.DatasetBindings) != 1 || afterDelete.DatasetBindings[0].Name != "scanobjectnn-objbg" {
		t.Fatalf("snapshot drifted after delete: %+v, %v", afterDelete.DatasetBindings, err)
	}
}
