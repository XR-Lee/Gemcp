package experiment

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/ent/repository"
)

func TestOwnerRepositoryReadinessPendingDeployKey(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	pending, err := f.client.Repository.Create().
		SetProjectID(f.project.ID).SetName("private-lab").
		SetSSHURL("git@github.com:research/private-lab.git").SetSSHHost("github.com").
		SetDefaultBranch("main").SetDeployPublicKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake gemcp-test").
		SetStatus(repository.StatusPendingKey).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(f.client, f.box, f.verifier)
	view, err := service.OwnerRepositoryReadiness(ctx, f.principal.TenantID, f.project.PublicID.String(), pending.PublicID.String())
	if err != nil {
		t.Fatal(err)
	}
	if view.Ready || view.Access != "ssh_deploy_key" || view.DeployKeySettingsURL != "https://github.com/research/private-lab/settings/keys" {
		t.Fatalf("pending readiness = %+v", view)
	}
	if len(view.Blockers) < 2 || view.Blockers[0].Kind != "deploy_key_required" || view.Blockers[0].Href != view.DeployKeySettingsURL {
		t.Fatalf("pending blockers = %+v", view.Blockers)
	}
	if !hasBlocker(view.Blockers, "dataset_binding_required") {
		t.Fatalf("expected dataset blocker: %+v", view.Blockers)
	}
}

func TestOwnerRepositoryReadinessInspectsManifestAndDefaults(t *testing.T) {
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
	git := &proposalGit{resolved: proposalCommit, archive: namedWorkloadArchive(t), branch: "master"}
	service := NewService(f.client, f.box, git, WithPreparedExperiments(
		git, git, proposalProvider{idle: 2}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	view, err := service.OwnerRepositoryReadiness(ctx, f.principal.TenantID, f.project.PublicID.String(), f.repository.PublicID.String())
	if err != nil || !view.Ready {
		t.Fatalf("ready view = %+v err=%v", view, err)
	}
	if view.DetectedDefaultBranch != "master" || view.DefaultBranch != "master" || view.CommitSHA != proposalCommit {
		t.Fatalf("detected branch = %+v", view)
	}
	if !view.Manifest.Present || strings.Join(view.Manifest.Workloads, ",") != "objbg-smoke" {
		t.Fatalf("manifest = %+v", view.Manifest)
	}
	if view.Defaults.Environment == nil || view.Defaults.ResourceProfile == nil || view.Defaults.DatasetBinding == nil {
		t.Fatalf("defaults = %+v", view.Defaults)
	}
	if view.Defaults.DatasetBinding.Name != "scanobjectnn-objbg" {
		t.Fatalf("dataset default = %+v", view.Defaults.DatasetBinding)
	}
	updated, err := f.client.Repository.Get(ctx, f.repository.ID)
	if err != nil || updated.DefaultBranch != "master" {
		t.Fatalf("persisted branch = %+v err=%v", updated, err)
	}
}

func TestOwnerRepositoryReadinessMissingManifestIsNotABlocker(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	git := &proposalGit{resolved: proposalCommit, archive: emptyRootArchive(t)}
	service := NewService(f.client, f.box, git, WithPreparedExperiments(
		git, git, proposalProvider{idle: 2}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	view, err := service.OwnerRepositoryReadiness(ctx, f.principal.TenantID, f.project.PublicID.String(), f.repository.PublicID.String())
	if err != nil {
		t.Fatal(err)
	}
	if view.Manifest.Present || view.Manifest.Error == "" {
		t.Fatalf("missing manifest = %+v", view.Manifest)
	}
	if !hasBlocker(view.Blockers, "dataset_binding_required") {
		t.Fatalf("blockers = %+v", view.Blockers)
	}
	if view.Ready {
		t.Fatal("missing dataset should keep the repository unready")
	}
}

func TestOwnerRepositoryReadinessNotFound(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	_, err := f.service.OwnerRepositoryReadiness(context.Background(), f.principal.TenantID, f.project.PublicID.String(), "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Fatalf("error = %v", err)
	}
}

func hasBlocker(blockers []ReadinessBlocker, kind string) bool {
	for _, blocker := range blockers {
		if blocker.Kind == kind {
			return true
		}
	}
	return false
}

func emptyRootArchive(t *testing.T) []byte {
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
	if err := tarWriter.WriteHeader(&tar.Header{Name: "repo/README.md", Mode: 0o644, Size: 6, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("hello\n")); err != nil {
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
