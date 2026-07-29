package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/nodeaccess"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/selfhosted"
	"github.com/gin-gonic/gin"
)

type NodeHandlers struct {
	service     *nodeaccess.Service
	assignments *selfhosted.Service
}

func NewNodeHandlers(service *nodeaccess.Service, assignments ...*selfhosted.Service) *NodeHandlers {
	handlers := &NodeHandlers{service: service}
	if len(assignments) > 0 {
		handlers.assignments = assignments[0]
	}
	return handlers
}

func (h *NodeHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), principal.TenantID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) IssueEnrollment(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input nodeaccess.EnrollmentIssueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.IssueEnrollment(c.Request.Context(), principal.TenantID, principal.UserPublicID, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *NodeHandlers) ClaimEnrollment(c *gin.Context) {
	var input nodeprotocol.EnrollmentClaimRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.ClaimEnrollment(c.Request.Context(), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) ApproveEnrollment(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input nodeaccess.ApprovalInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.ApproveEnrollment(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) RevokeEnrollment(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.RevokeEnrollment(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) ListRuntimeConfigs(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	if h.assignments == nil {
		writeError(c, http.StatusServiceUnavailable, "SELF_HOSTED_DISABLED", "Self-hosted execution is not enabled")
		return
	}
	result, err := h.assignments.ListRuntimeConfigs(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) CreateRuntimeConfig(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	if h.assignments == nil {
		writeError(c, http.StatusServiceUnavailable, "SELF_HOSTED_DISABLED", "Self-hosted execution is not enabled")
		return
	}
	var input selfhosted.RuntimeConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.assignments.CreateRuntimeConfig(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *NodeHandlers) EnableTrustedWorkspace(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	if h.assignments == nil {
		writeError(c, http.StatusServiceUnavailable, "SELF_HOSTED_DISABLED", "Self-hosted execution is not enabled")
		return
	}
	var input selfhosted.TrustedWorkspaceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.assignments.EnableTrustedWorkspace(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) DisableTrustedWorkspace(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	if h.assignments == nil {
		writeError(c, http.StatusServiceUnavailable, "SELF_HOSTED_DISABLED", "Self-hosted execution is not enabled")
		return
	}
	result, err := h.assignments.DisableTrustedWorkspace(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("node_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) Sync(c *gin.Context) {
	token, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		writeError(c, http.StatusUnauthorized, "NODE_UNAUTHENTICATED", "valid Node authentication is required")
		return
	}
	var input nodeprotocol.SyncRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.Sync(c.Request.Context(), token, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NodeHandlers) Source(c *gin.Context) {
	token, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		writeError(c, http.StatusUnauthorized, "NODE_UNAUTHENTICATED", "valid Node authentication is required")
		return
	}
	principal, err := h.service.Authenticate(c.Request.Context(), token)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if h.assignments == nil {
		writeError(c, http.StatusServiceUnavailable, "SELF_HOSTED_DISABLED", "Self-hosted execution is not enabled")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	archive, err := h.assignments.Source(ctx, principal.NodeID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	defer archive.Close()
	file, err := archive.Open()
	if err != nil {
		h.writeError(c, err)
		return
	}
	defer file.Close()
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Length", strconv.FormatInt(archive.SizeBytes(), 10))
	c.Header("Content-Disposition", `attachment; filename="source.tar.gz"`)
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, file); err != nil {
		slog.Warn("Self-hosted source stream interrupted", "error", err)
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	return strings.TrimSpace(token), ok && strings.EqualFold(scheme, "Bearer") && strings.TrimSpace(token) != ""
}

func (h *NodeHandlers) writeError(c *gin.Context, err error) {
	var validation *nodeaccess.ValidationError
	var runtimeValidation *selfhosted.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_NODE_REQUEST", validation.Error())
	case errors.As(err, &runtimeValidation):
		writeError(c, http.StatusBadRequest, "INVALID_SELF_HOSTED_RUNTIME", runtimeValidation.Error())
	case errors.Is(err, nodeaccess.ErrDisabled):
		writeError(c, http.StatusServiceUnavailable, "SELF_HOSTED_DISABLED", "Self-hosted nodes are not enabled")
	case errors.Is(err, nodeaccess.ErrPublicURL):
		writeError(c, http.StatusServiceUnavailable, "NODE_PUBLIC_URL_UNAVAILABLE", "GEMCP_PUBLIC_URL must be a credential-free HTTPS origin")
	case errors.Is(err, nodeaccess.ErrNotFound):
		writeError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node or enrollment not found")
	case errors.Is(err, nodeaccess.ErrEnrollmentInvalid):
		writeError(c, http.StatusGone, "NODE_ENROLLMENT_INVALID", "node enrollment is invalid, expired, completed, or revoked")
	case errors.Is(err, nodeaccess.ErrEnrollmentState):
		writeError(c, http.StatusConflict, "NODE_ENROLLMENT_STATE", "node enrollment cannot be changed in its current state")
	case errors.Is(err, nodeaccess.ErrEnrollmentLimit):
		writeError(c, http.StatusConflict, "NODE_ENROLLMENT_LIMIT", "the organization has too many active node enrollments")
	case errors.Is(err, nodeaccess.ErrInvalidToken):
		writeError(c, http.StatusUnauthorized, "NODE_UNAUTHENTICATED", "valid Node authentication is required")
	case errors.Is(err, nodeaccess.ErrProtocol):
		writeError(c, http.StatusUpgradeRequired, "NODE_PROTOCOL_UNSUPPORTED", "node protocol version is not supported")
	case errors.Is(err, nodeaccess.ErrConflict), ent.IsConstraintError(err):
		writeError(c, http.StatusConflict, "NODE_STATE_CONFLICT", "node state conflicts with durable control-plane state")
	case errors.Is(err, selfhosted.ErrAssignment):
		writeError(c, http.StatusNotFound, "NODE_ASSIGNMENT_NOT_FOUND", "node Assignment was not found")
	case errors.Is(err, selfhosted.ErrAssignmentGone):
		writeError(c, http.StatusGone, "NODE_ASSIGNMENT_FINISHED", "node Assignment is no longer active")
	case errors.Is(err, selfhosted.ErrSourceLimit):
		writeError(c, http.StatusTooManyRequests, "NODE_SOURCE_LIMIT", "node source download limit reached")
	case errors.Is(err, selfhosted.ErrInvalidEvent):
		writeError(c, http.StatusConflict, "NODE_EVENT_INVALID", "node workload event is invalid")
	case errors.Is(err, selfhosted.ErrDisabled):
		writeError(c, http.StatusServiceUnavailable, "SELF_HOSTED_DISABLED", "Self-hosted execution is not enabled")
	case errors.Is(err, selfhosted.ErrProject):
		writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
	default:
		slog.Error("node operation failed", "error", err)
		writeError(c, http.StatusInternalServerError, "NODE_OPERATION_FAILED", "node operation failed")
	}
}
