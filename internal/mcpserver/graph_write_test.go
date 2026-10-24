package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGraphWritesReturnWhatTheyRecordedOverMCP(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp-graph-write?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("Project").SetSlug("project").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	repository, _ := client.Repository.Create().
		SetProjectID(project.ID).SetName("main").SetSSHURL("git@github.com:XR-Lee/Gemcp.git").SetSSHHost("github.com").
		SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().
		SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
		SetPriceFromMilli(10).SetPriceToMilli(3000).SetIsDefault(true).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	rawToken, prefix, _ := secrets.RandomToken("gmc", 32)
	token, _ := client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("graph-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).SetScopes([]string{"read", "submit"}).Save(ctx)

	researchService := research.NewService(client)
	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}),
		"test", nil, WithResearch(researchService),
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "gemcp-test", Version: "test"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}},
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()
	call := func(name string, arguments map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil || result.IsError {
			t.Fatalf("%s = %+v, %v", name, result, err)
		}
		var output map[string]any
		decodeStructured(t, result.StructuredContent, &output)
		return output
	}
	nodes := func(output map[string]any) []map[string]any {
		t.Helper()
		var list []map[string]any
		decodeStructured(t, output["nodes"], &list)
		return list
	}

	created := call("update_research_workspace", map[string]any{"study": map[string]any{
		"name": "compact", "question": "Do Graph writes stay small over MCP?",
	}})
	if _, ok := created["study"]; ok || created["view"] != research.ViewCompact || created["study_id"] == "" {
		t.Fatalf("Study write = %+v", created)
	}
	question := nodes(created)
	if len(question) != 1 || question[0]["kind"] != "question" {
		t.Fatalf("Study write nodes = %+v", question)
	}
	recorded := call("update_research_workspace", map[string]any{"node": map[string]any{
		"kind": "hypothesis", "title": "Compact writes are enough", "summary": "An Agent never needs the whole Graph back from a write.",
		"from_node_id": question[0]["id"], "relation": "leads_to",
	}})
	hypothesis := nodes(recorded)
	if len(hypothesis) != 1 || hypothesis[0]["kind"] != "hypothesis" || recorded["hypothesis"] == nil {
		t.Fatalf("hypothesis write = %+v", recorded)
	}
	hypothesisID, _ := hypothesis[0]["id"].(string)

	full := call("update_research_workspace", map[string]any{"view": "full", "node": map[string]any{
		"kind": "observation", "title": "Writes used to return every node", "from_node_id": hypothesisID, "relation": "leads_to",
	}})
	var study research.StudyView
	decodeStructured(t, full["study"], &study)
	if full["view"] != research.ViewFull || len(study.Nodes) != 3 || full["studies"] == nil || len(nodes(full)) != 1 {
		t.Fatalf("full write = %+v", full)
	}

	rejected, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "update_research_workspace", Arguments: map[string]any{
		"view": "everything", "node": map[string]any{"kind": "observation", "title": "Never written", "from_node_id": hypothesisID, "relation": "leads_to"},
	}})
	if err != nil || !rejected.IsError || !strings.Contains(textOf(rejected), "view must be compact or full") {
		t.Fatalf("unknown view = %+v, %v", rejected, err)
	}

	finishedAt := time.Now().UTC()
	experimentRecord, err := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).SetRepositoryID(repository.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha("0123456789012345678901234567890123456789").
		SetCommand("python train.py").SetState("succeeded").SetFinishedAt(finishedAt).SetMaxRuntimeSeconds(300).
		SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"name": "main"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		SetMetrics(map[string]any{"overall_accuracy": 0.91}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	if _, err := researchService.BindPreparedRun(ctx, principal, hypothesisID, experimentRecord.PublicID.String(), "smoke run"); err != nil {
		t.Fatal(err)
	}
	closed := call("close_run", map[string]any{
		"experiment_id": experimentRecord.PublicID.String(), "title": "smoke accuracy", "metric_name": "overall_accuracy", "verdict": "supports",
	})
	written := nodes(closed)
	var summary research.HypothesisSummary
	decodeStructured(t, closed["hypothesis"], &summary)
	if _, ok := closed["study"]; ok || len(written) != 2 || written[0]["kind"] != "result" || written[1]["kind"] != "observation" {
		t.Fatalf("close_run = %+v", closed)
	}
	if summary.ID != hypothesisID || summary.Run == nil || summary.Run.Verdict != "supports" || summary.Tally.Supports != 1 {
		t.Fatalf("close_run hypothesis = %+v", summary)
	}
}

func textOf(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if typed, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, typed.Text)
		}
	}
	return strings.Join(parts, " ")
}
