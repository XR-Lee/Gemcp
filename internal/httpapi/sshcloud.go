package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/gin-gonic/gin"
)

type SSHCloudHandlers struct {
	service *sshcloud.Service
}

func NewSSHCloudHandlers(service *sshcloud.Service) *SSHCloudHandlers {
	return &SSHCloudHandlers{service: service}
}

func (h *SSHCloudHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	if h.service == nil || !h.service.Enabled() {
		writeError(c, http.StatusServiceUnavailable, "SSH_CLOUD_DISABLED", "Cloud SSH execution is not enabled")
		return
	}
	result, err := h.service.List(c.Request.Context(), principal.TenantID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *SSHCloudHandlers) Create(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input sshcloud.CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.Create(c.Request.Context(), principal.TenantID, principal.UserPublicID, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *SSHCloudHandlers) Probe(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.Probe(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *SSHCloudHandlers) Rotate(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input sshcloud.RotateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	result, err := h.service.Rotate(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *SSHCloudHandlers) Revoke(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.Revoke(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *SSHCloudHandlers) writeError(c *gin.Context, err error) {
	var validation *sshcloud.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_SSH_CLOUD_NODE", validation.Error())
	case errors.Is(err, sshcloud.ErrDisabled):
		writeError(c, http.StatusServiceUnavailable, "SSH_CLOUD_DISABLED", "Cloud SSH execution is not enabled")
	case errors.Is(err, sshcloud.ErrNotFound):
		writeError(c, http.StatusNotFound, "SSH_CLOUD_NODE_NOT_FOUND", "Cloud SSH node not found")
	case errors.Is(err, sshcloud.ErrProject):
		writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
	case errors.Is(err, sshcloud.ErrHostKeyChanged):
		writeError(c, http.StatusConflict, "SSH_CLOUD_HOST_KEY_CHANGED", "remote SSH host key changed; probe is blocked until the Owner reviews the node")
	case errors.Is(err, sshcloud.ErrBusy):
		writeError(c, http.StatusConflict, "SSH_CLOUD_NODE_BUSY", "Cloud SSH node has an active Assignment")
	case errors.Is(err, sshcloud.ErrNodeLimit):
		writeError(c, http.StatusConflict, "SSH_CLOUD_NODE_LIMIT", "Cloud SSH node limit reached")
	default:
		slog.Error("Cloud SSH operation failed", "error", err)
		writeError(c, http.StatusInternalServerError, "SSH_CLOUD_OPERATION_FAILED", "Cloud SSH operation failed")
	}
}
