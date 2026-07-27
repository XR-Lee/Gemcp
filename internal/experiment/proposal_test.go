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

	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
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
	idle int
	err  error
}

func (p proposalProvider) QueryResources(context.Context, int) (provider.ResourceSnapshot, error) {
	if p.err != nil {
		return provider.ResourceSnapshot{}, p.err
	}
	return provider.ResourceSnapshot{
		Provider:      provider.Summary{Name: "private", Status: "active"},
		GPUStock:      []provider.GPUStock{{Name: "RTX 4090", Idle: p.idle, Total: 2}},
		PrivateImages: []provider.Image{{UUID: "image-uuid", Name: "image", Source: "private"}},
	}, nil
}

type proposalRuntime struct {
	healthy    bool
	selfHosted bool
}

func (r proposalRuntime) Status(context.Context) (execution.RuntimeStatus, error) {
	return execution.RuntimeStatus{
		SchedulerEnabled: r.healthy, SchedulerHealthy: r.healthy, WatchdogHealthy: r.healthy,
		PublicURLConfigured: r.healthy, SelfHostedEnabled: r.selfHosted, GlobalConcurrency: 2,
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
	t.Run("expired", func(t *testing.T) {
		f := newFixture(t, 100000, 20000)
		service := preparedService(t, f, 2)
		base := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
		service.now = func() time.Time { return base }
		prepared, err := service.Prepare(context.Background(), f.principal, validPrepare())
		if err != nil {
			t.Fatal(err)
		}
		service.now = func() time.Time { return base.Add(31 * time.Minute) }
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
