package research

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	_ "github.com/mattn/go-sqlite3"
)

func TestNormalizeEvidenceCommitTreatsHostSentinelAsAbsent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "host", want: ""},
		{in: "HOST", want: ""},
		{in: "  host  ", want: ""},
		{in: "a1b2c3d", want: "a1b2c3d"},
		{in: "0123456789012345678901234567890123456789", want: "0123456789012345678901234567890123456789"},
		{in: "not-a-sha", wantErr: true},
		{in: "abc12", wantErr: true},
	}
	for _, tc := range cases {
		got, err := normalizeEvidenceCommit(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeEvidenceCommit(%q) accepted invalid SHA", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("normalizeEvidenceCommit(%q) = %q, %v want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestCloseRunHostProcessExperimentWithoutInventedSHA(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-close-run-host?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("host-close-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("local-cpu-host").SetBackend("ssh_cloud").SetImageUUID(sshcloud.HostImage).SetIsDefault(true).Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("local-cpu").SetBackend("ssh_cloud").SetRegion("ssh_cloud").SetGpuNames([]string{"host"}).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(0).SetPriceToMilli(0).SetIsDefault(true).Save(ctx)
	finished, err := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha(sshcloud.HostCommit).
		SetCommand("python3 train.py").SetState("succeeded").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"source": "host"}).
		SetEnvironmentSnapshot(map[string]any{"name": "local-cpu-host", "backend": "ssh_cloud"}).
		SetResourceSnapshot(map[string]any{"name": "local-cpu", "backend": "ssh_cloud"}).
		SetOutputPath("managed://experiments/cpu/outputs").SetReservedCostMilli(0).
		SetMetrics(map[string]any{"overall_accuracy": 0.75}).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExperimentProposal.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).
		SetExperimentID(finished.ID).SetStatus("submitted").SetRequestedRef(sshcloud.HostRef).
		SetCommitSha(sshcloud.HostCommit).SetExecutionMode("argv").SetArgv([]string{"python3", "train.py"}).
		SetDisplayCommand("python3 train.py").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).
		SetTerminationGraceSeconds(30).SetProjectSnapshot(map[string]any{"expected_metric": "overall_accuracy"}).
		SetRepositorySnapshot(map[string]any{"source": "host"}).SetEnvironmentSnapshot(map[string]any{"backend": "ssh_cloud"}).
		SetResourceSnapshot(map[string]any{"backend": "ssh_cloud"}).SetChecks([]map[string]any{}).
		SetReservedCostMilli(0).SetConfirmationDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").
		SetExpiresAt(time.Now().UTC().Add(time.Hour)).Save(ctx); err != nil {
		t.Fatal(err)
	}
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	created, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "cpu-loop first", Question: "Can a host-process Experiment close without a git SHA?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Node: &NodeInput{StudyID: created.Study.ID, Kind: "hypothesis", Title: "Host process can close", FromNodeID: created.Study.Nodes[0].ID, Relation: "leads_to"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "cpu-loop leftover", Question: "A second Study from a previous local CPU loop."},
	}); err != nil {
		t.Fatal(err)
	}
	runID, err := service.BindPreparedRun(ctx, principal, hypothesis.Study.Nodes[1].ID, finished.PublicID.String(), "local CPU fixture")
	if err != nil || runID == "" {
		t.Fatalf("BindPreparedRun() = %q, %v", runID, err)
	}
	closed, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(),
		Title:        "ModelNet40-mini CPU fixture finished",
		Summary:      "Copied overall_accuracy from the scraped metrics.json. Do not infer from logs.",
		Status:       "succeeded",
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Study == nil || closed.Study.ID != created.Study.ID {
		t.Fatalf("close_run selected study = %+v", closed.Study)
	}
	result := nodeByKind(closed.Study.Nodes, "result")
	if result.Kind != "result" || result.Title != "ModelNet40-mini CPU fixture finished" || result.CommitSHA != "" {
		t.Fatalf("host-process result = %+v", result)
	}
	if result.MetricName != "overall_accuracy" || result.MetricValue == nil || *result.MetricValue != 0.75 {
		t.Fatalf("host-process result metric = %+v", result)
	}
	if !hasHighlightOnHypothesis(closed.Study, hypothesis.Study.Nodes[1].ID, "ModelNet40-mini CPU fixture finished") {
		t.Fatalf("missing highlight: %+v", closed.Study)
	}
	for _, node := range closed.Study.Nodes {
		if node.Kind == "observation" && node.Title == "ModelNet40-mini CPU fixture finished" && node.CommitSHA != "" {
			t.Fatalf("highlight copied host sentinel commit_sha = %q", node.CommitSHA)
		}
		if node.Kind == "run" && node.ExperimentID == finished.PublicID.String() && node.CommitSHA != "" {
			t.Fatalf("run copied host sentinel commit_sha = %q", node.CommitSHA)
		}
	}
}

func TestCloseRunUnboundExperimentStillReportsMissingRun(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:research-close-run-unbound?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	token, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("lab-agent").SetPrefix("gmc_lab").SetTokenHash([]byte("unbound-hash")).SetScopes([]string{"read", "submit"}).Save(ctx)
	environment, _ := client.Environment.Create().SetProjectID(project.ID).SetName("default").SetImageUUID("image").Save(ctx)
	profile, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetName("default").SetRegion("west").SetGpuNames([]string{"RTX 4090"}).SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(3000).Save(ctx)
	finished, _ := client.Experiment.Create().
		SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(token.ID).
		SetEnvironmentID(environment.ID).SetResourceProfileID(profile.ID).SetCommitSha(sshcloud.HostCommit).
		SetCommand("python3 train.py").SetState("succeeded").SetMaxRuntimeSeconds(300).SetTimeoutExtensionSeconds(60).SetTerminationGraceSeconds(30).
		SetRepositorySnapshot(map[string]any{"source": "host"}).SetEnvironmentSnapshot(map[string]any{"name": "default"}).
		SetResourceSnapshot(map[string]any{"name": "default"}).SetOutputPath("/outputs").SetReservedCostMilli(0).
		Save(ctx)
	principal := agentauth.Principal{
		TenantID: tenant.ID, ProjectID: project.ID, ProjectPublicID: project.PublicID.String(),
		TokenID: token.ID, TokenPublicID: token.PublicID.String(), Scopes: token.Scopes,
	}
	service := NewService(client)
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "first", Question: "What is the first question to answer?"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AgentUpdate(ctx, principal, UpdateInput{
		Study: &StudyInput{Name: "second", Question: "What is the second question to answer?"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := service.AgentCloseRun(ctx, principal, CloseRunInput{
		ExperimentID: finished.PublicID.String(), Title: "unbound host result",
	})
	var validation *ValidationError
	if err == nil || !errors.As(err, &validation) || validation.Message != "no Graph run is bound to this Experiment" {
		t.Fatalf("unbound close_run error = %v", err)
	}
}
