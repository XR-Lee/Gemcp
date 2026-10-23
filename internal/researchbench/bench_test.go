package researchbench

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/mcpserver"
	"github.com/XR-Lee/Gemcp/internal/provider"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// The benchmark runs the production MCP server, Graph binder, and prepared
// proposal path in process. Only Git, the GPU provider, and the scheduler are
// stand-ins: the commit archive is examples/research-bench, and simLab plays
// the Owner and the backend.
//
//	GEMCP_BENCH_OUT=dir         write <label>.json and <label>.md there
//	GEMCP_BENCH_LABEL=name      label for this pass (default "current")
//	GEMCP_BENCH_MEASURE_ONLY=1  report without asserting, for older builds

const (
	benchCommit   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	capacityLimit = 60
)

type benchGit struct{ archive []byte }

func (benchGit) VerifyCommit(context.Context, int, string) error { return nil }

func (benchGit) ResolveRef(context.Context, int, string) (string, error) { return benchCommit, nil }

func (g benchGit) ArchiveCommit(context.Context, int, string, int64) (gitrepository.Archive, error) {
	return &benchArchive{data: g.archive}, nil
}

func (benchGit) DetectDefaultBranch(context.Context, int) (string, string, error) {
	return "main", benchCommit, nil
}

type benchArchive struct{ data []byte }

func (a *benchArchive) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.data)), nil
}
func (a *benchArchive) Close() error     { return nil }
func (a *benchArchive) SizeBytes() int64 { return int64(len(a.data)) }

type benchProvider struct{}

func (benchProvider) QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error) {
	return provider.ResourceSnapshot{
		Provider:      provider.Summary{Name: "private", Backend: "private", Status: "active"},
		GPUStock:      []provider.GPUStock{{Name: "RTX 4090", Idle: 2, Total: 2}},
		PrivateImages: []provider.Image{{UUID: "image-uuid", Name: "image", Source: "private"}},
	}, nil
}

type benchRuntime struct{}

func (benchRuntime) Status(context.Context) (execution.RuntimeStatus, error) {
	return execution.RuntimeStatus{
		SchedulerEnabled: true, SchedulerHealthy: true, WatchdogHealthy: true,
		PublicURLConfigured: true, PublicURLHTTPS: true, GlobalConcurrency: 2,
	}, nil
}

// simLab is the Owner and the backend. The Owner confirms a proposal only
// while the run budget lasts and only when the command it shows is exactly the
// workload run it claims to be; the backend runs the task simulator and
// stores the outcome the way the scheduler would.
type simLab struct {
	client   *ent.Client
	budget   int
	approved int
}

func (l *simLab) Approve(_ context.Context, task Task, proposal map[string]any) error {
	if l.approved >= l.budget {
		return ErrBudget
	}
	if !flag(proposal, "eligible") {
		return fmt.Errorf("proposal is not eligible: %v", field(proposal, "checks"))
	}
	proposed, params, err := ParseArgv(textList(proposal, "execution", "argv"))
	if err != nil {
		return err
	}
	if proposed.ID != task.ID || text(proposal, "workload") != task.ID {
		return fmt.Errorf("proposal runs %s through workload %q, want %s", proposed.ID, text(proposal, "workload"), task.ID)
	}
	if got, want := text(proposal, "execution", "display_command"), strings.Join(task.Argv(params), " "); got != want {
		return fmt.Errorf("Owner sees %q, but the workload runs %q", got, want)
	}
	l.approved++
	return nil
}

func (l *simLab) Execute(ctx context.Context, experimentID string) error {
	publicID, err := uuid.Parse(experimentID)
	if err != nil {
		return err
	}
	record, err := l.client.Experiment.Query().Where(entexperiment.PublicIDEQ(publicID)).Only(ctx)
	if err != nil {
		return err
	}
	task, params, err := ParseArgv(record.Argv)
	if err != nil {
		return err
	}
	outcome := task.Simulate(params)
	now := time.Now().UTC()
	update := record.Update().SetStartedAt(now).SetFinishedAt(now)
	log := fmt.Sprintf("%s starting seed=%d\n", task.ID, params["seed"])
	if outcome.Crashed {
		update.SetState("failed").SetExitCode(137).SetFailureCode("process_exit").
			SetFailureReason("exit status 137").SetLogTail(log + "RuntimeError: CUDA out of memory (simulated)\n")
	} else {
		metrics := map[string]any{}
		for name, value := range outcome.Metrics {
			metrics[name] = value
		}
		encoded, _ := json.Marshal(metrics)
		update.SetState("succeeded").SetExitCode(0).SetMetrics(metrics).
			SetLogTail(log + task.ID + " wrote " + string(encoded) + "\n")
	}
	_, err = update.Save(ctx)
	return err
}

type bench struct {
	client *ent.Client
	tools  Tools
}

func newBench(t *testing.T) *bench {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, dialect.SQLite, "file:research-bench-"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	tenant, err := client.Tenant.Create().SetName("Research bench").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Research bench").SetSlug("research-bench").
		SetMonthlyBudgetMilli(100_000_000).SetMaxExperimentMilli(20000).
		SetMaxRuntimeSeconds(3600).SetTimeoutExtensionSeconds(3600).SetTerminationGraceSeconds(60).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Repository.Create().
		SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").
		SetSSHHost("github.com").SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Environment.Create().
		SetProjectID(project.ID).SetName("default").SetImageUUID("image-uuid").SetIsDefault(true).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResourceProfile.Create().
		SetProjectID(project.ID).SetName("default").SetRegion("private").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(118).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
		SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx); err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	rawToken, prefix, err := secrets.RandomToken("gmc", 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("research-bench").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).
		SetScopes([]string{"read", "submit", "cancel"}).Save(ctx); err != nil {
		t.Fatal(err)
	}

	git := benchGit{archive: benchSourceArchive(t)}
	experiments := experiment.NewService(client, box, git, experiment.WithPreparedExperiments(
		git, git, benchProvider{}, benchRuntime{}, experiment.ProposalConfig{SourceMaxBytes: 1 << 20},
	))
	researchService := research.NewService(client)
	experiments.SetGraphBinder(researchService)
	handler := mcpserver.New(
		agentauth.NewService(client, box), experiments, "research-bench", nil, mcpserver.WithResearch(researchService),
	).Handler()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	session, err := Connect(ctx, server.URL, rawToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return &bench{client: client, tools: MCPTools{Session: session}}
}

// benchSourceArchive packs examples/research-bench like a GitHub commit
// archive, so prepare_experiment resolves the real gemcp.yaml.
func benchSourceArchive(t *testing.T) []byte {
	t.Helper()
	var result bytes.Buffer
	gzipWriter := gzip.NewWriter(&result)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": benchCommit},
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gemcp.yaml", "bench_task.py"} {
		content, err := os.ReadFile(filepath.Join("..", "..", "examples", "research-bench", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := tarWriter.WriteHeader(&tar.Header{Name: "repo/" + name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestResearchBench(t *testing.T) {
	ctx := context.Background()
	b := newBench(t)
	label := os.Getenv("GEMCP_BENCH_LABEL")
	if label == "" {
		label = "current"
	}
	report := Report{Label: label}
	for _, policy := range []Policy{Careful{}, Grid{}, Hasty{}} {
		for _, task := range Tasks() {
			episode, err := Play(ctx, b.tools, &simLab{client: b.client, budget: task.Budget}, policy, task)
			if err != nil {
				t.Fatalf("%s/%s: %v", policy.Name(), task.ID, err)
			}
			score, err := ScoreStudy(ctx, b.tools, task, episode.StudyID)
			if err != nil {
				t.Fatalf("score %s/%s: %v", policy.Name(), task.ID, err)
			}
			score.AddEpisode(episode)
			report.Scores = append(report.Scores, score)
		}
	}
	integrity, err := ProbeIntegrity(ctx, b.tools, &simLab{client: b.client, budget: 2})
	if err != nil {
		t.Fatalf("integrity probe: %v", err)
	}
	if err := ProbeCapacity(ctx, b.tools, &simLab{client: b.client, budget: capacityLimit}, capacityLimit, &integrity); err != nil {
		t.Fatalf("capacity probe: %v", err)
	}
	report.Integrity = integrity

	markdown := RenderMarkdown(report)
	t.Log("\n" + markdown)
	if dir := os.Getenv("GEMCP_BENCH_OUT"); dir != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, label+".json"), encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, label+".md"), []byte(markdown), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("GEMCP_BENCH_MEASURE_ONLY") != "" {
		return
	}
	assertBench(t, report)
}

// assertBench holds Gemcp to a faithful record for every Agent, good or bad,
// and checks that the benchmark still tells a careful Agent from a careless
// one.
func assertBench(t *testing.T, report Report) {
	t.Helper()
	summaries := report.Summaries()
	byAgent := map[string]Summary{}
	for _, summary := range summaries {
		byAgent[summary.Agent] = summary
	}
	all := byAgent["all"]
	for name, got := range map[string][2]int{
		"lab mismatches":                 {all.LabMismatches, 0},
		"unclosed runs":                  {all.Runs - all.Closed, 0},
		"result metric mismatches":       {all.MetricMismatches, 0},
		"wrong run verdicts":             {all.Runs - all.VerdictCorrect, 0},
		"false supports":                 {all.FalseSupports, 0},
		"crashes recorded as evidence":   {all.CrashAsEvidence, 0},
		"conflicting verdicts":           {all.Conflicts, 0},
		"lost hypothesis criteria":       {all.Episodes - all.CriterionKept, 0},
		"numbers with wrong provenance":  {all.MetricNodes - all.ProvenanceCorrect, 0},
		"incomplete Owner cards":         {all.Runs - all.OwnerRecords, 0},
		"failed report checks":           {all.ReportChecks - all.ReportPassed, 0},
		"misleading crash prompts":       {all.MisleadingPrompts, 0},
		"verdict workarounds":            {all.LegacyVerdicts, 0},
		"evidence rewrites that took":    {report.Integrity.Effective(), 0},
		"runs spent without a Graph run": {report.Integrity.Orphaned, 0},
		"out-of-range proposals":         {boolCount(report.Integrity.OutOfBounds), 0},
	} {
		if got[0] != got[1] {
			t.Errorf("%s = %d, want %d", name, got[0], got[1])
		}
	}
	if all.Crashes == 0 || all.MetricNodes == 0 || all.ReportChecks == 0 {
		t.Errorf("benchmark exercised too little: crashes=%d metric nodes=%d report checks=%d", all.Crashes, all.MetricNodes, all.ReportChecks)
	}
	careful, grid, hasty := byAgent["careful"], byAgent["grid"], byAgent["hasty"]
	if careful.ConclusionsCorrect != careful.Episodes || careful.SearchSolved != careful.SearchEpisodes {
		t.Errorf("careful Agent: %d/%d conclusions, %d/%d searches solved", careful.ConclusionsCorrect, careful.Episodes, careful.SearchSolved, careful.SearchEpisodes)
	}
	if grid.SearchSolved >= careful.SearchSolved {
		t.Errorf("grid Agent solved %d searches, careful %d: the benchmark no longer rewards efficient search", grid.SearchSolved, careful.SearchSolved)
	}
	if hasty.ConclusionsCorrect >= hasty.Episodes {
		t.Errorf("hasty Agent got every conclusion right: the benchmark no longer punishes one-run conclusions")
	}
	// The full Study must refuse at prepare, before the Owner is asked to
	// confirm a run the Graph cannot bind.
	if !report.Integrity.CapacityHit || report.Integrity.Capacity < 40 ||
		report.Integrity.RefusedBy != "prepare_experiment" || !strings.Contains(report.Integrity.Refusal, "limit reached") {
		t.Errorf("one hypothesis held %d runs (limit hit %v, refused by %q: %q), want at least 40 before prepare_experiment refuses readably",
			report.Integrity.Capacity, report.Integrity.CapacityHit, report.Integrity.RefusedBy, report.Integrity.Refusal)
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

// TestScoreLiveStudy grades a Study that any Agent ran against a live Gemcp
// with the examples/research-bench workloads:
//
//	GEMCP_BENCH_URL=http://127.0.0.1:8080/mcp GEMCP_BENCH_TOKEN=<agent token> \
//	GEMCP_BENCH_STUDY=<study id> GEMCP_BENCH_TASK=lr-sweep \
//	go test ./internal/researchbench -run TestScoreLiveStudy -v
func TestScoreLiveStudy(t *testing.T) {
	endpoint := os.Getenv("GEMCP_BENCH_URL")
	if endpoint == "" {
		t.Skip("set GEMCP_BENCH_URL, GEMCP_BENCH_TOKEN, GEMCP_BENCH_STUDY, and GEMCP_BENCH_TASK to grade a live Study")
	}
	task, ok := TaskByID(os.Getenv("GEMCP_BENCH_TASK"))
	if !ok {
		t.Fatalf("GEMCP_BENCH_TASK must name a benchmark task")
	}
	ctx := context.Background()
	session, err := Connect(ctx, endpoint, os.Getenv("GEMCP_BENCH_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	score, err := ScoreStudy(ctx, MCPTools{Session: session}, task, os.Getenv("GEMCP_BENCH_STUDY"))
	if err != nil {
		t.Fatal(err)
	}
	score.Agent = "live"
	t.Log("\n" + RenderMarkdown(Report{Label: "live", Scores: []Score{score}}))
}

// TestRenderComparison writes the side-by-side table from saved passes,
// oldest first:
//
//	GEMCP_BENCH_COMPARE=main.json,branch.json \
//	go test ./internal/researchbench -run TestRenderComparison -v
func TestRenderComparison(t *testing.T) {
	paths := os.Getenv("GEMCP_BENCH_COMPARE")
	if paths == "" {
		t.Skip("set GEMCP_BENCH_COMPARE to comma-separated GEMCP_BENCH_OUT reports")
	}
	var reports []Report
	for _, path := range strings.Split(paths, ",") {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var report Report
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		reports = append(reports, report)
	}
	comparison := RenderComparison(reports...)
	t.Log("\n" + comparison)
	if dir := os.Getenv("GEMCP_BENCH_OUT"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "comparison.md"), []byte(comparison), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
