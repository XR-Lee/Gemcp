package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/nodeaccess"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/selfhosted"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type nodeHTTPArchive struct{ payload []byte }

func (a *nodeHTTPArchive) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.payload)), nil
}
func (a *nodeHTTPArchive) Close() error     { return nil }
func (a *nodeHTTPArchive) SizeBytes() int64 { return int64(len(a.payload)) }

type nodeHTTPArchiver struct{ payload []byte }

func (a nodeHTTPArchiver) ArchiveCommit(context.Context, int, string, int64) (gitrepository.Archive, error) {
	return &nodeHTTPArchive{payload: a.payload}, nil
}

func TestNodeEnrollmentAndSyncHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, dialect.SQLite, "file:node-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Research").SetSlug("research").SetMonthlyBudgetMilli(1000).SetMaxExperimentMilli(1000).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	selfHostedConfig := selfhosted.DefaultConfig()
	selfHostedConfig.Enabled = true
	selfHostedConfig.InstanceID = "http-test"
	assignmentService, _ := selfhosted.NewService(client, nodeHTTPArchiver{payload: []byte("source-archive")}, selfHostedConfig)
	handlers := NewNodeHandlers(nodeaccess.NewService(
		client, box, "https://gemcp.example.com", true, nodeaccess.WithEventProjector(assignmentService),
	), assignmentService)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authenticatedContext{Principal: auth.Principal{
			TenantID: tenant.ID, TenantPublicID: tenant.PublicID.String(), UserPublicID: "owner", Role: "owner",
		}})
		c.Next()
	})
	router.GET("/nodes", handlers.List)
	router.POST("/node-enrollments", handlers.IssueEnrollment)
	router.POST("/node-enrollments/claim", handlers.ClaimEnrollment)
	router.POST("/node-enrollments/:id/approve", handlers.ApproveEnrollment)
	router.POST("/nodes/sync", handlers.Sync)
	router.GET("/node-assignments/:id/source", handlers.Source)
	router.GET("/projects/:id/self-hosted-runtimes", handlers.ListRuntimeConfigs)
	router.POST("/projects/:id/self-hosted-runtimes", handlers.CreateRuntimeConfig)
	router.PUT("/projects/:id/self-hosted-trusted-workspace", handlers.EnableTrustedWorkspace)
	router.DELETE("/projects/:id/self-hosted-trusted-workspace/:node_id", handlers.DisableTrustedWorkspace)

	issue := jsonRequest(t, http.MethodPost, "/node-enrollments", nodeaccess.EnrollmentIssueInput{Label: "gpu-home-a"})
	issueResponse := httptest.NewRecorder()
	router.ServeHTTP(issueResponse, issue)
	if issueResponse.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	var issued struct {
		Data nodeaccess.EnrollmentIssueResult `json:"data"`
	}
	if err := json.Unmarshal(issueResponse.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(issued.Data.SetupURL)
	fragment, _ := url.ParseQuery(parsed.Fragment)
	code := fragment.Get("code")
	if !strings.HasPrefix(code, "gne_") {
		t.Fatalf("setup URL=%q", issued.Data.SetupURL)
	}
	inventory := nodeprotocol.Inventory{
		InstallationID: uuid.NewString(), MachineFingerprint: strings.Repeat("a", 64), Hostname: "gpu-home-a",
		OperatingSystem: "linux", Architecture: "amd64", AgentVersion: "test", ProtocolVersion: nodeprotocol.Version,
		CPUCount: 8, MemoryBytes: 32 << 30, GPUs: []nodeprotocol.GPU{{UUID: "GPU-test", Name: "RTX 3090", MemoryBytes: 24 << 30}},
		Storage: nodeprotocol.Storage{Root: "/mnt/gemcp", TotalBytes: 1 << 40, AvailableBytes: 1 << 39},
	}
	claimResponse := httptest.NewRecorder()
	router.ServeHTTP(claimResponse, jsonRequest(t, http.MethodPost, "/node-enrollments/claim", nodeprotocol.EnrollmentClaimRequest{Code: code, Inventory: inventory}))
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimResponse.Code, claimResponse.Body.String())
	}
	var claimed struct {
		Data nodeprotocol.EnrollmentClaimResponse `json:"data"`
	}
	if err := json.Unmarshal(claimResponse.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	approveResponse := httptest.NewRecorder()
	router.ServeHTTP(approveResponse, jsonRequest(t, http.MethodPost, "/node-enrollments/"+claimed.Data.EnrollmentID+"/approve", nodeaccess.ApprovalInput{
		PairingCode: claimed.Data.PairingCode, ProjectIDs: []string{project.PublicID.String()},
	}))
	if approveResponse.Code != http.StatusOK || !strings.Contains(approveResponse.Body.String(), `"status":"active"`) {
		t.Fatalf("approve status=%d body=%s", approveResponse.Code, approveResponse.Body.String())
	}
	syncRequest := jsonRequest(t, http.MethodPost, "/nodes/sync", nodeprotocol.SyncRequest{
		InstallationID: inventory.InstallationID, MachineFingerprint: inventory.MachineFingerprint,
		AgentVersion: "test", ProtocolVersion: nodeprotocol.Version, ObservedState: "online",
	})
	syncRequest.Header.Set("Authorization", "Bearer "+claimed.Data.NodeToken)
	syncResponse := httptest.NewRecorder()
	router.ServeHTTP(syncResponse, syncRequest)
	if syncResponse.Code != http.StatusOK || !strings.Contains(syncResponse.Body.String(), `"desired_state":"active"`) {
		t.Fatalf("sync status=%d body=%s", syncResponse.Code, syncResponse.Body.String())
	}
	workspaceResponse := httptest.NewRecorder()
	router.ServeHTTP(workspaceResponse, jsonRequest(t, http.MethodPut, "/projects/"+project.PublicID.String()+"/self-hosted-trusted-workspace", selfhosted.TrustedWorkspaceInput{
		NodeID: claimed.Data.NodeID, WorkspacePath: "/home/campus.ncl.ac.uk/nxl51/gemcp_tmp",
	}))
	if workspaceResponse.Code != http.StatusOK || !strings.Contains(workspaceResponse.Body.String(), `"workspace_path":"/home/campus.ncl.ac.uk/nxl51/gemcp_tmp"`) || !strings.Contains(workspaceResponse.Body.String(), `"gpu_name":"RTX 3090"`) {
		t.Fatalf("workspace status=%d body=%s", workspaceResponse.Code, workspaceResponse.Body.String())
	}
	disableWorkspaceResponse := httptest.NewRecorder()
	router.ServeHTTP(disableWorkspaceResponse, httptest.NewRequest(http.MethodDelete, "/projects/"+project.PublicID.String()+"/self-hosted-trusted-workspace/"+claimed.Data.NodeID, nil))
	if disableWorkspaceResponse.Code != http.StatusOK || !strings.Contains(disableWorkspaceResponse.Body.String(), `"disabled":true`) {
		t.Fatalf("disable workspace status=%d body=%s", disableWorkspaceResponse.Code, disableWorkspaceResponse.Body.String())
	}
	runtimeResponse := httptest.NewRecorder()
	router.ServeHTTP(runtimeResponse, jsonRequest(t, http.MethodPost, "/projects/"+project.PublicID.String()+"/self-hosted-runtimes", selfhosted.RuntimeConfigInput{
		Name: "local-3090", Image: "registry.example/train@sha256:" + strings.Repeat("b", 64),
		GPUNames: []string{"RTX 3090"}, CPULimit: 8, MemoryGB: 32, MakeDefault: true,
	}))
	if runtimeResponse.Code != http.StatusCreated || !strings.Contains(runtimeResponse.Body.String(), `"is_default":true`) {
		t.Fatalf("runtime status=%d body=%s", runtimeResponse.Code, runtimeResponse.Body.String())
	}
	nodeRecord, _ := client.SelfHostedNode.Query().Only(ctx)
	repositoryRecord, _ := client.Repository.Create().SetProjectID(project.ID).SetName("source").SetSSHURL("git@github.com:o/r.git").SetSSHHost("github.com").SetDefaultBranch("main").Save(ctx)
	environmentRecord, _ := client.Environment.Create().SetProjectID(project.ID).SetBackend("self_hosted").SetName("source-env").SetImageUUID("registry.example/train@sha256:" + strings.Repeat("c", 64)).Save(ctx)
	profileRecord, _ := client.ResourceProfile.Create().SetProjectID(project.ID).SetBackend("self_hosted").SetName("source-profile").SetRegion("self_hosted").
		SetGpuNames([]string{"RTX 3090"}).SetCudaFrom(1).SetCudaTo(1).SetCPUFrom(1).SetCPUTo(8).SetMemoryFromGB(1).SetMemoryToGB(32).SetPriceFromMilli(0).SetPriceToMilli(0).Save(ctx)
	agentRecord, _ := client.AgentToken.Create().SetProjectID(project.ID).SetLabel("source-agent").SetPrefix("gmc_source").SetTokenHash([]byte("source-agent-hash")).Save(ctx)
	experimentRecord, _ := client.Experiment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetAgentTokenID(agentRecord.ID).
		SetRepositoryID(repositoryRecord.ID).SetEnvironmentID(environmentRecord.ID).SetResourceProfileID(profileRecord.ID).SetState("provisioning").
		SetCommitSha(strings.Repeat("0", 40)).SetCommand("true").SetMaxRuntimeSeconds(60).SetTimeoutExtensionSeconds(0).SetTerminationGraceSeconds(5).
		SetRepositorySnapshot(map[string]any{}).SetEnvironmentSnapshot(map[string]any{}).SetResourceSnapshot(map[string]any{}).
		SetOutputPath("managed://experiments/source/outputs").SetReservedCostMilli(0).Save(ctx)
	attemptRecord, _ := client.Attempt.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetExperimentID(experimentRecord.ID).SetNumber(1).Save(ctx)
	assignmentRecord, _ := client.NodeAssignment.Create().SetTenantID(tenant.ID).SetProjectID(project.ID).SetExperimentID(experimentRecord.ID).
		SetAttemptID(attemptRecord.ID).SetNodeID(nodeRecord.ID).SetOutputRef("experiments/source/outputs").Save(ctx)
	sourceRequest := httptest.NewRequest(http.MethodGet, "/node-assignments/"+assignmentRecord.PublicID.String()+"/source", nil)
	sourceRequest.Header.Set("Authorization", "Bearer "+claimed.Data.NodeToken)
	sourceResponse := httptest.NewRecorder()
	router.ServeHTTP(sourceResponse, sourceRequest)
	if sourceResponse.Code != http.StatusOK || sourceResponse.Body.String() != "source-archive" {
		t.Fatalf("source status=%d body=%q", sourceResponse.Code, sourceResponse.Body.String())
	}
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/nodes", nil))
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), claimed.Data.NodeToken) || strings.Contains(listResponse.Body.String(), code) {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	unauthenticatedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthenticatedResponse, jsonRequest(t, http.MethodPost, "/nodes/sync", nodeprotocol.SyncRequest{}))
	if unauthenticatedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated sync status=%d body=%s", unauthenticatedResponse.Code, unauthenticatedResponse.Body.String())
	}
}

func TestNodeValidationErrorsKeepDomainCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := &NodeHandlers{}
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "node access", err: &nodeaccess.ValidationError{Message: "invalid enrollment"}, code: "INVALID_NODE_REQUEST"},
		{name: "self hosted runtime", err: &selfhosted.ValidationError{Message: "invalid runtime"}, code: "INVALID_SELF_HOSTED_RUNTIME"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			handlers.writeError(context, test.err)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("response status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func jsonRequest(t *testing.T, method, target string, value any) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, target, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	return request
}
