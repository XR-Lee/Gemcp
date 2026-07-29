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

func (h *AgentTokenHandlers) IssueEnrollment(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input agentaccess.EnrollmentIssueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.IssueEnrollment(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *AgentTokenHandlers) ClaimEnrollment(c *gin.Context) {
	var input struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.ClaimEnrollment(c.Request.Context(), input.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AgentTokenHandlers) CompleteEnrollment(c *gin.Context) {
	var input struct {
		Code      string   `json:"code"`
		Client    string   `json:"client"`
		ToolCount int      `json:"tool_count"`
		Checks    []string `json:"checks"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.CompleteEnrollment(c.Request.Context(), input.Code, agentaccess.EnrollmentCompleteInput{
		Client: input.Client, ToolCount: input.ToolCount, Checks: input.Checks,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AgentTokenHandlers) RevokeEnrollment(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.RevokeEnrollment(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("enrollmentID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
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

func (h *AgentTokenHandlers) UpdateScopes(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input agentaccess.UpdateScopesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.UpdateScopes(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("tokenID"), input)
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
	case errors.Is(err, agentaccess.ErrEnrollmentLimit):
		writeError(c, http.StatusConflict, "AGENT_SETUP_LIMIT", "this project already has the maximum number of active Agent setup links")
	case errors.Is(err, agentaccess.ErrEnrollmentState):
		writeError(c, http.StatusConflict, "AGENT_SETUP_STATE", "completed Agent setup links are managed through their issued Agent Token")
	case errors.Is(err, agentaccess.ErrEnrollmentInvalid):
		writeError(c, http.StatusGone, "AGENT_SETUP_INVALID", "Agent setup link is invalid, expired, revoked, or already completed")
	case ent.IsConstraintError(err):
		writeError(c, http.StatusConflict, "AGENT_TOKEN_CONFLICT", "Agent token could not be created due to a uniqueness conflict")
	default:
		slog.Error("Agent token operation failed", "error", err)
		writeError(c, http.StatusInternalServerError, "AGENT_TOKEN_OPERATION_FAILED", "Agent token operation failed")
	}
}
