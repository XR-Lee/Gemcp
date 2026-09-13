package experiment

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/google/uuid"
)

const proposalCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type proposalGit struct {
	resolved string
	archive  []byte
	err      error
	branch   string
}

func (g *proposalGit) VerifyCommit(context.Context, int, string) error { return g.err }

func (g *proposalGit) ResolveRef(_ context.Context, _ int, _ string) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.resolved, nil
}

func (g *proposalGit) ArchiveCommit(context.Context, int, string, int64) (gitrepository.Archive, error) {
	if g.err != nil {
		return nil, g.err
	}
	return &memoryProposalArchive{data: append([]byte(nil), g.archive...)}, nil
}

func (g *proposalGit) DetectDefaultBranch(context.Context, int) (string, string, error) {
	if g.err != nil {
		return "", "", g.err
	}
	branch := g.branch
	if branch == "" {
		branch = "main"
	}
	return branch, g.resolved, nil
}

type memoryProposalArchive struct{ data []byte }

func (a *memoryProposalArchive) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.data)), nil
}
func (a *memoryProposalArchive) Close() error     { return nil }
func (a *memoryProposalArchive) SizeBytes() int64 { return int64(len(a.data)) }

type proposalProvider struct {
	idle    int
	err     error
	backend string
	region  string
}

func (p proposalProvider) QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error) {
	if p.err != nil {
		return provider.ResourceSnapshot{}, p.err
	}
	backend := p.backend
	if backend == "" {
		backend = "private"
	}
	return provider.ResourceSnapshot{
		Provider:      provider.Summary{Name: backend, Backend: backend, Status: "active"},
		GPUStock:      []provider.GPUStock{{Name: "RTX 4090", Region: p.region, Idle: p.idle, Total: 2}},
		PrivateImages: []provider.Image{{UUID: "image-uuid", Name: "image", Source: "private"}},
	}, nil
}

type proposalRuntime struct {
	healthy    bool
	selfHosted bool
	sshCloud   bool
}

func (r proposalRuntime) Status(context.Context) (execution.RuntimeStatus, error) {
	return execution.RuntimeStatus{
		SchedulerEnabled: r.healthy, SchedulerHealthy: r.healthy, WatchdogHealthy: r.healthy,
		PublicURLConfigured: r.healthy, PublicURLHTTPS: r.healthy, SelfHostedEnabled: r.selfHosted,
		SSHCloudEnabled: r.sshCloud, GlobalConcurrency: 2,
	}, nil
}

func preparedService(t *testing.T, f fixture, idle int) *Service {
	t.Helper()
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	return NewService(
		f.client, f.box, git,
		WithPreparedExperiments(git, git, proposalProvider{idle: idle}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20}),
	)
}

func proposalArchive(t *testing.T) []byte {
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
	content := []byte("print('ok')\n")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "smoke.py", Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
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

func validPrepare() PrepareInput {
	return PrepareInput{Argv: []string{"python", "smoke.py", "--label", "value with spaces"}, RuntimePreset: "smoke", MaxRuntimeSeconds: 300}
}

func TestPreparedExperimentPublicElasticRequiresMatchingRegion(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	environment, err := f.client.Environment.Create().
		SetProjectID(f.project.ID).SetName("elastic-default").SetBackend("autodl_elastic").
		SetImageUUID("image-uuid").SetIsDefault(false).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := f.client.ResourceProfile.Create().
		SetProjectID(f.project.ID).SetName("elastic-default").SetBackend("autodl_elastic").
		SetRegion("westDC2").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).
		SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input := validPrepare()
	input.Environment = environment.Name
	input.ResourceProfile = profile.Name
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	wrongRegion := NewService(f.client, f.box, git, WithPreparedExperiments(
		git, git, proposalProvider{idle: 2, backend: "elastic", region: "eastDC1"}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	blocked, err := wrongRegion.Prepare(ctx, f.principal, input)
	if err != nil || blocked.Proposal == nil || blocked.Proposal.Eligible {
		t.Fatalf("wrong-region Prepare() = %+v, %v", blocked, err)
	}

	service := NewService(f.client, f.box, git, WithPreparedExperiments(
		git, git, proposalProvider{idle: 2, backend: "elastic", region: "westDC2"}, proposalRuntime{healthy: true}, ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible || prepared.Proposal.Resource.Backend != "autodl_elastic" || prepared.Proposal.Resource.Region != "westDC2" {
		t.Fatalf("public Elastic Prepare() = %+v, %v", prepared, err)
	}
}

func TestPreparedExperimentIsZeroCostUntilConfirmedAndServerIdempotent(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	service := preparedService(t, f, 2)
	submissionTime := time.Date(2026, time.August, 30, 1, 30, 0, 0, time.FixedZone("BST", 60*60))
	service.now = func() time.Time { return submissionTime }
	ctx := context.Background()
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.Proposal == nil || !prepared.Proposal.Eligible || prepared.Proposal.Repository.CommitSHA != proposalCommit ||
		prepared.Proposal.Execution.Mode != "argv" || prepared.Proposal.ReservedCostMilli != 3825 || prepared.Proposal.ReservedCostCNY != "3.825" {
		t.Fatalf("prepared proposal = %+v", prepared)
	}
	if count, _ := f.client.Experiment.Query().Count(ctx); count != 0 {
		t.Fatalf("prepare created %d Experiments", count)
	}
	if count, _ := f.client.BudgetEntry.Query().Count(ctx); count != 0 {
		t.Fatalf("prepare created %d budget entries", count)
	}
	input := SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest}
	otherToken, _, err := createAgentToken(ctx, f.client, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherPrincipal := f.principal
	otherPrincipal.TokenID = otherToken.ID
	otherPrincipal.TokenPublicID = otherToken.PublicID.String()
	if _, err := service.SubmitPrepared(ctx, otherPrincipal, input); !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("cross-Token submit error = %v", err)
	}
	wrongDigest := input
	wrongDigest.ConfirmationDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := service.SubmitPrepared(ctx, f.principal, wrongDigest); !errors.Is(err, ErrProposalChanged) {
		t.Fatalf("wrong-digest submit error = %v", err)
	}
	first, err := service.SubmitPrepared(ctx, f.principal, input)
	if err != nil {
		t.Fatalf("SubmitPrepared() error = %v", err)
	}
	if first.Idempotent || first.Experiment.ExecutionMode != "argv" || strings.Join(first.Experiment.Argv, "|") != "python|smoke.py|--label|value with spaces" ||
		first.Experiment.Command != "python smoke.py --label 'value with spaces'" {
		t.Fatalf("submitted Experiment = %+v", first)
	}
	record, err := service.getRecord(ctx, f.project.ID, first.Experiment.ID)
	if err != nil || !record.NextAttemptAt.Equal(submissionTime.UTC()) || record.NextAttemptAt.Location() != time.UTC {
		t.Fatalf("next_attempt_at = %v, want UTC %v (error=%v)", record.NextAttemptAt, submissionTime.UTC(), err)
	}
	second, err := service.SubmitPrepared(ctx, f.principal, input)
	if err != nil || !second.Idempotent || second.Experiment.ID != first.Experiment.ID {
		t.Fatalf("idempotent prepared submit = %+v, %v", second, err)
	}
	if count, _ := f.client.Experiment.Query().Count(ctx); count != 1 {
		t.Fatalf("experiment count = %d", count)
	}
	if count, _ := f.client.BudgetEntry.Query().Count(ctx); count != 1 {
		t.Fatalf("budget entry count = %d", count)
	}
}

func TestPreparedExperimentRejectsDriftBlockedAndExpiredProposals(t *testing.T) {
	t.Run("configuration drift", func(t *testing.T) {
		f := newFixture(t, 100000, 20000)
		service := preparedService(t, f, 2)
		prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.profile.Update().SetPriceToMilli(4000).Save(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, err = service.SubmitPrepared(context.Background(), f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
		if !errors.Is(err, ErrProposalChanged) {
			t.Fatalf("drift submit error = %v", err)
		}
		if count, _ := f.client.Experiment.Query().Count(context.Background()); count != 0 {
			t.Fatalf("drift created %d Experiments", count)
		}
	})
	t.Run("capacity blocked", func(t *testing.T) {
		f := newFixture(t, 100000, 20000)
		service := preparedService(t, f, 0)
		prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
		if err != nil || prepared.Proposal == nil || prepared.Proposal.Eligible {
			t.Fatalf("blocked Prepare() = %+v, %v", prepared, err)
		}
		_, err = service.SubmitPrepared(context.Background(), f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
		if !errors.Is(err, ErrProposalBlocked) {
			t.Fatalf("blocked submit error = %v", err)
		}
	})
	t.Run("valid immediately before expiry", func(t *testing.T) {
		f := newFixture(t, 100000, 20000)
		service := preparedService(t, f, 2)
		base := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
		service.now = func() time.Time { return base }
		prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
		if err != nil {
			t.Fatal(err)
		}
		service.now = func() time.Time { return base.Add(2*time.Hour - time.Microsecond) }
		if _, err := service.SubmitPrepared(context.Background(), f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest}); err != nil {
			t.Fatalf("submit before expiry error = %v", err)
		}
	})
	t.Run("expired at deadline", func(t *testing.T) {
		f := newFixture(t, 100000, 20000)
		service := preparedService(t, f, 2)
		base := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
		service.now = func() time.Time { return base }
		prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
		if err != nil {
			t.Fatal(err)
		}
		service.now = func() time.Time { return base.Add(2 * time.Hour) }
		_, err = service.SubmitPrepared(context.Background(), f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
		if !errors.Is(err, ErrProposalExpired) {
			t.Fatalf("expired submit error = %v", err)
		}
	})
}

func TestPrepareReturnsChoicesInsteadOfGuessing(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	if _, err := f.client.Repository.Create().SetProjectID(f.project.ID).SetName("second").
		SetSSHURL("git@github.com:XR-Lee/second.git").SetSSHHost("github.com").SetDefaultBranch("main").
		SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	service := preparedService(t, f, 2)
	prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Proposal != nil || len(prepared.ChoiceRequired) != 2 || prepared.ChoiceRequired[0].Field != "repository" {
		t.Fatalf("choice result = %+v", prepared)
	}
	if count, _ := f.client.ExperimentProposal.Query().Count(context.Background()); count != 0 {
		t.Fatalf("ambiguous prepare persisted %d proposals", count)
	}
}

func TestPreparedSelfHostedRequiresArgvCapabilityAndIsUnmetered(t *testing.T) {
	for _, test := range []struct {
		name        string
		argvCapable bool
		eligible    bool
	}{
		{name: "new Node", argvCapable: true, eligible: true},
		{name: "old Node", argvCapable: false, eligible: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, 1, 1)
			ctx := context.Background()
			environment, err := f.client.Environment.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").
				SetName("self-runtime").SetImageUUID("registry.example/train@sha256:" + strings.Repeat("b", 64)).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := f.client.ResourceProfile.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").
				SetName("self-gpu").SetRegion("self_hosted").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).
				SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).
				SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			capabilities := map[string]any{"gpus": []any{map[string]any{"uuid": "GPU-test", "name": "RTX 4090"}}}
			if test.argvCapable {
				capabilities["execution_modes"] = []any{"shell", "argv"}
			}
			node, err := f.client.SelfHostedNode.Create().SetTenantID(f.principal.TenantID).SetLabel("node").
				SetTokenPrefix("gmn_test").SetTokenHash([]byte("node-hash")).SetStatus("active").SetObservedState("online").
				SetInstallationID(uuid.NewString()).SetMachineFingerprint(strings.Repeat("c", 64)).SetHostname("node").
				SetOperatingSystem("linux").SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion("1").
				SetCapabilities(capabilities).SetStorage(map[string]any{}).SetLastSeenAt(time.Now().UTC()).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.client.NodeProjectAccess.Create().SetTenantID(f.principal.TenantID).SetNodeID(node.ID).SetProjectID(f.project.ID).Save(ctx); err != nil {
				t.Fatal(err)
			}
			git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
			service := NewService(f.client, f.box, git, WithPreparedExperiments(
				git, git, nil, proposalRuntime{healthy: true, selfHosted: true},
				ProposalConfig{SourceMaxBytes: 1 << 20, SelfHostedEnabled: true},
			))
			input := validPrepare()
			input.Environment = environment.Name
			input.ResourceProfile = profile.Name
			prepared, err := service.Prepare(ctx, f.principal, input)
			if err != nil || prepared.Proposal == nil || prepared.Proposal.Eligible != test.eligible || prepared.Proposal.ReservedCostMilli != 0 {
				t.Fatalf("Self-hosted Prepare() = %+v, %v", prepared, err)
			}
			if !test.eligible {
				return
			}
			submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{
				ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest,
			})
			if err != nil || submitted.Experiment.ReservedCostMilli != 0 || !strings.HasPrefix(submitted.Experiment.OutputPath, "managed://") {
				t.Fatalf("Self-hosted SubmitPrepared() = %+v, %v", submitted, err)
			}
		})
	}
}

func TestPreparedTrustedWorkspaceAcceptsTagAndBindsOwnerPath(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node, err := f.client.SelfHostedNode.Create().SetTenantID(f.principal.TenantID).SetLabel("usb-pc").
		SetTokenPrefix("gmn_workspace").SetTokenHash([]byte("workspace-node-hash")).SetStatus("active").SetObservedState("online").
		SetInstallationID(uuid.NewString()).SetMachineFingerprint(strings.Repeat("d", 64)).SetHostname("workspace-node").
		SetOperatingSystem("linux").SetArchitecture("amd64").SetAgentVersion("test").SetProtocolVersion("1").
		SetCapabilities(map[string]any{
			"gpus":            []any{map[string]any{"uuid": "GPU-workspace", "name": "NVIDIA RTX A4000"}},
			"execution_modes": []any{"shell", "argv"}, "workspace_modes": []any{"trusted_rw"}, "dataset_modes": []any{"workspace_env_v1"},
		}).SetStorage(map[string]any{}).SetLastSeenAt(time.Now().UTC()).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workspacePath := "/home/campus.ncl.ac.uk/nxl51/gemcp_tmp"
	access, err := f.client.NodeProjectAccess.Create().SetTenantID(f.principal.TenantID).SetNodeID(node.ID).SetProjectID(f.project.ID).
		SetExecutionPolicy(nodeprojectaccess.ExecutionPolicyTrustedWorkspace).SetWorkspacePath(workspacePath).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := f.client.WorkspaceDataset.Create().SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).SetNodeID(node.ID).
		SetAgentTokenID(f.principal.TokenID).SetName("scanobjectnn-objbg").SetRelativePath("data/ScanObjectNN/main_split").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	environmentRecord, err := f.client.Environment.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").SetName("workspace-usb-pc").
		SetImageUUID("workspace:any-public-image").SetRecipeRef("trusted-workspace:" + node.PublicID.String()).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profileRecord, err := f.client.ResourceProfile.Create().SetProjectID(f.project.ID).SetBackend("self_hosted").SetName("workspace-usb-pc").
		SetRegion("trusted_workspace").SetGpuNames([]string{"NVIDIA RTX A4000"}).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).
		SetCPUFrom(1).SetCPUTo(22).SetMemoryFromGB(1).SetMemoryToGB(29).SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithPreparedExperiments(
		git, git, nil, proposalRuntime{healthy: true, selfHosted: true}, ProposalConfig{SourceMaxBytes: 1 << 20, SelfHostedEnabled: true},
	))
	input := validPrepare()
	input.Environment, input.ResourceProfile, input.Image = environmentRecord.Name, profileRecord.Name, "pytorch/pytorch:2.4.1-cuda12.1-cudnn9-runtime"
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("workspace Prepare()=%+v err=%v", prepared, err)
	}
	resource := prepared.Proposal.Resource
	if resource.ExecutionPolicy != "trusted_workspace" || resource.WorkspacePath != workspacePath || resource.NodeID != node.PublicID.String() || !resource.ImageMutable || resource.Image != input.Image ||
		len(resource.WorkspaceDatasets) != 1 || resource.WorkspaceDatasets[0].EnvironmentVariable != "GEMCP_DATASET_SCANOBJECTNN_OBJBG" {
		t.Fatalf("workspace resource=%+v", resource)
	}
	submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
	if err != nil {
		t.Fatal(err)
	}
	experimentRecord, err := f.client.Experiment.Query().Where().Only(ctx)
	if err != nil || experimentRecord.PublicID.String() != submitted.Experiment.ID || snapshotString(experimentRecord.EnvironmentSnapshot, "workspace_path") != workspacePath || snapshotString(experimentRecord.EnvironmentSnapshot, "image_uuid") != input.Image {
		t.Fatalf("workspace experiment=%+v err=%v", experimentRecord, err)
	}

	second, err := service.Prepare(ctx, f.principal, input)
	if err != nil || second.Proposal == nil {
		t.Fatalf("second Prepare()=%+v err=%v", second, err)
	}
	if _, err := access.Update().SetWorkspacePath("/home/campus.ncl.ac.uk/nxl51/other").Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{ProposalID: second.Proposal.ID, ConfirmationDigest: second.Proposal.ConfirmationDigest}); !errors.Is(err, ErrProposalChanged) {
		t.Fatalf("workspace path drift error=%v", err)
	}
	if _, err := access.Update().SetWorkspacePath(workspacePath).Save(ctx); err != nil {
		t.Fatal(err)
	}
	third, err := service.Prepare(ctx, f.principal, input)
	if err != nil || third.Proposal == nil {
		t.Fatalf("third Prepare()=%+v err=%v", third, err)
	}
	if _, err := dataset.Update().SetStatus("disabled").Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{ProposalID: third.Proposal.ID, ConfirmationDigest: third.Proposal.ConfirmationDigest}); !errors.Is(err, ErrProposalChanged) {
		t.Fatalf("workspace dataset drift error=%v", err)
	}
}

func TestPrepareRequiresFromNodeWhenStudyExistsAndSubmitBindsRun(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	researchService := mustResearchService(t, f)
	created, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "objbg-scan", "Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?"))
	if err != nil {
		t.Fatal(err)
	}
	service := preparedService(t, f, 2)
	service.SetGraphBinder(researchService)
	if _, err := service.Prepare(ctx, f.principal, validPrepare()); err == nil {
		t.Fatal("Prepare() accepted a Study Project without from_node_id")
	}
	if _, err := service.Prepare(ctx, f.principal, prepareFrom(created.Study.Nodes[0].ID)); err == nil {
		t.Fatal("Prepare() accepted a question node")
	}
	hypothesis, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "hypothesis", Title: "Background noise caps accuracy", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := prepareFrom(hypothesis.Study.Nodes[1].ID)
	input.ExpectedMetric = "overall_accuracy"
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || prepared.Proposal.FromNodeID != hypothesis.Study.Nodes[1].ID || prepared.Proposal.ExpectedMetric != "overall_accuracy" {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	listed, err := service.List(ctx, f.principal, ListInput{Limit: 10})
	if err != nil || len(listed.Experiments) != 0 {
		t.Fatalf("List() before submit = %+v, %v", listed, err)
	}
	submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
	if err != nil || submitted.RunNodeID == "" {
		t.Fatalf("SubmitPrepared() = %+v, %v", submitted, err)
	}
	workspace, err := researchService.AgentWorkspace(ctx, f.principal, research.WorkspaceInput{})
	if err != nil || workspace.Study == nil || len(workspace.Study.Nodes) != 3 || workspace.Study.Nodes[2].ExperimentID != submitted.Experiment.ID {
		t.Fatalf("bound workspace = %+v, %v", workspace.Study, err)
	}
	views, err := service.List(ctx, f.principal, ListInput{Limit: 10})
	if err != nil || len(views.Experiments) != 1 || !views.Experiments[0].GraphLinked || views.Experiments[0].Orphaned {
		t.Fatalf("List() after bind = %+v, %v", views, err)
	}
}

func TestCLIPathHypothesisProposalRunAndHighlight(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	researchService := mustResearchService(t, f)
	created, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "cli-path", "Can a CLI Agent stay on the Graph from hypothesis to highlight?"))
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "hypothesis", Title: "Noise caps accuracy", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := preparedService(t, f, 2)
	service.SetGraphBinder(researchService)
	input := prepareFrom(hypothesis.Study.Nodes[1].ID)
	input.ExpectedMetric = "overall_accuracy"
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
	if err != nil || submitted.RunNodeID == "" {
		t.Fatalf("SubmitPrepared() = %+v, %v", submitted, err)
	}
	record, err := service.getRecord(ctx, f.project.ID, submitted.Experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := record.Update().SetState("succeeded").SetMetrics(map[string]any{"overall_accuracy": 86.4}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	beforeClose, err := service.Get(ctx, f.principal, submitted.Experiment.ID)
	if err != nil || !beforeClose.ClosableRun || beforeClose.ExecutionContext.ExpectedMetric != "overall_accuracy" {
		t.Fatalf("closable Get() = %+v, %v", beforeClose, err)
	}
	metric := 86.4
	closed, err := researchService.AgentCloseRun(ctx, f.principal, research.CloseRunInput{
		ExperimentID: submitted.Experiment.ID, Title: "CLI smoke accuracy", Highlight: "Background noise still enters kNN",
		MetricName: "overall_accuracy", MetricValue: &metric,
	})
	if err != nil || closed.Study == nil || len(closed.Study.Hypotheses) != 1 {
		t.Fatalf("close_run workspace = %+v, %v", closed.Study, err)
	}
	rec := closed.Study.Hypotheses[0].Experiments
	if len(rec) != 1 || rec[0].Branch == "" || rec[0].CommitSHA == "" || rec[0].HighlightTitle != "Background noise still enters kNN" || rec[0].ResultTitle != "CLI smoke accuracy" {
		t.Fatalf("hypothesis records = %+v", rec)
	}
	if !containsActionKind(closed.NextActions, "record_decision") {
		t.Fatalf("next actions after highlight = %+v", closed.NextActions)
	}
	afterClose, err := service.Get(ctx, f.principal, submitted.Experiment.ID)
	if err != nil || afterClose.ClosableRun {
		t.Fatalf("closed Get() = %+v, %v", afterClose, err)
	}
}

func containsActionKind(actions []research.NextAction, kind string) bool {
	for _, action := range actions {
		if action.Kind == kind {
			return true
		}
	}
	return false
}

func TestPrepareRejectsIsolatedHypothesis(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	researchService := mustResearchService(t, f)
	if _, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "isolated", "Should a parentless hypothesis prepare?")); err != nil {
		t.Fatal(err)
	}
	isolated, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "hypothesis", Title: "Parentless claim"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fromID := ""
	for _, node := range isolated.Study.Nodes {
		if node.Kind == "hypothesis" && node.Title == "Parentless claim" {
			fromID = node.ID
		}
	}
	service := preparedService(t, f, 2)
	service.SetGraphBinder(researchService)
	_, err = service.Prepare(ctx, f.principal, prepareFrom(fromID))
	var validation *ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Message, "isolated") {
		t.Fatalf("Prepare() isolated hypothesis = %v", err)
	}
}

func TestPrepareRejectsPlanWithoutHypothesisAncestor(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	researchService := mustResearchService(t, f)
	created, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "plan-only", "Can a plan spend without a hypothesis?"))
	if err != nil {
		t.Fatal(err)
	}
	planned, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "plan", Title: "Plan under question", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	planID := ""
	for _, node := range planned.Study.Nodes {
		if node.Kind == "plan" {
			planID = node.ID
		}
	}
	service := preparedService(t, f, 2)
	service.SetGraphBinder(researchService)
	_, err = service.Prepare(ctx, f.principal, prepareFrom(planID))
	var validation *ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Message, "trace back to a hypothesis") {
		t.Fatalf("Prepare() hypothesis-less plan = %v", err)
	}
}

func TestSubmitPreparedRejectsDoomedGraphBindBeforeSpending(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	researchService := mustResearchService(t, f)
	created, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "preflight", "Does submit refuse to spend when the Graph bind is doomed?"))
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "hypothesis", Title: "Noise caps accuracy", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := preparedService(t, f, 2)
	service.SetGraphBinder(researchService)
	prepared, err := service.Prepare(ctx, f.principal, prepareFrom(hypothesis.Study.Nodes[1].ID))
	if err != nil || prepared.Proposal == nil {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	// The Study completes between prepare and submit: the bind is doomed, so
	// the submission must be rejected before any money is committed.
	if _, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Study: &research.StudyInput{ID: created.Study.ID, Name: created.Study.Name, Question: created.Study.Question, Status: "archived"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
	if err == nil || !strings.Contains(err.Error(), "active Study") {
		t.Fatalf("doomed bind submit error = %v", err)
	}
	if count, _ := f.client.Experiment.Query().Count(ctx); count != 0 {
		t.Fatalf("doomed submit still created an Experiment, count=%d", count)
	}
	if count, _ := f.client.BudgetEntry.Query().Count(ctx); count != 0 {
		t.Fatalf("doomed submit still reserved budget, count=%d", count)
	}
	// Reactivating the Study lets the same proposal submit and bind normally.
	if _, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Study: &research.StudyInput{ID: created.Study.ID, Name: created.Study.Name, Question: created.Study.Question, Status: "active"},
	}); err != nil {
		t.Fatal(err)
	}
	submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest})
	if err != nil || submitted.RunNodeID == "" || submitted.GraphBindWarning != "" {
		t.Fatalf("reactivated submit = %+v, %v", submitted, err)
	}
}

func TestBindPreparedGraphWarnsInsteadOfFailingAfterCommit(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	ctx := context.Background()
	researchService := mustResearchService(t, f)
	created, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "bind-race", "What happens when the bind fails after the Experiment committed?"))
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "hypothesis", Title: "Noise caps accuracy", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := preparedService(t, f, 2)
	service.SetGraphBinder(researchService)
	repositoryRecord, err := f.client.Repository.Query().First(ctx)
	if err != nil {
		t.Fatal(err)
	}
	environmentRecord, err := f.client.Environment.Query().First(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profileRecord, err := f.client.ResourceProfile.Query().First(ctx)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := f.client.Experiment.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).SetAgentTokenID(f.principal.TokenID).
		SetRepositoryID(repositoryRecord.ID).SetEnvironmentID(environmentRecord.ID).SetResourceProfileID(profileRecord.ID).
		SetCommitSha("0123456789012345678901234567890123456789").SetCommand("python train.py").
		SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the race where the Study completes after the Experiment and
	// budget committed but before the Graph bind ran.
	if _, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Study: &research.StudyInput{ID: created.Study.ID, Name: created.Study.Name, Question: created.Study.Question, Status: "archived"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := service.bindPreparedGraph(ctx, f.principal, map[string]any{"from_node_id": hypothesis.Study.Nodes[1].ID}, SubmitPreparedResult{Experiment: makeView(orphan)})
	if err != nil {
		t.Fatalf("bind failure after commit must not surface as a submit error: %v", err)
	}
	if result.GraphBindWarning == "" || !strings.Contains(result.GraphBindWarning, "Do not prepare again") || result.RunNodeID != "" {
		t.Fatalf("bind failure after commit = %+v", result)
	}
}

func prepareFrom(fromNodeID string) PrepareInput {
	input := validPrepare()
	input.FromNodeID = fromNodeID
	return input
}

func researchUpdate(_ fixture, name, question string) research.UpdateInput {
	return research.UpdateInput{Study: &research.StudyInput{Name: name, Question: question}}
}

func mustResearchService(t *testing.T, f fixture) *research.Service {
	t.Helper()
	return research.NewService(f.client)
}

func TestPreparedSSHCloudPassesOnLoopbackWhileAutoDLRequiresHTTPS(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, nil)
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	loopbackStatus := func() execution.RuntimeStatus {
		return execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, WatchdogHealthy: false, PublicURLConfigured: true,
			PublicURLHTTPS: false, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}
	}
	sshService := NewService(f.client, f.box, git, WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: loopbackStatus()}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	input := validPrepare()
	input.Environment, input.ResourceProfile = environmentName, profileName
	prepared, err := sshService.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("Cloud SSH Prepare()=%+v err=%v", prepared, err)
	}
	autodlService := NewService(f.client, f.box, git, WithPreparedExperiments(
		git, git, proposalProvider{idle: 2}, proposalStatusRuntime{status: loopbackStatus()}, ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	autodlInput := validPrepare()
	autodlPrepared, err := autodlService.Prepare(ctx, f.principal, autodlInput)
	if err != nil || autodlPrepared.Proposal == nil || autodlPrepared.Proposal.Eligible {
		t.Fatalf("AutoDL loopback Prepare()=%+v err=%v", autodlPrepared, err)
	}
	failed := false
	for _, check := range autodlPrepared.Proposal.Checks {
		if check.ID == "public_url" && check.Status == ProposalCheckFail {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("AutoDL proposal did not fail public_url on loopback HTTP: %+v", autodlPrepared.Proposal.Checks)
	}
}

func TestSSHCloudHostProcessCloseRunWithoutInventedSHA(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, map[string]any{"os": "Linux"})
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	controller := &fakeSSHCloudController{result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	researchService := mustResearchService(t, f)
	service.SetGraphBinder(researchService)
	created, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "cpu-loop", "Can a CPU host process close without a git SHA?"))
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := researchService.AgentUpdate(ctx, f.principal, research.UpdateInput{
		Node: &research.NodeInput{Kind: "hypothesis", Title: "Host process can close", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := researchService.AgentUpdate(ctx, f.principal, researchUpdate(f, "cpu-loop leftover", "A second Study left from an earlier loop.")); err != nil {
		t.Fatal(err)
	}
	input := prepareFrom(hypothesis.Study.Nodes[1].ID)
	input.ExpectedMetric = "overall_accuracy"
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("Prepare() = %+v, %v", prepared, err)
	}
	if prepared.Proposal.Repository.CommitSHA != "" || prepared.Proposal.Repository.RequestedRef != "" {
		t.Fatalf("prepared host proposal leaked sentinel git fields: %+v", prepared.Proposal.Repository)
	}
	submitted, err := service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{
		ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest,
	})
	if err != nil || submitted.Experiment.ID == "" || submitted.RunNodeID == "" {
		t.Fatalf("SubmitPrepared() = %+v, %v", submitted, err)
	}
	record, err := service.getRecord(ctx, f.project.ID, submitted.Experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.CommitSha != sshcloud.HostCommit {
		t.Fatalf("stored host commit_sha = %q", record.CommitSha)
	}
	if _, err := record.Update().SetState("succeeded").SetMetrics(map[string]any{"overall_accuracy": 0.75}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := service.Get(ctx, f.principal, submitted.Experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CommitSHA != "" {
		t.Fatalf("get_experiment commit_sha = %q", got.CommitSHA)
	}
	if got.StudyID != created.Study.ID {
		t.Fatalf("get_experiment study_id = %q want %q", got.StudyID, created.Study.ID)
	}
	if got.ExecutionContext.RequestedRef != "" {
		t.Fatalf("get_experiment requested_ref = %q", got.ExecutionContext.RequestedRef)
	}
	if !got.ClosableRun {
		t.Fatalf("closable Get() = %+v", got)
	}
	closed, err := researchService.AgentCloseRun(ctx, f.principal, research.CloseRunInput{
		ExperimentID: submitted.Experiment.ID,
		Title:        "ModelNet40-mini CPU fixture finished",
		Summary:      "Copied overall_accuracy from the scraped metrics.json.",
		Status:       "succeeded",
	})
	if err != nil || closed.Study == nil || closed.Study.ID != created.Study.ID {
		t.Fatalf("close_run = %+v, %v", closed.Study, err)
	}
	var result research.NodeView
	var highlight bool
	for _, node := range closed.Study.Nodes {
		if node.Kind == "result" {
			result = node
		}
		if node.Kind == "observation" && node.Title == "ModelNet40-mini CPU fixture finished" {
			highlight = true
			if node.CommitSHA != "" {
				t.Fatalf("highlight copied host sentinel commit_sha = %q", node.CommitSHA)
			}
		}
	}
	if result.Kind != "result" || result.CommitSHA != "" || result.MetricName != "overall_accuracy" {
		t.Fatalf("closed result = %+v", result)
	}
	if !highlight {
		t.Fatalf("closed workspace missing highlight: %+v", closed.Study)
	}
}

func TestPrepareSSHCloudWithoutImageOrRepository(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, map[string]any{"os": "Linux"})
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	controller := &fakeSSHCloudController{result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible || prepared.Proposal.Resource.Backend != "ssh_cloud" {
		t.Fatalf("Prepare()=%+v err=%v", prepared, err)
	}
	if controller.calls != 1 || controller.lastImage != sshcloud.HostImage || prepared.Proposal.Resource.Image != sshcloud.HostImage {
		t.Fatalf("controller=%+v image=%s", controller, prepared.Proposal.Resource.Image)
	}
	if prepared.Proposal.Repository.ID != "" || prepared.Proposal.Resource.Host != "203.0.113.10" ||
		prepared.Proposal.Resource.User != "ubuntu" || prepared.Proposal.Resource.WorkingDirectory != "$HOME" ||
		prepared.Proposal.Resource.Isolation != "none" {
		t.Fatalf("resource=%+v repository=%+v", prepared.Proposal.Resource, prepared.Proposal.Repository)
	}
}

func TestPrepareSSHCloudPinsHostUserCwdArgv(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, nil)
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	controller := &fakeSSHCloudController{result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	home := validPrepare()
	homePrepared, err := service.Prepare(ctx, f.principal, home)
	if err != nil || homePrepared.Proposal == nil || !homePrepared.Proposal.Eligible {
		t.Fatalf("home Prepare()=%+v err=%v", homePrepared, err)
	}
	cwd := validPrepare()
	cwd.Cwd = "/opt/exp"
	cwd.Image = "pytorch/pytorch@sha256:" + strings.Repeat("e", 64)
	cwdPrepared, err := service.Prepare(ctx, f.principal, cwd)
	if err != nil || cwdPrepared.Proposal == nil || !cwdPrepared.Proposal.Eligible {
		t.Fatalf("cwd Prepare()=%+v err=%v", cwdPrepared, err)
	}
	if cwdPrepared.Proposal.Resource.WorkingDirectory != "/opt/exp" || cwdPrepared.Proposal.Resource.Image != sshcloud.HostImage {
		t.Fatalf("cwd resource=%+v", cwdPrepared.Proposal.Resource)
	}
	if cwdPrepared.Proposal.ConfirmationDigest == homePrepared.Proposal.ConfirmationDigest {
		t.Fatal("cwd did not change the confirmation digest")
	}
}

func TestPrepareSSHCloudSucceedsWithoutGPU(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, map[string]any{"os": "Linux", "nvidia_ready": false})
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	controller := &fakeSSHCloudController{result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage, NvidiaReady: false,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("Prepare()=%+v err=%v", prepared, err)
	}
}

func TestPrepareSSHCloudRejectsRelativeCwd(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, nil)
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	controller := &fakeSSHCloudController{result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	input := validPrepare()
	input.Cwd = "relative/path"
	_, err := service.Prepare(ctx, f.principal, input)
	var validation *ValidationError
	if err == nil || !errors.As(err, &validation) || !strings.Contains(validation.Message, "absolute") {
		t.Fatalf("Prepare() err=%v", err)
	}
}

func createSSHCloudHost(t *testing.T, f fixture, inventory map[string]any) *ent.CloudSSHNode {
	t.Helper()
	if inventory == nil {
		inventory = map[string]any{"os": "Linux"}
	}
	node, err := f.client.CloudSSHNode.Create().SetTenantID(f.principal.TenantID).SetLabel("gpu-cloud-1").
		SetSSHHost("203.0.113.10").SetSSHUser("ubuntu").SetAuthMethod("password").SetCredentialCiphertext("v1.not-used").
		SetStatus("active").SetInventory(inventory).Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.CloudSSHProjectAccess.Create().SetTenantID(f.principal.TenantID).SetNodeID(node.ID).SetProjectID(f.project.ID).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	return node
}

func createSSHCloudRuntime(t *testing.T, f fixture, node *ent.CloudSSHNode) (string, string) {
	t.Helper()
	name := "ssh-cloud-node"
	if _, err := f.client.Environment.Create().SetProjectID(f.project.ID).SetBackend("ssh_cloud").SetName(name).
		SetImageUUID(sshcloud.HostImage).SetRecipeRef("ssh-cloud:" + node.PublicID.String()).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.ResourceProfile.Create().SetProjectID(f.project.ID).SetBackend("ssh_cloud").SetName(name).
		SetRegion("ssh_cloud").SetGpuNames([]string{"NVIDIA GeForce RTX 4090"}).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).
		SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(0).SetPriceToMilli(0).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	return name, name
}

type proposalStatusRuntime struct {
	status execution.RuntimeStatus
}

func (r proposalStatusRuntime) Status(context.Context) (execution.RuntimeStatus, error) {
	return r.status, nil
}

func TestPreparedExperimentRejectsUnknownAndOversizedPresets(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	service := preparedService(t, f, 2)
	ctx := context.Background()
	input := validPrepare()
	input.RuntimePreset = "full"
	if _, err := service.Prepare(ctx, f.principal, input); err == nil || !strings.Contains(err.Error(), "runtime_preset must be smoke, probe, train, or provision") {
		t.Fatalf("unknown preset error = %v", err)
	}
	input = validPrepare()
	input.MaxRuntimeSeconds = 301
	if _, err := service.Prepare(ctx, f.principal, input); err == nil || !strings.Contains(err.Error(), "max_runtime_seconds must be between 1 and 300") {
		t.Fatalf("oversized smoke error = %v", err)
	}
}

func TestPreparedTrainRequiresDatasetBindingAndOwnerCanConfirm(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	service := preparedService(t, f, 2)
	ctx := context.Background()
	input := validPrepare()
	input.RuntimePreset = "train"
	input.MaxRuntimeSeconds = 3600
	blocked, err := service.Prepare(ctx, f.principal, input)
	if err != nil || blocked.Proposal == nil || blocked.Proposal.Eligible {
		t.Fatalf("train without binding Prepare() = %+v, %v", blocked, err)
	}
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("scanobjectnn-objbg").SetBackend("autodl_private").
		SetCanonicalRoot("/root/autodl-fs/datasets/ScanObjectNN").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").
		SetRequiredMarkers([]string{"main_split/train.h5"}).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible || prepared.Proposal.RuntimePreset != "train" ||
		prepared.Proposal.MaxRuntimeSeconds != 3600 || len(prepared.Proposal.Resource.DatasetBindings) != 1 {
		t.Fatalf("train with binding Prepare() = %+v, %v", prepared, err)
	}
	if _, err := service.OwnerSubmitPrepared(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), prepared.Proposal.ID, OwnerSubmitPreparedInput{
		ConfirmationDigest: prepared.Proposal.ConfirmationDigest,
	}); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed owner submit error = %v", err)
	}
	result, err := service.OwnerSubmitPrepared(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), prepared.Proposal.ID, OwnerSubmitPreparedInput{
		ConfirmationDigest: prepared.Proposal.ConfirmationDigest, Confirmed: true,
	})
	if err != nil || result.Experiment.ID == "" || result.Experiment.MaxRuntimeSeconds != 3600 {
		t.Fatalf("OwnerSubmitPrepared() = %+v, %v", result, err)
	}
}

func TestPreparedProvisionRequiresSourcesAndOmitsArgv(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	service := preparedService(t, f, 2)
	ctx := context.Background()
	input := PrepareInput{RuntimePreset: "provision"}
	blocked, err := service.Prepare(ctx, f.principal, input)
	if err != nil || blocked.Proposal == nil || blocked.Proposal.Eligible {
		t.Fatalf("provision without binding Prepare() = %+v, %v", blocked, err)
	}
	binding, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("scanobjectnn-objbg").SetBackend("autodl_private").
		SetCanonicalRoot("/root/autodl-fs/datasets/ScanObjectNN").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").
		SetRequiredMarkers([]string{"main_split/train.h5"}).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stillBlocked, err := service.Prepare(ctx, f.principal, input)
	if err != nil || stillBlocked.Proposal == nil || stillBlocked.Proposal.Eligible {
		t.Fatalf("provision without sources Prepare() = %+v, %v", stillBlocked, err)
	}
	if _, err := binding.Update().
		SetSources([]map[string]string{{
			"url": "https://huggingface.co/datasets/example/resolve/main/train.h5", "relative_path": "main_split/train.h5",
		}}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Prepare(ctx, f.principal, PrepareInput{RuntimePreset: "provision", Argv: []string{"python", "fetch.py"}}); err == nil || !strings.Contains(err.Error(), "omit argv") {
		t.Fatalf("provision with argv error = %v", err)
	}
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible || prepared.Proposal.RuntimePreset != "provision" ||
		len(prepared.Proposal.Execution.Argv) != 1 || prepared.Proposal.Execution.Argv[0] != "gemcp-dataset-provision" ||
		len(prepared.Proposal.Resource.DatasetBindings) != 1 || len(prepared.Proposal.Resource.DatasetBindings[0].Sources) != 1 {
		t.Fatalf("provision Prepare() = %+v, %v", prepared, err)
	}
}

func TestPreparedSSHCloudInjectsDatasetBindings(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, nil)
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("scanobjectnn-objbg").SetBackend("ssh_cloud").
		SetCanonicalRoot("/root/data/ScanObjectNN").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	controller := &fakeSSHCloudController{result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible || len(prepared.Proposal.Resource.DatasetBindings) != 1 {
		t.Fatalf("SSH Prepare() = %+v, %v", prepared, err)
	}
	if prepared.Proposal.Resource.DatasetBindings[0].CanonicalRoot != "/root/data/ScanObjectNN" {
		t.Fatalf("SSH dataset binding = %+v", prepared.Proposal.Resource.DatasetBindings)
	}
}

func TestPreparedSSHCloudSubmitKeepsCwdAndDatasetFilter(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	node := createSSHCloudHost(t, f, map[string]any{"os": "Linux", "nvidia_ready": false})
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("modelnet40-mini").SetBackend("ssh_cloud").
		SetCanonicalRoot("/home/ubuntu/gemcp/datasets/modelnet40-mini").
		SetEnvironmentVariable("GEMCP_DATASET_MODELNET40_MINI").
		SetRequiredMarkers([]string{"meta.json", "train/chair/0001.off"}).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("scanobjectnn-objbg").SetBackend("ssh_cloud").
		SetCanonicalRoot("/home/ubuntu/data/ScanObjectNN").
		SetEnvironmentVariable("GEMCP_DATASET_SCANOBJECTNN_OBJBG").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	controller := &fakeSSHCloudController{result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	input := PrepareInput{
		Argv: []string{"python3", "/home/ubuntu/gemcp/datasets/modelnet40-mini/train.py"},
		Cwd:  "/home/ubuntu/gemcp/datasets/modelnet40-mini", RuntimePreset: "smoke",
		Dataset: "modelnet40-mini", Environment: environmentName, ResourceProfile: profileName,
	}
	prepared, err := service.Prepare(ctx, f.principal, input)
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("Prepare()=%+v err=%v", prepared, err)
	}
	if prepared.Proposal.Resource.WorkingDirectory != input.Cwd || prepared.Proposal.Resource.Host != "203.0.113.10" ||
		len(prepared.Proposal.Resource.DatasetBindings) != 1 || prepared.Proposal.Resource.DatasetBindings[0].Name != "modelnet40-mini" {
		t.Fatalf("prepared resource=%+v", prepared.Proposal.Resource)
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
		t.Fatalf("currentProposal digest drifted prepared=%s reloaded=%s cwd=%q host=%q bindings=%d",
			prepared.Proposal.ConfirmationDigest, proposalDigest(reloaded), reloaded.cwd, reloaded.sshHost, len(reloaded.bindings))
	}
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("extra-local").SetBackend("ssh_cloud").
		SetCanonicalRoot("/home/ubuntu/extra").
		SetEnvironmentVariable("GEMCP_DATASET_EXTRA_LOCAL").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	submitted, err := service.OwnerSubmitPrepared(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), prepared.Proposal.ID, OwnerSubmitPreparedInput{
		ConfirmationDigest: prepared.Proposal.ConfirmationDigest, Confirmed: true,
	})
	if err != nil || submitted.Experiment.ID == "" || submitted.Experiment.State == "" {
		t.Fatalf("OwnerSubmitPrepared()=%+v err=%v", submitted, err)
	}
}

func TestPreparedExperimentRejectsPolicyUpdateAfterPrepare(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	service := preparedService(t, f, 2)
	ctx := context.Background()
	prepared, err := service.Prepare(ctx, f.principal, validPrepare())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.project.Update().SetMaxRuntimeSeconds(7200).Save(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = service.SubmitPrepared(ctx, f.principal, SubmitPreparedInput{
		ProposalID: prepared.Proposal.ID, ConfirmationDigest: prepared.Proposal.ConfirmationDigest,
	})
	if !errors.Is(err, ErrProposalChanged) {
		t.Fatalf("policy drift submit error = %v", err)
	}
}

func TestPreparedLocalCPUFailsClosedOnMissingMarkers(t *testing.T) {
	f := newFixture(t, 1, 1)
	ctx := context.Background()
	root := t.TempDir()
	node, err := f.client.CloudSSHNode.Create().SetTenantID(f.principal.TenantID).SetLabel("local-cpu").
		SetSSHHost("127.0.0.1").SetSSHUser("ubuntu").SetAuthMethod("password").SetCredentialCiphertext("v1.not-used").
		SetStatus("active").SetInventory(map[string]any{"os": "Linux"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.CloudSSHProjectAccess.Create().SetTenantID(f.principal.TenantID).SetNodeID(node.ID).SetProjectID(f.project.ID).Save(ctx); err != nil {
		t.Fatal(err)
	}
	environmentName, profileName := createSSHCloudRuntime(t, f, node)
	if _, err := f.client.DatasetBinding.Create().
		SetTenantID(f.principal.TenantID).SetProjectID(f.project.ID).
		SetName("modelnet40-mini").SetBackend("ssh_cloud").
		SetCanonicalRoot(root).
		SetEnvironmentVariable("GEMCP_DATASET_MODELNET40_MINI").
		SetRequiredMarkers([]string{"meta.json"}).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	controller := &fakeSSHCloudController{localProcess: true, result: sshcloud.EnsureResult{
		EnvironmentName: environmentName, ProfileName: profileName, ResolvedImage: sshcloud.HostImage,
	}}
	git := &proposalGit{resolved: proposalCommit, archive: proposalArchive(t)}
	service := NewService(f.client, f.box, git, WithSSHCloud(controller), WithPreparedExperiments(
		git, git, nil, proposalStatusRuntime{status: execution.RuntimeStatus{
			SchedulerEnabled: true, SchedulerHealthy: true, PublicURLConfigured: true, SSHCloudEnabled: true, GlobalConcurrency: 2,
		}}, ProposalConfig{SourceMaxBytes: 1 << 20, SSHCloudEnabled: true},
	))
	blocked, err := service.Prepare(ctx, f.principal, PrepareInput{
		Argv: []string{"python3", "train.py"}, Environment: environmentName, ResourceProfile: profileName,
	})
	if err != nil || blocked.Proposal == nil || blocked.Proposal.Eligible {
		t.Fatalf("missing marker Prepare()=%+v err=%v", blocked, err)
	}
	found := false
	for _, check := range blocked.Proposal.Checks {
		if check.ID == "required_markers" && check.Status == ProposalCheckFail && strings.Contains(check.Detail, "meta.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("checks=%+v", blocked.Proposal.Checks)
	}
	if err := os.WriteFile(filepath.Join(root, "meta.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(ctx, f.principal, PrepareInput{
		Argv: []string{"python3", "train.py"}, Environment: environmentName, ResourceProfile: profileName,
	})
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("present marker Prepare()=%+v err=%v", prepared, err)
	}
}

func TestOwnerPrepareCreatesProposalWithoutAgentToken(t *testing.T) {
	f := newFixture(t, 100000, 20000)
	service := preparedService(t, f, 2)
	ctx := context.Background()
	prepared, err := service.OwnerPrepare(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), validPrepare())
	if err != nil || prepared.Proposal == nil || !prepared.Proposal.Eligible {
		t.Fatalf("OwnerPrepare() = %+v, %v", prepared, err)
	}
	publicID, err := uuid.Parse(prepared.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.client.ExperimentProposal.Query().Where(experimentproposal.PublicIDEQ(publicID)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.AgentTokenID != nil {
		t.Fatalf("owner proposal stored agent_token_id %v", *record.AgentTokenID)
	}
	submitted, err := service.OwnerSubmitPrepared(ctx, f.principal.TenantID, "owner-1", f.project.PublicID.String(), prepared.Proposal.ID, OwnerSubmitPreparedInput{
		ConfirmationDigest: prepared.Proposal.ConfirmationDigest, Confirmed: true,
	})
	if err != nil || submitted.Experiment.ID == "" {
		t.Fatalf("OwnerSubmitPrepared() = %+v, %v", submitted, err)
	}
}
