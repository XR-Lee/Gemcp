package research

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	_ "github.com/mattn/go-sqlite3"
)

func TestCatalogRoundTripStoresResearchBranchRowsWithoutStartingWorkloads(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-catalog?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	principal, repo := seedCatalogProject(t, ctx, client)
	service := NewService(client)

	first := CatalogRowInput{
		Branch:         "autoresearch/sprint-beat-sast-20260817",
		Setting:        "G2 seed-2 best-checkpoint Mean U-spec",
		Method:         "U-spec unified organizer",
		Implementation: "cfgs/sprint plus frozen analyze_sprint_g2.py",
		Metric:         "four_task_mean=90.4114",
		Result:         "promote_U-spec; versus SAST 90.91 is -0.4986 pp",
		Link:           "SPRINT_G2_DECISION.md",
		Hash:           "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0",
	}
	second := CatalogRowInput{
		Branch:         "autoresearch/learnable-membership-20260829",
		Setting:        "M1M3 G-0prime objbg Coad u0",
		Method:         "select_t_star",
		Implementation: "M1M3_G0PRIME_RESULTS.md eval-only train x eval matrix",
		Metric:         "coad_pp=6.1962",
		Result:         "T*=objbg because Coad(u0)=6.1962 pp",
		Link:           "M1M3_G0PRIME_RESULTS.md",
		Hash:           "ea6b1b5e64b9fa9815bc0ac6852a3c723e03b5e5",
	}

	written, err := service.AgentRecordCatalog(ctx, principal, CatalogRecordInput{
		RepositoryID: repo.PublicID.String(),
		Rows:         []CatalogRowInput{first, second},
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := service.AgentCatalog(ctx, principal, CatalogListInput{RepositoryID: repo.PublicID.String()})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Repositories) != 1 || listed.Repositories[0].SSHURL != "git@github.com:XR-Lee/DynamicPointMamba.git" {
		t.Fatalf("listed registration = %+v", listed.Repositories)
	}
	if listed.Repositories[0].Name != "DynamicPointMamba" || listed.Repositories[0].DefaultBranch != "main" || listed.Repositories[0].Status != "active" {
		t.Fatalf("registration DTO = %+v", listed.Repositories[0])
	}
	assertCatalogContains(t, listed, first)
	assertCatalogContains(t, listed, second)
	if len(written.Repositories) != 1 || len(written.Repositories[0].Rows) != 2 {
		t.Fatalf("write view = %+v", written)
	}

	if count, _ := client.Experiment.Query().Count(ctx); count != 0 {
		t.Fatalf("catalog write created Experiment count=%d", count)
	}
	if count, _ := client.ExperimentProposal.Query().Count(ctx); count != 0 {
		t.Fatalf("catalog write created Proposal count=%d", count)
	}
	if count, _ := client.BudgetEntry.Query().Count(ctx); count != 0 {
		t.Fatalf("catalog write created budget reservation count=%d", count)
	}

	if _, err := service.AgentRecordCatalog(ctx, principal, CatalogRecordInput{
		RepositoryID: repo.PublicID.String(),
		Rows: []CatalogRowInput{{
			Branch: "autoresearch/sprint-beat-sast-20260817", Setting: "secret setting",
			Method: "method", Implementation: "impl", Metric: "1.0", Result: "ok",
			Hash: "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0",
			Link: "https://example.com?api_key=secret",
		}},
	}); err == nil {
		t.Fatal("accepted credential-like catalog text")
	}
	if _, err := service.AgentRecordCatalog(ctx, principal, CatalogRecordInput{
		RepositoryID: repo.PublicID.String(),
		Rows: []CatalogRowInput{{
			Branch: "main", Setting: "Use token gmc_secret_value_here",
			Method: "method", Implementation: "impl", Metric: "1.0", Result: "ok",
			Hash: "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0",
		}},
	}); err == nil {
		t.Fatal("accepted credential-like setting")
	}

	readOnly := principal
	readOnly.Scopes = []string{"read"}
	if _, err := service.AgentRecordCatalog(ctx, readOnly, CatalogRecordInput{
		RepositoryID: repo.PublicID.String(), Rows: []CatalogRowInput{first},
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only catalog write error = %v", err)
	}

	owner, err := service.OwnerCatalog(ctx, principal.TenantID, principal.ProjectPublicID, repo.PublicID.String())
	if err != nil {
		t.Fatal(err)
	}
	assertCatalogContains(t, owner, first)
	assertCatalogContains(t, owner, second)
}

func TestParsePublishedDynamicPointMambaExperimentMap(t *testing.T) {
	yamlBytes, err := os.ReadFile(filepath.Join("testdata", "dynamicpointmamba", "experiment_graph.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rows := ParseCatalogSources("", "", "experiment_graph.yaml", string(yamlBytes))
	if len(rows) < 2 {
		t.Fatalf("parsed rows = %+v", rows)
	}
	branches := map[string]CatalogRowInput{}
	for _, row := range rows {
		if row.Hash == "" || (row.Metric == "" && row.Result == "") {
			t.Fatalf("incomplete extracted row = %+v", row)
		}
		branches[row.Branch] = row
	}
	if _, ok := branches["autoresearch/sprint-beat-sast-20260817"]; !ok {
		t.Fatalf("missing sprint branch in %+v", branches)
	}
	if _, ok := branches["autoresearch/invariance-plan-20260817"]; !ok {
		t.Fatalf("missing invariance branch in %+v", branches)
	}
	if !strings.Contains(branches["autoresearch/sprint-beat-sast-20260817"].Metric, "90.4114") &&
		!strings.Contains(branches["autoresearch/sprint-beat-sast-20260817"].Result, "90.4114") {
		t.Fatalf("sprint row missing published metric: %+v", branches["autoresearch/sprint-beat-sast-20260817"])
	}

	g2, err := os.ReadFile(filepath.Join("testdata", "dynamicpointmamba", "SPRINT_G2_DECISION.md"))
	if err != nil {
		t.Fatal(err)
	}
	tableRows := ParseCatalogSources("autoresearch/sprint-beat-sast-20260817", "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0", "SPRINT_G2_DECISION.md", string(g2))
	if len(tableRows) == 0 {
		t.Fatal("G2 markdown produced no rows")
	}
	foundSpec := false
	for _, row := range tableRows {
		if strings.Contains(row.Setting, "U-spec") && strings.Contains(row.Metric, "90.4114") {
			foundSpec = true
		}
	}
	if !foundSpec {
		t.Fatalf("G2 rows missing U-spec 90.4114: %+v", tableRows)
	}
}

func TestExtractAndPersistDynamicPointMambaCatalog(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-dpm-catalog?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	principal, repo := seedCatalogProject(t, ctx, client)
	service := NewService(client)

	rows, source := loadDynamicPointMambaRows(t)
	branches := map[string]struct{}{}
	withEvidence := 0
	for _, row := range rows {
		branches[row.Branch] = struct{}{}
		if row.Hash != "" && (row.Metric != "" || row.Result != "") {
			withEvidence++
		}
	}
	if len(branches) < 2 || withEvidence < 2 {
		t.Fatalf("source %s produced branches=%d evidence=%d rows=%+v", source, len(branches), withEvidence, rows)
	}

	written, err := service.AgentRecordCatalog(ctx, principal, CatalogRecordInput{
		RepositoryID: repo.PublicID.String(),
		Rows:         rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := service.OwnerCatalog(ctx, principal.TenantID, principal.ProjectPublicID, "")
	if err != nil {
		t.Fatal(err)
	}
	if listed.Repositories[0].SSHURL != DynamicPointMambaSSHURL() {
		t.Fatalf("registration ssh_url = %s", listed.Repositories[0].SSHURL)
	}
	storedBranches := map[string]struct{}{}
	evidence := 0
	for _, row := range listed.Repositories[0].Rows {
		storedBranches[row.Branch] = struct{}{}
		if row.Hash != "" && (row.Metric != "" || row.Result != "") {
			evidence++
		}
	}
	if len(storedBranches) < 2 || evidence < 2 {
		t.Fatalf("stored catalog source=%s branches=%d evidence=%d view=%+v", source, len(storedBranches), evidence, listed)
	}
	if len(written.Repositories[0].Rows) != len(listed.Repositories[0].Rows) {
		t.Fatalf("write/list mismatch write=%d list=%d", len(written.Repositories[0].Rows), len(listed.Repositories[0].Rows))
	}
	t.Logf("dynamicpointmamba catalog source=%s branches=%d rows=%d", source, len(storedBranches), len(listed.Repositories[0].Rows))
}

func loadDynamicPointMambaRows(t *testing.T) ([]CatalogRowInput, string) {
	t.Helper()
	clone := DynamicPointMambaCheckout()
	if info, err := os.Stat(filepath.Join(clone, ".git")); err == nil && info != nil {
		rows, err := ExtractFromCheckout(clone)
		if err != nil {
			t.Fatalf("ExtractFromCheckout(%s) error = %v", clone, err)
		}
		if catalogBranchCount(rows) >= 2 {
			return capCatalogWrite(rows), clone
		}
		t.Fatalf("checkout %s produced too few research branches: %+v", clone, rows)
	}
	yamlBytes, err := os.ReadFile(filepath.Join("testdata", "dynamicpointmamba", "experiment_graph.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rows := ParseCatalogSources("", "", "experiment_graph.yaml", string(yamlBytes))
	if catalogBranchCount(rows) < 2 {
		t.Fatalf("testdata map produced too few branches: %+v", rows)
	}
	return capCatalogWrite(rows), "testdata/dynamicpointmamba/experiment_graph.yaml"
}

func capCatalogWrite(rows []CatalogRowInput) []CatalogRowInput {
	if len(rows) <= maxCatalogRowsPerWrite {
		return rows
	}
	return rows[:maxCatalogRowsPerWrite]
}

func catalogBranchCount(rows []CatalogRowInput) int {
	seen := map[string]struct{}{}
	for _, row := range rows {
		if row.Branch != "" && row.Hash != "" {
			seen[row.Branch] = struct{}{}
		}
	}
	return len(seen)
}

func seedCatalogProject(t *testing.T, ctx context.Context, client *ent.Client) (agentauth.Principal, *ent.Repository) {
	t.Helper()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.Project.Create().SetTenantID(tenant.ID).SetName("DynamicPointMamba").SetSlug("dynamicpointmamba").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("catalog-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := client.Repository.Create().
		SetProjectID(project.ID).SetName("DynamicPointMamba").
		SetSSHURL(DynamicPointMambaSSHURL()).SetSSHHost("github.com").
		SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}, repo
}

func assertCatalogContains(t *testing.T, view CatalogView, want CatalogRowInput) {
	t.Helper()
	if len(view.Repositories) == 0 {
		t.Fatal("catalog has no repositories")
	}
	for _, repo := range view.Repositories {
		for _, row := range repo.Rows {
			if row.Branch == want.Branch && row.Hash == want.Hash && row.Setting == want.Setting {
				if row.Method != want.Method || row.Implementation != want.Implementation || row.Metric != want.Metric || row.Result != want.Result || row.Link != want.Link {
					t.Fatalf("row fields = %+v want %+v", row, want)
				}
				return
			}
		}
	}
	t.Fatalf("missing catalog row %+v in %+v", want, view)
}
