package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/gin-gonic/gin"
)

type RuntimeHandlers struct {
	operations *execution.Operations
}

func NewRuntimeHandlers(operations *execution.Operations) *RuntimeHandlers {
	return &RuntimeHandlers{operations: operations}
}

func (h *RuntimeHandlers) Status(c *gin.Context) {
	if _, ok := ownerPrincipal(c); !ok {
		return
	}
	result, err := h.operations.Status(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RuntimeHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.operations.List(c.Request.Context(), principal.TenantID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RuntimeHandlers) Stop(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.operations.RequestStop(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"data": result})
}

func (h *RuntimeHandlers) EmergencyStop(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Confirmation != "STOP" {
		writeError(c, http.StatusBadRequest, "INVALID_EMERGENCY_CONFIRMATION", "type STOP to confirm emergency shutdown")
		return
	}
	result, err := h.operations.EmergencyStop(c.Request.Context(), principal.TenantID, principal.UserPublicID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"data": result})
}

func (h *RuntimeHandlers) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, execution.ErrManagedResourceNotFound):
		writeError(c, http.StatusNotFound, "MANAGED_RESOURCE_NOT_FOUND", "managed Provider resource not found")
	case errors.Is(err, execution.ErrManagedResourceTerminal):
		writeError(c, http.StatusConflict, "MANAGED_RESOURCE_TERMINAL", "managed Provider resource is already terminal")
	default:
		slog.Error("runtime operations API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "RUNTIME_OPERATION_FAILED", "runtime operation failed")
	}
}
