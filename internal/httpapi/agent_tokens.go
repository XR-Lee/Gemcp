package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/agentaccess"
	"github.com/gin-gonic/gin"
)

type AgentTokenHandlers struct {
	service *agentaccess.Service
}

func NewAgentTokenHandlers(service *agentaccess.Service) *AgentTokenHandlers {
	return &AgentTokenHandlers{service: service}
}

func (h *AgentTokenHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AgentTokenHandlers) Issue(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input agentaccess.IssueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.Issue(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *AgentTokenHandlers) Revoke(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.Revoke(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("tokenID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AgentTokenHandlers) writeError(c *gin.Context, err error) {
	var validation *agentaccess.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_AGENT_TOKEN", validation.Error())
	case errors.Is(err, agentaccess.ErrNotFound):
		writeError(c, http.StatusNotFound, "AGENT_TOKEN_NOT_FOUND", "Agent token or project not found")
	case errors.Is(err, agentaccess.ErrProjectArchived):
		writeError(c, http.StatusConflict, "PROJECT_ARCHIVED", "archived projects cannot issue new Agent tokens")
	case errors.Is(err, agentaccess.ErrPublicURLUnavailable):
		writeError(c, http.StatusServiceUnavailable, "MCP_PUBLIC_URL_UNAVAILABLE", "GEMCP_PUBLIC_URL must be a credential-free HTTPS origin before exporting MCP configuration")
	case errors.Is(err, agentaccess.ErrActiveTokenLimit):
		writeError(c, http.StatusConflict, "AGENT_TOKEN_LIMIT", "this project already has the maximum number of active Agent tokens")
	case ent.IsConstraintError(err):
		writeError(c, http.StatusConflict, "AGENT_TOKEN_CONFLICT", "Agent token could not be created due to a uniqueness conflict")
	default:
		slog.Error("Agent token operation failed", "error", err)
		writeError(c, http.StatusInternalServerError, "AGENT_TOKEN_OPERATION_FAILED", "Agent token operation failed")
	}
}
