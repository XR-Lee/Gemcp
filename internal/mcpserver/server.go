package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/experiment"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxMCPRequestBytes = 1 << 20

type Server struct {
	agentAuth   *agentauth.Service
	experiments *experiment.Service
	logger      *slog.Logger
	handler     http.Handler
}

type emptyInput struct{}

func New(agentAuth *agentauth.Service, experiments *experiment.Service, version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{agentAuth: agentAuth, experiments: experiments, logger: logger}
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "gemcp", Version: version}, &mcp.ServerOptions{
		Instructions: "Manage immutable, budget-governed experiments in the Agent token's project. Call get_project_options and get_project_cost before submission, use a full Git commit SHA, and reuse an idempotency key only for an identical request.",
	})
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "get_project_options", Description: "List the project policy and approved repositories, environments, and resource profiles.",
	}, server.getProjectOptions)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "submit_experiment", Description: "Verify an immutable Git commit and enqueue a budget-reserved experiment. Retries must reuse the same idempotency key.",
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

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, &mcp.StreamableHTTPOptions{
		SessionTimeout:             30 * time.Minute,
		DisableLocalhostProtection: true,
	})
	crossOrigin := http.NewCrossOriginProtection()
	protected := crossOrigin.Handler(http.MaxBytesHandler(streamable, maxMCPRequestBytes))
	server.handler = mcpauth.RequireBearerToken(server.verifyToken, nil)(protected)
	return server
}

func (s *Server) Handler() http.Handler { return s.handler }

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
		experiment.ErrCommitVerification, experiment.ErrProjectPaused,
	} {
		if errors.Is(err, public) {
			return public
		}
	}
	s.logger.Error("MCP tool failed", "tool", tool, "error", err)
	return errors.New("internal control-plane error")
}
