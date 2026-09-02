package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExperimentCatalogMCPRoundTripTwice(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:mcp-catalog?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().
		SetTenantID(tenant.ID).SetName("DynamicPointMamba").SetSlug("dynamicpointmamba").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).SetMaxRuntimeSeconds(3600).
		Save(ctx)
	repo, _ := client.Repository.Create().
		SetProjectID(project.ID).SetName("DynamicPointMamba").
		SetSSHURL(research.DynamicPointMambaSSHURL()).SetSSHHost("github.com").
		SetDefaultBranch("main").SetHostKeyFingerprint("SHA256:test").SetStatus("active").
		Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	rawToken, prefix, _ := secrets.RandomToken("gmc", 32)
	_, _ = client.AgentToken.Create().
		SetProjectID(project.ID).SetLabel("catalog-agent").SetPrefix(prefix).
		SetTokenHash(box.Digest("agent-token", rawToken)).SetScopes([]string{"read", "submit"}).Save(ctx)

	handler := New(
		agentauth.NewService(client, box),
		experiment.NewService(client, box, allowCommitVerifier{}),
		"test", nil, WithResearch(research.NewService(client)),
	).Handler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	httpClient := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gemcp-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	rows := []map[string]any{
		{
			"branch":         "autoresearch/sprint-beat-sast-20260817",
			"setting":        "G2 seed-2 best-checkpoint Mean U-spec",
			"method":         "U-spec unified organizer",
			"implementation": "SPRINT_G2_DECISION.md frozen analyzer",
			"metric":         "90.4114",
			"result":         "promote_U-spec versus SAST -0.4986 pp",
			"link":           "SPRINT_G2_DECISION.md",
			"hash":           "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0",
		},
		{
			"branch":         "autoresearch/learnable-membership-20260829",
			"setting":        "M1M3 G-0prime objbg Coad u0",
			"method":         "select_t_star",
			"implementation": "M1M3_G0PRIME_RESULTS.md",
			"metric":         "6.1962",
			"result":         "T*=objbg Coad(u0)=6.1962 pp",
			"link":           "M1M3_G0PRIME_RESULTS.md",
			"hash":           "ea6b1b5e64b9fa9815bc0ac6852a3c723e03b5e5",
		},
	}
	recordArgs := map[string]any{"repository_id": repo.PublicID.String(), "rows": rows}

	var firstContent, secondContent string
	for i := 0; i < 2; i++ {
		recorded, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_experiment_catalog", Arguments: recordArgs})
		if err != nil || recorded.IsError {
			t.Fatalf("record_experiment_catalog run %d = %+v, %v", i+1, recorded, err)
		}
		listed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_experiment_catalog", Arguments: map[string]any{
			"repository_id": repo.PublicID.String(),
		}})
		if err != nil || listed.IsError {
			t.Fatalf("get_experiment_catalog run %d = %+v, %v", i+1, listed, err)
		}
		raw, err := json.Marshal(listed.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		var view research.CatalogView
		decodeStructured(t, listed.StructuredContent, &view)
		content := catalogScientificContent(t, view)
		if i == 0 {
			firstContent = content
		} else {
			secondContent = content
		}
		if len(view.Repositories) != 1 || len(view.Repositories[0].Rows) != 2 {
			t.Fatalf("catalog view run %d = %+v", i+1, view)
		}
		got := view.Repositories[0]
		if got.SSHURL != research.DynamicPointMambaSSHURL() || got.DefaultBranch != "main" || got.Status != "active" {
			t.Fatalf("registration DTO run %d = %+v", i+1, got)
		}
		if !strings.Contains(body, "90.4114") || !strings.Contains(body, "6.1962") ||
			!strings.Contains(body, "autoresearch/sprint-beat-sast-20260817") ||
			!strings.Contains(body, "autoresearch/learnable-membership-20260829") ||
			!strings.Contains(body, "79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0") ||
			!strings.Contains(body, "U-spec unified organizer") {
			t.Fatalf("catalog body missing rows run %d: %s", i+1, body)
		}
		if count, _ := client.Experiment.Query().Count(ctx); count != 0 {
			t.Fatalf("MCP catalog ingest created Experiment count=%d", count)
		}
	}
	if firstContent == "" || firstContent != secondContent {
		t.Fatalf("repeat catalog content differs\nfirst=%s\nsecond=%s", firstContent, secondContent)
	}
}

func catalogScientificContent(t *testing.T, view research.CatalogView) string {
	t.Helper()
	type row struct {
		Branch, Setting, Method, Implementation, Metric, Result, Link, Hash, SSHURL, DefaultBranch, Status string
	}
	payload := make([]row, 0)
	for _, repo := range view.Repositories {
		for _, item := range repo.Rows {
			payload = append(payload, row{
				Branch: item.Branch, Setting: item.Setting, Method: item.Method, Implementation: item.Implementation,
				Metric: item.Metric, Result: item.Result, Link: item.Link, Hash: item.Hash,
				SSHURL: repo.SSHURL, DefaultBranch: repo.DefaultBranch, Status: repo.Status,
			})
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
