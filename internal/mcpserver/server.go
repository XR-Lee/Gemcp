package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/XR-Lee/Gemcp/guides"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/research"
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
	research     *research.Service
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

const serverInstructions = "Keep the Owner-facing research Graph current with get_research_workspace and update_research_workspace: a Study, iteration plan, and typed Graph are the human-visible work. Never include prompts, private reasoning, credentials, or environment dumps in research text. Report only controlled Agent workflow phases with report_agent_activity. Use prepare_experiment as the normal zero-cost execution path; wait for human approval before submit_prepared_experiment. Linking an Experiment to a Graph node never starts a workload. Use submit_experiment only as the Advanced shell-command compatibility path."

type Option func(*Server)

func WithConfiguration(repositories *gitrepository.Service, datasets *workspacecatalog.Service) Option {
	return func(server *Server) {
		server.repositories = repositories
		server.datasets = datasets
	}
}

func WithResearch(service *research.Service) Option {
	return func(server *Server) {
		server.research = service
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
		Name: "get_project_options", Description: "List project policy, approved execution options, and automatically discovered authorized Self-hosted Node readiness.",
	}, server.getProjectOptions)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_repository_registrations", Description: "List active and pending Git repositories for the authenticated Project, including public deploy keys.",
	}, server.listRepositoryRegistrations)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "register_repository", Description: "Create a pending GitHub SSH repository registration in the authenticated Project and return its read-only deploy public key. Requires configure scope.",
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
		Name: "get_research_workspace", Description: "Return Studies, the selected iteration plan, and the research Graph for the authenticated Project. This never starts a workload.",
	}, server.getResearchWorkspace)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "update_research_workspace", Description: "Create or update a Study, replace the active iteration plan, or record a Graph node. This never starts a workload. Requires submit scope.",
	}, server.updateResearchWorkspace)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "report_agent_activity", Description: "Report a controlled workflow phase so the Owner console can show what the Agent is doing without collecting prompts or reasoning.",
	}, server.reportAgentActivity)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "prepare_experiment", Description: "Prepare a zero-cost immutable argv proposal. Gemcp resolves repository, ref, compatible defaults, capacity, cost, and idempotency; an image tag or digest may be selected only for an Owner-approved trusted Self-hosted workspace.",
	}, server.prepareExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "submit_prepared_experiment", Description: "Submit one confirmed prepared proposal by ID and exact confirmation digest. Identical retries return the same Experiment.",
	}, server.submitPreparedExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "submit_experiment", Description: "Advanced compatibility path: verify a full commit and enqueue an arbitrary shell command using a caller-managed idempotency key.",
	}, server.submitExperiment)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_experiment", Description: "Get the current state and immutable specification of one project experiment.",
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
		research.ErrNodeLimit, research.ErrEdgeLimit, research.ErrStudyConflict,
	} {
		if errors.Is(err, public) {
			return public
		}
	}
	s.logger.Error("MCP research tool failed", "tool", tool, "error", err)
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
	for _, public := range []error{
		experiment.ErrForbidden,
		gitrepository.ErrNotFound, gitrepository.ErrNotActive, gitrepository.ErrVerificationFailed, gitrepository.ErrConflict,
		workspacecatalog.ErrForbidden, workspacecatalog.ErrNotFound, workspacecatalog.ErrTrustedWorkspace,
		workspacecatalog.ErrWorkspaceChoice, workspacecatalog.ErrDatasetConflict, workspacecatalog.ErrDatasetLimit,
	} {
		if errors.Is(err, public) {
			return public
		}
	}
	s.logger.Error("MCP configuration tool failed", "tool", tool, "error", err)
	return errors.New("internal control-plane error")
}
