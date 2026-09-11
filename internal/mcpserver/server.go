package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/guides"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/XR-Lee/Gemcp/internal/imagebake"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/XR-Lee/Gemcp/internal/workspacecatalog"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxMCPRequestBytes = 1 << 20
	ssePrimer          = ": connected\n\n"
)

type Server struct {
	agentAuth    *agentauth.Service
	experiments  *experiment.Service
	repositories *gitrepository.Service
	datasets     *workspacecatalog.Service
	bindings     *datasetcatalog.Service
	environments *environmentcatalog.Service
	bakes        *imagebake.Service
	research     *research.Service
	sshCloud     *sshcloud.Service
	logger       *slog.Logger
	handler      http.Handler
}

type emptyInput struct{}

type UsageGuide struct {
	ProjectID   string   `json:"project_id"`
	TokenScopes []string `json:"token_scopes"`
	ResourceURI string   `json:"resource_uri"`
	PromptName  string   `json:"prompt_name"`
	Markdown    string   `json:"markdown"`
}

const serverInstructions = "Treat the research Graph as the execution contract. Call get_next_actions before spending; it proposes the next decision or Experiment from the hypothesis, its runs, and its observations. Keep the Owner-facing Study current with get_research_workspace. For a registered GitHub repository, extract raw experiment rows from distinct research branches into record_experiment_catalog (Setting, 方法/method, 实现/implementation, metric, 结果/result, link, hash). That catalog is the table-ready evidence store and is not the Graph; it never starts a workload. Confirm with get_experiment_catalog. Never include prompts, private reasoning, credentials, or environment dumps in research text. prepare_experiment requires a connected hypothesis or plan from_node_id when a Study exists; isolated nodes cannot prepare; that origin is bound into the confirmation digest. Project budget and submit scope are limits and technical capabilities, not financial approval. Show the immutable proposal and exact confirmation digest to the Owner, and never call submit_prepared_experiment until the Owner explicitly confirms that digest. submit_prepared_experiment writes the run node; bind failure is an error. Poll get_experiment for state, log_tail, and metrics; never SSH, fetch remote files, or infer metrics from logs. close_run is the only way to record a result after a terminal Experiment and also writes a highlight observation on the originating hypothesis; omit metric_name to copy the prepared expected_metric from the Experiment; result_commit_sha may attach the full Git commit containing its durable result manifest. Linking a Graph node never starts a workload. submit_experiment is rejected when an active Study exists."

type Option func(*Server)

func WithConfiguration(repositories *gitrepository.Service, datasets *workspacecatalog.Service, bindings *datasetcatalog.Service) Option {
	return func(server *Server) {
		server.repositories = repositories
		server.datasets = datasets
		server.bindings = bindings
	}
}

func WithResearch(service *research.Service) Option {
	return func(server *Server) {
		server.research = service
	}
}

func WithSSHCloud(service *sshcloud.Service) Option {
	return func(server *Server) {
		server.sshCloud = service
	}
}

func WithEnvironments(service *environmentcatalog.Service) Option {
	return func(server *Server) {
		server.environments = service
	}
}

func WithImageBakes(service *imagebake.Service) Option {
	return func(server *Server) {
		server.bakes = service
	}
}

func New(agentAuth *agentauth.Service, experiments *experiment.Service, version string, logger *slog.Logger, options ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{agentAuth: agentAuth, experiments: experiments, logger: logger}
	for _, option := range options {
		option(server)
	}
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "gemcp", Version: version}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_usage_guide", Description: "Return the mandatory Gemcp operating guide, approval boundary, and safe submission workflow.",
	}, server.getUsageGuide)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_project_options", Description: "List project policy, approved execution options, dataset bindings, dataset source catalog, Provider-visible images, public-cloud onboarding next steps, automatically discovered authorized Self-hosted Node readiness, and experimental Cloud SSH node readiness. Includes a readiness summary and heartbeat contract. Cloud SSH nodes are listed before Project authorization; the control plane probes them. Agents are Project-scoped and not exclusively bound to one node.",
	}, server.getProjectOptions)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_repository_registrations", Description: "List active and pending Git repositories for the authenticated Project, including public deploy keys.",
	}, server.listRepositoryRegistrations)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "register_repository", Description: "Register a GitHub repository from an SSH or HTTPS URL. Public repositories activate immediately with no Deploy Key. Private repositories return a pending record and a read-only deploy public key. Requires configure scope.",
	}, server.registerRepository)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "verify_repository", Description: "Verify one pending Project repository after its read-only deploy key is installed. Requires configure scope.",
	}, server.verifyRepository)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_workspace_datasets", Description: "List dataset paths declared below this Project's Owner-approved trusted workspace roots.",
	}, server.listWorkspaceDatasets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "register_workspace_dataset", Description: "Declare a normalized dataset path below an existing Owner-approved trusted workspace root. This never authorizes a new host path. Requires configure scope.",
	}, server.registerWorkspaceDataset)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "remove_workspace_dataset", Description: "Disable one Project workspace dataset declaration. Requires configure scope.",
	}, server.removeWorkspaceDataset)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_dataset_bindings", Description: "List Project dataset bindings for Public Elastic, Private Cloud, and Cloud SSH. These inject GEMCP_DATASET_* at start. Public Elastic bindings live under /root/autodl-fs/.",
	}, server.listDatasetBindings)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "register_dataset_binding", Description: "Register a dataset root for this Project. For Public Elastic use /root/autodl-fs/ plus optional catalog=scanobjectnn-objbg and allowlisted HTTPS sources. For local CPU use catalog=modelnet40-mini and backend=ssh_cloud; that fixture accepts submit scope when GEMCP_LOCAL_PROCESS_ENABLED is on. Requires configure for other bindings. This never uploads data; prepare_experiment with runtime_preset=provision downloads the sources.",
	}, server.registerDatasetBinding)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "remove_dataset_binding", Description: "Disable one Project dataset binding. Requires configure scope.",
	}, server.removeDatasetBinding)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "register_environment", Description: "Register a Provider-visible AutoDL image as a Project Environment, or ensure the Cloud SSH host Environment (backend=ssh_cloud, image_uuid=host/cpu/local). AutoDL registration requires configure. The local CPU host Environment accepts submit when GEMCP_LOCAL_PROCESS_ENABLED is on. Official image-* UUIDs remain Owner-only unless already used on the Project.",
	}, server.registerEnvironment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "remove_environment", Description: "Disable one Project Environment. Requires configure scope.",
	}, server.removeEnvironment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "request_image_bake", Description: "Request a zero-cost AutoDL Pro image bake. This persists a requested record with a confirmation digest and does not create a Pro instance, Experiment, Graph node, or budget reservation. Owner digest confirmation in the console starts the bake. Requires configure.",
	}, server.requestImageBake)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_image_bake", Description: "Get one Project image bake, including status and a finished image_uuid. This never starts Pro.",
	}, server.getImageBake)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_image_bakes", Description: "List recent Project image bakes. This never starts Pro.",
	}, server.listImageBakes)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_research_workspace", Description: "Return Studies, the selected iteration plan, and the research Graph for the authenticated Project. This never starts a workload.",
	}, server.getResearchWorkspace)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "update_research_workspace", Description: "Create or update a Study, replace the active iteration plan, or record a Graph node. This never starts a workload. Requires submit scope.",
	}, server.updateResearchWorkspace)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_next_actions", Description: "Return the next scientific step for the selected Study from its hypotheses, runs, and observations. Call this before prepare_experiment or close_run.",
	}, server.getNextActions)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "close_run", Description: "Record a result and a highlight observation on a terminal Experiment that already has a Graph run. The observation is linked to the originating hypothesis. Copy the metric from get_experiment or omit it to use the prepared expected_metric. Optional highlight sets the observation title. Do not infer metrics from logs. An optional full result_commit_sha can attach its durable Git manifest commit. Requires submit scope.",
	}, server.closeRun)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_experiment_catalog", Description: "Return registered repository identity plus extracted experiment catalog rows (Setting, method, implementation, metric, result, link, hash, branch). This never starts a workload.",
	}, server.getExperimentCatalog)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "record_experiment_catalog", Description: "Persist raw experiment rows extracted from research branches of a registered GitHub repository. Each row needs Setting, method (方法), implementation (实现), metric, result (结果), link, and hash. This never starts a workload. Requires submit scope.",
	}, server.recordExperimentCatalog)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "report_agent_activity", Description: "Report a controlled workflow phase so the Owner console can show what the Agent is doing without collecting prompts or reasoning.",
	}, server.reportAgentActivity)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "prepare_experiment", Description: "Prepare a zero-cost immutable argv proposal. Pass argv for a one-shot command, or workload plus optional parameters to resolve a reviewed gemcp.yaml at the verified commit root. If that file is missing or has no matching name, a Project workload saved from a succeeded one-shot is used. runtime_preset may be smoke (300s), probe (3600s), train (up to the Project max runtime), or provision (Gemcp-owned AutoDL dataset fetch; omit argv and workload). Optional install_dependencies runs python -m pip install --user from the verified commit. When the Project has an active Study, from_node_id must be a connected hypothesis or a plan under that Study and is bound into the confirmation digest. Isolated nodes cannot prepare. For Cloud SSH, omit image and repository; pass argv and optional cwd. Do not invent SSH credentials, wrap argv in a shell, or write wget/curl/conda.",
	}, server.prepareExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "register_ssh_cloud_node", Description: "Register a Cloud SSH host for this Project. Accepts an ssh command line or host/port/user plus a password or private key. Credentials are write-only and never returned. Remote hosts require operate_nodes. The loopback CPU stub (empty host or 127.0.0.1) accepts submit when GEMCP_LOCAL_PROCESS_ENABLED is on and is probed immediately.",
	}, server.registerSSHCloudNode)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "rotate_ssh_cloud_node_credential", Description: "Replace the encrypted SSH password or private key for one Cloud SSH node. Credentials are write-only. Requires operate_nodes scope.",
	}, server.rotateSSHCloudNodeCredential)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "submit_prepared_experiment", Description: "Submit one confirmed prepared proposal by ID and exact confirmation digest. Identical retries return the same Experiment and bind its Graph run node.",
	}, server.submitPreparedExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "submit_experiment", Description: "Advanced compatibility path: verify a full commit and enqueue an arbitrary shell command using a caller-managed idempotency key. Rejected when the Project has an active Study; use prepare_experiment with from_node_id instead.",
	}, server.submitExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_experiment", Description: "Get the current state, immutable specification, bounded log_tail, metrics.json projection, registered artifacts, and settlement for one project experiment. This is the monitoring surface; it never exposes SSH or remote files.",
	}, server.getExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_experiments", Description: "List recent project experiments, optionally filtered by state.",
	}, server.listExperiments)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "cancel_experiment", Description: "Request cancellation. A queued experiment is cancelled immediately and its reservation is released.",
	}, server.cancelExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_artifacts", Description: "Return the durable output path and registered artifacts for an experiment.",
	}, server.listArtifacts)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_project_cost", Description: "Return the current project budget period, reservations, estimated charges, and available capacity.",
	}, server.getProjectCost)
	mcpServer.AddResource(&mcp.Resource{
		Name: "agent-guide", Title: "Gemcp Agent Operating Guide", Description: "Mandatory workflow for safe, immutable, budget-approved Gemcp experiments.",
		URI: guides.AgentResourceURI, MIMEType: "text/markdown",
	}, server.readAgentGuide)
	mcpServer.AddPrompt(&mcp.Prompt{
		Name: guides.AgentPromptName, Title: "Operate Gemcp safely", Description: "Load the required preflight, approval, submission, monitoring, and cancellation workflow.",
	}, server.operateGemcpPrompt)

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, &mcp.StreamableHTTPOptions{
		SessionTimeout:             30 * time.Minute,
		DisableLocalhostProtection: true,
	})
	crossOrigin := http.NewCrossOriginProtection()
	protected := crossOrigin.Handler(http.MaxBytesHandler(primeStandaloneSSE(streamable), maxMCPRequestBytes))
	server.handler = mcpauth.RequireBearerToken(server.verifyToken, nil)(protected)
	return server
}

func (s *Server) Handler() http.Handler { return s.handler }

// primeStandaloneSSE sends a valid SSE comment when the SDK first flushes an
// authenticated standalone stream. Some reverse proxies buffer headers until
// the first body bytes, which otherwise prevents clients from finishing setup.
func primeStandaloneSSE(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			w = &ssePrimerWriter{ResponseWriter: w}
		}
		next.ServeHTTP(w, request)
	})
}

type ssePrimerWriter struct {
	http.ResponseWriter
	primed bool
}

func (w *ssePrimerWriter) FlushError() error {
	if !w.primed && w.Header().Get("Content-Type") == "text/event-stream" {
		w.primed = true
		if _, err := w.ResponseWriter.Write([]byte(ssePrimer)); err != nil {
			return err
		}
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *ssePrimerWriter) Flush() { _ = w.FlushError() }

func (w *ssePrimerWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) getUsageGuide(_ context.Context, request *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, UsageGuide, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, UsageGuide{}, err
	}
	if !principal.HasScope("read") {
		return nil, UsageGuide{}, s.toolError("get_usage_guide", experiment.ErrForbidden)
	}
	return nil, UsageGuide{
		ProjectID: principal.ProjectPublicID, TokenScopes: append([]string(nil), principal.Scopes...),
		ResourceURI: guides.AgentResourceURI, PromptName: guides.AgentPromptName, Markdown: guides.AgentMCP(),
	}, nil
}

func (s *Server) readAgentGuide(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI: request.Params.URI, MIMEType: "text/markdown", Text: guides.AgentMCP(),
	}}}, nil
}

func (s *Server) operateGemcpPrompt(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Apply the Gemcp safety and approval workflow before operating project experiments.",
		Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: guides.AgentMCP()}}},
	}, nil
}

func (s *Server) verifyToken(ctx context.Context, raw string, _ *http.Request) (*mcpauth.TokenInfo, error) {
	principal, err := s.agentAuth.Authenticate(ctx, raw)
	if errors.Is(err, agentauth.ErrInvalidToken) {
		return nil, mcpauth.ErrInvalidToken
	}
	if err != nil {
		s.logger.Error("verify MCP Agent token", "error", err)
		return nil, errors.New("Agent token verification failed")
	}
	if s.sshCloud != nil && s.sshCloud.LocalProcessEnabled() {
		principal = agentauth.LocalCPUScopes(principal)
	}
	expiration := time.Now().UTC().Add(24 * time.Hour)
	if principal.ExpiresAt != nil && principal.ExpiresAt.Before(expiration) {
		expiration = *principal.ExpiresAt
	}
	return &mcpauth.TokenInfo{
		Scopes: principal.Scopes, Expiration: expiration, UserID: principal.TokenPublicID,
		Extra: map[string]any{"gemcp_principal": principal},
	}, nil
}

func (s *Server) getProjectOptions(ctx context.Context, request *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, experiment.ProjectOptions, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.ProjectOptions{}, err
	}
	output, err := s.experiments.Options(ctx, principal)
	return nil, output, s.toolError("get_project_options", err)
}

func (s *Server) listRepositoryRegistrations(ctx context.Context, request *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, gitrepository.ListResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, gitrepository.ListResult{}, err
	}
	if !principal.HasScope("read") {
		return nil, gitrepository.ListResult{}, experiment.ErrForbidden
	}
	if s.repositories == nil {
		return nil, gitrepository.ListResult{}, errors.New("repository configuration service is unavailable")
	}
	views, err := s.repositories.List(ctx, principal.TenantID, principal.ProjectPublicID)
	return nil, gitrepository.ListResult{Repositories: views}, s.configurationToolError("list_repository_registrations", err)
}

func (s *Server) registerRepository(ctx context.Context, request *mcp.CallToolRequest, input gitrepository.AgentInput) (*mcp.CallToolResult, gitrepository.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, gitrepository.View{}, err
	}
	if !principal.HasScope("configure") {
		return nil, gitrepository.View{}, experiment.ErrForbidden
	}
	if s.repositories == nil {
		return nil, gitrepository.View{}, errors.New("repository configuration service is unavailable")
	}
	view, err := s.repositories.CreateForAgent(ctx, principal.TenantID, principal.TokenPublicID, principal.ProjectPublicID, input)
	return nil, view, s.configurationToolError("register_repository", err)
}

func (s *Server) verifyRepository(ctx context.Context, request *mcp.CallToolRequest, input gitrepository.AgentVerifyInput) (*mcp.CallToolResult, gitrepository.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, gitrepository.View{}, err
	}
	if !principal.HasScope("configure") {
		return nil, gitrepository.View{}, experiment.ErrForbidden
	}
	if s.repositories == nil {
		return nil, gitrepository.View{}, errors.New("repository configuration service is unavailable")
	}
	verifyContext, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	view, err := s.repositories.VerifyForAgent(verifyContext, principal.TenantID, principal.TokenPublicID, principal.ProjectPublicID, input)
	return nil, view, s.configurationToolError("verify_repository", err)
}

func (s *Server) listWorkspaceDatasets(ctx context.Context, request *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, workspacecatalog.ListResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, workspacecatalog.ListResult{}, err
	}
	if s.datasets == nil {
		return nil, workspacecatalog.ListResult{}, errors.New("workspace dataset service is unavailable")
	}
	result, err := s.datasets.List(ctx, principal)
	return nil, result, s.configurationToolError("list_workspace_datasets", err)
}

func (s *Server) registerWorkspaceDataset(ctx context.Context, request *mcp.CallToolRequest, input workspacecatalog.RegisterInput) (*mcp.CallToolResult, workspacecatalog.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, workspacecatalog.View{}, err
	}
	if s.datasets == nil {
		return nil, workspacecatalog.View{}, errors.New("workspace dataset service is unavailable")
	}
	view, err := s.datasets.Register(ctx, principal, input)
	return nil, view, s.configurationToolError("register_workspace_dataset", err)
}

func (s *Server) removeWorkspaceDataset(ctx context.Context, request *mcp.CallToolRequest, input workspacecatalog.RemoveInput) (*mcp.CallToolResult, workspacecatalog.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, workspacecatalog.View{}, err
	}
	if s.datasets == nil {
		return nil, workspacecatalog.View{}, errors.New("workspace dataset service is unavailable")
	}
	view, err := s.datasets.Remove(ctx, principal, input)
	return nil, view, s.configurationToolError("remove_workspace_dataset", err)
}

func (s *Server) listDatasetBindings(ctx context.Context, request *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, datasetcatalog.ListResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, datasetcatalog.ListResult{}, err
	}
	if s.bindings == nil {
		return nil, datasetcatalog.ListResult{}, errors.New("dataset binding service is unavailable")
	}
	result, err := s.bindings.List(ctx, principal)
	return nil, result, s.configurationToolError("list_dataset_bindings", err)
}

func (s *Server) registerDatasetBinding(ctx context.Context, request *mcp.CallToolRequest, input datasetcatalog.RegisterInput) (*mcp.CallToolResult, datasetcatalog.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, datasetcatalog.View{}, err
	}
	if s.bindings == nil {
		return nil, datasetcatalog.View{}, errors.New("dataset binding service is unavailable")
	}
	view, err := s.bindings.Register(ctx, principal, input)
	return nil, view, s.configurationToolError("register_dataset_binding", err)
}

func (s *Server) removeDatasetBinding(ctx context.Context, request *mcp.CallToolRequest, input datasetcatalog.RemoveInput) (*mcp.CallToolResult, datasetcatalog.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, datasetcatalog.View{}, err
	}
	if s.bindings == nil {
		return nil, datasetcatalog.View{}, errors.New("dataset binding service is unavailable")
	}
	view, err := s.bindings.Remove(ctx, principal, input)
	return nil, view, s.configurationToolError("remove_dataset_binding", err)
}

func (s *Server) registerEnvironment(ctx context.Context, request *mcp.CallToolRequest, input environmentcatalog.RegisterInput) (*mcp.CallToolResult, environmentcatalog.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, environmentcatalog.View{}, err
	}
	if looksLikeSSHCloudHostEnvironment(input) {
		view, hookErr := s.registerSSHCloudHostEnvironment(ctx, principal, input)
		return nil, view, s.sshCloudToolError("register_environment", hookErr)
	}
	if s.environments == nil {
		return nil, environmentcatalog.View{}, errors.New("environment service is unavailable")
	}
	view, err := s.environments.Register(ctx, principal, input)
	if err != nil && s.sshCloud != nil && s.sshCloud.LocalProcessEnabled() &&
		(errors.Is(err, environmentcatalog.ErrImage) || errors.Is(err, environmentcatalog.ErrForbidden)) {
		fallback, hookErr := s.registerSSHCloudHostEnvironment(ctx, principal, input)
		return nil, fallback, s.sshCloudToolError("register_environment", hookErr)
	}
	return nil, view, s.configurationToolError("register_environment", err)
}

func looksLikeSSHCloudHostEnvironment(input environmentcatalog.RegisterInput) bool {
	backend := strings.ToLower(strings.TrimSpace(input.Backend))
	image := strings.ToLower(strings.TrimSpace(input.ImageUUID))
	switch backend {
	case "ssh_cloud", "ssh-cloud", "cloud-ssh":
		return true
	}
	switch image {
	case "", sshcloud.HostImage, "cpu", "local", "local-cpu", "local_cpu":
		return true
	}
	return false
}

func (s *Server) allowsLocalCPUSubmit(principal agentauth.Principal) bool {
	return principal.HasScope("submit") && s.sshCloud != nil && s.sshCloud.LocalProcessEnabled()
}

func (s *Server) registerSSHCloudHostEnvironment(ctx context.Context, principal agentauth.Principal, input environmentcatalog.RegisterInput) (environmentcatalog.View, error) {
	if !principal.HasScope("configure") && !s.allowsLocalCPUSubmit(principal) {
		return environmentcatalog.View{}, experiment.ErrForbidden
	}
	if s.sshCloud == nil {
		return environmentcatalog.View{}, sshcloud.ErrDisabled
	}
	result, err := s.sshCloud.EnsureForProject(ctx, principal.TenantID, "agent:"+principal.TokenPublicID, principal.ProjectPublicID, sshcloud.HostImage)
	if err != nil {
		return environmentcatalog.View{}, err
	}
	if strings.TrimSpace(result.EnvironmentName) == "" {
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = "register_ssh_cloud_node before register_environment with backend=ssh_cloud and image_uuid=host"
		}
		return environmentcatalog.View{}, &sshcloud.ValidationError{Message: message}
	}
	if s.environments != nil {
		listed, listErr := s.environments.List(ctx, principal)
		if listErr == nil {
			for _, view := range listed.Environments {
				if view.Name == result.EnvironmentName && view.Backend == sshcloud.Backend {
					return view, nil
				}
			}
		}
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = result.EnvironmentName
	}
	return environmentcatalog.View{
		ProjectID: principal.ProjectPublicID, Name: name, Backend: sshcloud.Backend,
		ImageUUID: sshcloud.HostImage, IsDefault: true, Status: "approved",
	}, nil
}

func (s *Server) requestImageBake(ctx context.Context, request *mcp.CallToolRequest, input imagebake.RequestInput) (*mcp.CallToolResult, imagebake.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, imagebake.View{}, err
	}
	if s.bakes == nil {
		return nil, imagebake.View{}, errors.New("image bake service is unavailable")
	}
	view, err := s.bakes.Request(ctx, principal, input)
	return nil, view, s.configurationToolError("request_image_bake", err)
}

func (s *Server) getImageBake(ctx context.Context, request *mcp.CallToolRequest, input imagebake.GetInput) (*mcp.CallToolResult, imagebake.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, imagebake.View{}, err
	}
	if s.bakes == nil {
		return nil, imagebake.View{}, errors.New("image bake service is unavailable")
	}
	view, err := s.bakes.Get(ctx, principal, input.BakeID)
	return nil, view, s.configurationToolError("get_image_bake", err)
}

func (s *Server) listImageBakes(ctx context.Context, request *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, imagebake.ListResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, imagebake.ListResult{}, err
	}
	if s.bakes == nil {
		return nil, imagebake.ListResult{}, errors.New("image bake service is unavailable")
	}
	result, err := s.bakes.List(ctx, principal)
	return nil, result, s.configurationToolError("list_image_bakes", err)
}

func (s *Server) removeEnvironment(ctx context.Context, request *mcp.CallToolRequest, input environmentcatalog.RemoveInput) (*mcp.CallToolResult, environmentcatalog.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, environmentcatalog.View{}, err
	}
	if s.environments == nil {
		return nil, environmentcatalog.View{}, errors.New("environment service is unavailable")
	}
	view, err := s.environments.Remove(ctx, principal, input)
	return nil, view, s.configurationToolError("remove_environment", err)
}

func (s *Server) getResearchWorkspace(ctx context.Context, request *mcp.CallToolRequest, input research.WorkspaceInput) (*mcp.CallToolResult, research.Workspace, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, research.Workspace{}, err
	}
	if s.research == nil {
		return nil, research.Workspace{}, errors.New("research workspace service is unavailable")
	}
	output, err := s.research.AgentWorkspace(ctx, principal, input)
	return nil, output, s.researchToolError("get_research_workspace", err)
}

func (s *Server) updateResearchWorkspace(ctx context.Context, request *mcp.CallToolRequest, input research.UpdateInput) (*mcp.CallToolResult, research.Workspace, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, research.Workspace{}, err
	}
	if s.research == nil {
		return nil, research.Workspace{}, errors.New("research workspace service is unavailable")
	}
	output, err := s.research.AgentUpdate(ctx, principal, input)
	return nil, output, s.researchToolError("update_research_workspace", err)
}

func (s *Server) getNextActions(ctx context.Context, request *mcp.CallToolRequest, input research.WorkspaceInput) (*mcp.CallToolResult, research.NextActionsView, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, research.NextActionsView{}, err
	}
	if s.research == nil {
		return nil, research.NextActionsView{}, errors.New("research workspace service is unavailable")
	}
	output, err := s.research.AgentNextActions(ctx, principal, input)
	return nil, output, s.researchToolError("get_next_actions", err)
}

func (s *Server) closeRun(ctx context.Context, request *mcp.CallToolRequest, input research.CloseRunInput) (*mcp.CallToolResult, research.Workspace, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, research.Workspace{}, err
	}
	if s.research == nil {
		return nil, research.Workspace{}, errors.New("research workspace service is unavailable")
	}
	output, err := s.research.AgentCloseRun(ctx, principal, input)
	return nil, output, s.researchToolError("close_run", err)
}

func (s *Server) getExperimentCatalog(ctx context.Context, request *mcp.CallToolRequest, input research.CatalogListInput) (*mcp.CallToolResult, research.CatalogView, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, research.CatalogView{}, err
	}
	if s.research == nil {
		return nil, research.CatalogView{}, errors.New("research workspace service is unavailable")
	}
	output, err := s.research.AgentCatalog(ctx, principal, input)
	return nil, output, s.researchToolError("get_experiment_catalog", err)
}

func (s *Server) recordExperimentCatalog(ctx context.Context, request *mcp.CallToolRequest, input research.CatalogRecordInput) (*mcp.CallToolResult, research.CatalogView, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, research.CatalogView{}, err
	}
	if s.research == nil {
		return nil, research.CatalogView{}, errors.New("research workspace service is unavailable")
	}
	output, err := s.research.AgentRecordCatalog(ctx, principal, input)
	return nil, output, s.researchToolError("record_experiment_catalog", err)
}

func (s *Server) reportAgentActivity(ctx context.Context, request *mcp.CallToolRequest, input experiment.ReportActivityInput) (*mcp.CallToolResult, experiment.ReportActivityResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.ReportActivityResult{}, err
	}
	output, err := s.experiments.ReportActivity(ctx, principal, input)
	return nil, output, s.toolError("report_agent_activity", err)
}

func (s *Server) submitExperiment(ctx context.Context, request *mcp.CallToolRequest, input experiment.SubmitInput) (*mcp.CallToolResult, experiment.SubmitResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.SubmitResult{}, err
	}
	output, err := s.experiments.Submit(ctx, principal, input)
	return nil, output, s.toolError("submit_experiment", err)
}

func (s *Server) prepareExperiment(ctx context.Context, request *mcp.CallToolRequest, input experiment.PrepareInput) (*mcp.CallToolResult, experiment.PrepareResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.PrepareResult{}, err
	}
	output, err := s.experiments.Prepare(ctx, principal, input)
	return nil, output, s.toolError("prepare_experiment", err)
}

func (s *Server) submitPreparedExperiment(ctx context.Context, request *mcp.CallToolRequest, input experiment.SubmitPreparedInput) (*mcp.CallToolResult, experiment.SubmitPreparedResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.SubmitPreparedResult{}, err
	}
	output, err := s.experiments.SubmitPrepared(ctx, principal, input)
	return nil, output, s.toolError("submit_prepared_experiment", err)
}

func (s *Server) getExperiment(ctx context.Context, request *mcp.CallToolRequest, input experiment.GetInput) (*mcp.CallToolResult, experiment.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.View{}, err
	}
	output, err := s.experiments.Get(ctx, principal, input.ExperimentID)
	return nil, output, s.toolError("get_experiment", err)
}

func (s *Server) listExperiments(ctx context.Context, request *mcp.CallToolRequest, input experiment.ListInput) (*mcp.CallToolResult, experiment.ListResult, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.ListResult{}, err
	}
	output, err := s.experiments.List(ctx, principal, input)
	return nil, output, s.toolError("list_experiments", err)
}

func (s *Server) cancelExperiment(ctx context.Context, request *mcp.CallToolRequest, input experiment.CancelInput) (*mcp.CallToolResult, experiment.View, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.View{}, err
	}
	output, err := s.experiments.Cancel(ctx, principal, input.ExperimentID)
	return nil, output, s.toolError("cancel_experiment", err)
}

func (s *Server) listArtifacts(ctx context.Context, request *mcp.CallToolRequest, input experiment.GetInput) (*mcp.CallToolResult, experiment.ArtifactView, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.ArtifactView{}, err
	}
	output, err := s.experiments.Artifacts(ctx, principal, input.ExperimentID)
	return nil, output, s.toolError("list_artifacts", err)
}

func (s *Server) registerSSHCloudNode(ctx context.Context, request *mcp.CallToolRequest, input sshcloud.CreateInput) (*mcp.CallToolResult, sshcloud.NodeView, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, sshcloud.NodeView{}, err
	}
	localCPU := s.sshCloud != nil && s.sshCloud.LocalProcessEnabled() && sshcloud.LooksLikeLocalProcessNode(input)
	if !principal.HasScope("operate_nodes") && !(localCPU && principal.HasScope("submit")) {
		return nil, sshcloud.NodeView{}, experiment.ErrForbidden
	}
	if s.sshCloud == nil {
		return nil, sshcloud.NodeView{}, sshcloud.ErrDisabled
	}
	probe := localCPU
	input.Probe = &probe
	input.ProjectID = principal.ProjectPublicID
	view, err := s.sshCloud.Create(ctx, principal.TenantID, "agent:"+principal.TokenPublicID, input)
	return nil, view, s.sshCloudToolError("register_ssh_cloud_node", err)
}

func (s *Server) rotateSSHCloudNodeCredential(ctx context.Context, request *mcp.CallToolRequest, input sshCloudRotateToolInput) (*mcp.CallToolResult, sshcloud.NodeView, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, sshcloud.NodeView{}, err
	}
	if !principal.HasScope("operate_nodes") {
		return nil, sshcloud.NodeView{}, experiment.ErrForbidden
	}
	if s.sshCloud == nil {
		return nil, sshcloud.NodeView{}, sshcloud.ErrDisabled
	}
	probe := false
	view, err := s.sshCloud.Rotate(ctx, principal.TenantID, "agent:"+principal.TokenPublicID, input.NodeID, sshcloud.RotateInput{
		AuthMethod: input.AuthMethod, Password: input.Password, PrivateKey: input.PrivateKey, Passphrase: input.Passphrase, Probe: &probe,
	})
	return nil, view, s.sshCloudToolError("rotate_ssh_cloud_node_credential", err)
}

type sshCloudRotateToolInput struct {
	NodeID     string `json:"node_id" jsonschema:"Cloud SSH node ID"`
	AuthMethod string `json:"auth_method,omitempty" jsonschema:"password or private_key"`
	Password   string `json:"password,omitempty" jsonschema:"replacement SSH password; write-only"`
	PrivateKey string `json:"private_key,omitempty" jsonschema:"replacement SSH private key; write-only"`
	Passphrase string `json:"passphrase,omitempty" jsonschema:"optional private-key passphrase; write-only"`
}

func (s *Server) getProjectCost(ctx context.Context, request *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, experiment.CostView, error) {
	principal, err := principalFrom(request)
	if err != nil {
		return nil, experiment.CostView{}, err
	}
	output, err := s.experiments.Cost(ctx, principal)
	return nil, output, s.toolError("get_project_cost", err)
}

func principalFrom(request *mcp.CallToolRequest) (agentauth.Principal, error) {
	if request == nil || request.Extra == nil || request.Extra.TokenInfo == nil {
		return agentauth.Principal{}, errors.New("authenticated Agent identity is unavailable")
	}
	principal, ok := request.Extra.TokenInfo.Extra["gemcp_principal"].(agentauth.Principal)
	if !ok {
		return agentauth.Principal{}, errors.New("authenticated Agent identity is invalid")
	}
	return principal, nil
}

func (s *Server) toolError(tool string, err error) error {
	if err == nil {
		return nil
	}
	var validation *experiment.ValidationError
	if errors.As(err, &validation) {
		return errors.New(validation.Message)
	}
	for _, public := range []error{
		experiment.ErrForbidden, experiment.ErrNotFound, experiment.ErrOptionNotFound,
		experiment.ErrIdempotencyConflict, experiment.ErrBudgetExceeded, experiment.ErrExperimentCap,
		experiment.ErrCommitVerification, experiment.ErrProjectPaused, experiment.ErrProposalNotFound,
		experiment.ErrProposalExpired, experiment.ErrProposalChanged, experiment.ErrProposalBlocked,
	} {
		if errors.Is(err, public) {
			return public
		}
	}
	s.logger.Error("MCP tool failed", "tool", tool, "error", err)
	return errors.New("internal control-plane error")
}

func (s *Server) researchToolError(tool string, err error) error {
	if err == nil {
		return nil
	}
	var validation *research.ValidationError
	if errors.As(err, &validation) {
		return errors.New(validation.Message)
	}
	for _, public := range []error{
		research.ErrForbidden, research.ErrNotFound, research.ErrChoice, research.ErrStudyLimit,
		research.ErrNodeLimit, research.ErrEdgeLimit, research.ErrStudyConflict, research.ErrCatalogLimit,
	} {
		if errors.Is(err, public) {
			return public
		}
	}
	s.logger.Error("MCP research tool failed", "tool", tool, "error", err)
	return errors.New("internal control-plane error")
}

func (s *Server) sshCloudToolError(tool string, err error) error {
	if err == nil {
		return nil
	}
	var validation *sshcloud.ValidationError
	if errors.As(err, &validation) {
		return errors.New(validation.Message)
	}
	for _, public := range []error{
		experiment.ErrForbidden, sshcloud.ErrDisabled, sshcloud.ErrNotFound, sshcloud.ErrProject,
		sshcloud.ErrHostKeyChanged, sshcloud.ErrBusy, sshcloud.ErrNodeLimit,
	} {
		if errors.Is(err, public) {
			return public
		}
	}
	s.logger.Error("MCP Cloud SSH tool failed", "tool", tool, "error", err)
	return errors.New("internal control-plane error")
}

func (s *Server) configurationToolError(tool string, err error) error {
	if err == nil {
		return nil
	}
	var repositoryValidation *gitrepository.ValidationError
	if errors.As(err, &repositoryValidation) {
		return errors.New(repositoryValidation.Message)
	}
	var datasetValidation *workspacecatalog.ValidationError
	if errors.As(err, &datasetValidation) {
		return errors.New(datasetValidation.Message)
	}
	var bindingValidation *datasetcatalog.ValidationError
	if errors.As(err, &bindingValidation) {
		return errors.New(bindingValidation.Message)
	}
	var environmentValidation *environmentcatalog.ValidationError
	if errors.As(err, &environmentValidation) {
		return errors.New(environmentValidation.Message)
	}
	var bakeValidation *imagebake.ValidationError
	if errors.As(err, &bakeValidation) {
		return errors.New(bakeValidation.Message)
	}
	for _, public := range []error{
		experiment.ErrForbidden,
		gitrepository.ErrNotFound, gitrepository.ErrNotActive, gitrepository.ErrVerificationFailed, gitrepository.ErrConflict,
		workspacecatalog.ErrForbidden, workspacecatalog.ErrNotFound, workspacecatalog.ErrTrustedWorkspace,
		workspacecatalog.ErrWorkspaceChoice, workspacecatalog.ErrDatasetConflict, workspacecatalog.ErrDatasetLimit,
		datasetcatalog.ErrForbidden, datasetcatalog.ErrNotFound, datasetcatalog.ErrProject, datasetcatalog.ErrConflict, datasetcatalog.ErrLimit,
		environmentcatalog.ErrForbidden, environmentcatalog.ErrNotFound, environmentcatalog.ErrProject, environmentcatalog.ErrConflict,
		environmentcatalog.ErrLimit, environmentcatalog.ErrImage,
		imagebake.ErrForbidden, imagebake.ErrNotFound, imagebake.ErrProject, imagebake.ErrBusy,
		imagebake.ErrDigestMismatch, imagebake.ErrNotRequested, imagebake.ErrNotCancellable,
	} {
		if errors.Is(err, public) {
			return public
		}
	}
	s.logger.Error("MCP configuration tool failed", "tool", tool, "error", err)
	return errors.New("internal control-plane error")
}
