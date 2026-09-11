package experiment

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
)

const namedWorkloadYAML = `version: 1
workloads:
  objbg-smoke:
    entrypoint:
      - python
      - tools/run.py
    arguments:
      - flag: --config
        parameter: config
      - flag: --seed
        parameter: seed
    runtime_preset: smoke
    datasets:
      - scanobjectnn-objbg
    parameters:
      config:
        type: config_path
        allowed_prefix: cfgs
        required: true
      seed:
        type: integer
        minimum: 0
        default: 2
`

func TestPrepareNamedWorkloadFromManifest(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("scanobjectnn-objbg").SetBackend("autodl_private").
		SetCanonicalRoot("/root/autodl-fs/datasets/ScanObjectNN").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	git := &proposalGit{resolved: proposalCommit, archive: namedWorkloadArchive(t)}
	service := NewService(
		f.client, f.box, git,
		WithPreparedExperiments(git, git, proposalProvider{idle: 2}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20}),
	)
	prepared, err := service.Prepare(ctx, f.principal, PrepareInput{
		Workload: "objbg-smoke", Parameters: map[string]string{"config": "cfgs/scanobjectnn.yaml"},
	})
	if err != nil || prepared.Proposal == nil || prepared.Proposal.Workload != "objbg-smoke" {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	if prepared.Proposal.Execution.DisplayCommand != "python tools/run.py --config cfgs/scanobjectnn.yaml --seed 2" {
		t.Fatalf("display command = %q", prepared.Proposal.Execution.DisplayCommand)
	}
	if prepared.Proposal.Dataset != "scanobjectnn-objbg" || prepared.Proposal.RuntimePreset != "smoke" {
		t.Fatalf("proposal defaults = %+v", prepared.Proposal)
	}
	if prepared.Proposal.Repository.Access != gitrepository.AccessPublicHTTPS {
		t.Fatalf("repository access = %q", prepared.Proposal.Repository.Access)
	}
	feed, err := service.OwnerOperations(ctx, f.principal.TenantID, f.project.PublicID.String(), 25)
	if err != nil || len(feed.Proposals) != 1 || feed.Proposals[0].Workload != "objbg-smoke" ||
		feed.Proposals[0].Dataset != "scanobjectnn-objbg" || feed.Proposals[0].RepositoryAccess != gitrepository.AccessPublicHTTPS {
		t.Fatalf("operations feed = %+v, %v", feed, err)
	}
	stored, err := f.client.ExperimentProposal.Query().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := service.currentProposal(ctx, f.principal, stored)
	if err != nil {
		t.Fatal(err)
	}
	if proposalDigest(reloaded) != prepared.Proposal.ConfirmationDigest {
		t.Fatalf("named workload digest drifted prepared=%s reloaded=%s", prepared.Proposal.ConfirmationDigest, proposalDigest(reloaded))
	}
}

func TestPrepareNamedWorkloadRejectsArgvAndMissingManifest(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(
		f.client, f.box, git,
		WithPreparedExperiments(git, git, proposalProvider{idle: 2}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20}),
	)
	if _, err := service.Prepare(ctx, f.principal, PrepareInput{
		Workload: "objbg-smoke", Argv: []string{"python", "smoke.py"},
	}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("argv+workload error = %v", err)
	}
	if _, err := service.Prepare(ctx, f.principal, PrepareInput{Workload: "objbg-smoke"}); err == nil || !strings.Contains(err.Error(), "gemcp.yaml") {
		t.Fatalf("missing manifest error = %v", err)
	}
}

func namedWorkloadArchive(t *testing.T) []byte {
	t.Helper()
	var result bytes.Buffer
	gzipWriter := gzip.NewWriter(&result)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": proposalCommit},
	}); err != nil {
		t.Fatal(err)
	}
	content := []byte(namedWorkloadYAML)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "repo/gemcp.yaml", Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}
