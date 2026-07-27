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
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxMCPRequestBytes = 1 << 20
	ssePrimer          = ": connected\n\n"
)

type Server struct {
	agentAuth   *agentauth.Service
	experiments *experiment.Service
	logger      *slog.Logger
	handler     http.Handler
}

type emptyInput struct{}

type UsageGuide struct {
	ProjectID   string   `json:"project_id"`
	TokenScopes []string `json:"token_scopes"`
	ResourceURI string   `json:"resource_uri"`
	PromptName  string   `json:"prompt_name"`
	Markdown    string   `json:"markdown"`
}

const serverInstructions = "Use prepare_experiment as the normal zero-cost path: provide a reviewed argv and optional repository/ref selectors, let Gemcp resolve immutable source, defaults, checks, cost, and server-side idempotency, then show the returned proposal and wait for human approval before submit_prepared_experiment. Use submit_experiment only as the Advanced shell-command compatibility path. Record the Experiment ID and monitor it to a terminal state."

func New(agentAuth *agentauth.Service, experiments *experiment.Service, version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{agentAuth: agentAuth, experiments: experiments, logger: logger}
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "gemcp", Version: version}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_usage_guide", Description: "Return the mandatory Gemcp operating guide, approval boundary, and safe submission workflow.",
	}, server.getUsageGuide)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_project_options", Description: "List the project policy and approved repositories, environments, and resource profiles.",
	}, server.getProjectOptions)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "prepare_experiment", Description: "Prepare a zero-cost immutable argv proposal. Gemcp resolves an unambiguous repository, ref, compatible defaults, preflight checks, cost, and idempotency.",
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
