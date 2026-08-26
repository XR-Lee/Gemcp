package experiment

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
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
	if _, err := service.Prepare(ctx, f.principal, input); err == nil || !strings.Contains(err.Error(), "runtime_preset must be smoke, probe, or train") {
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
