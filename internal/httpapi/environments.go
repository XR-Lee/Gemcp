package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XR-Lee/Gemcp/internal/environmentcatalog"
	"github.com/gin-gonic/gin"
)

type EnvironmentHandlers struct {
	service *environmentcatalog.Service
}

func NewEnvironmentHandlers(service *environmentcatalog.Service) *EnvironmentHandlers {
	return &EnvironmentHandlers{service: service}
}

func (h *EnvironmentHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.OwnerList(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result.Environments})
}

func (h *EnvironmentHandlers) Create(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input environmentcatalog.RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ENVIRONMENT", "name and image_uuid are required")
		return
	}
	view, err := h.service.OwnerRegister(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": view})
}

func (h *EnvironmentHandlers) Remove(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	view, err := h.service.OwnerRemove(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("environmentID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *EnvironmentHandlers) writeError(c *gin.Context, err error) {
	var validation *environmentcatalog.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_ENVIRONMENT", validation.Message)
	case errors.Is(err, environmentcatalog.ErrNotFound):
		writeError(c, http.StatusNotFound, "ENVIRONMENT_NOT_FOUND", "environment not found")
	case errors.Is(err, environmentcatalog.ErrProject):
		writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "active Project not found")
	case errors.Is(err, environmentcatalog.ErrConflict):
		writeError(c, http.StatusConflict, "ENVIRONMENT_CONFLICT", "environment name is already registered")
	case errors.Is(err, environmentcatalog.ErrLimit):
		writeError(c, http.StatusConflict, "ENVIRONMENT_LIMIT", "environment limit reached")
	case errors.Is(err, environmentcatalog.ErrImage):
		writeError(c, http.StatusBadRequest, "ENVIRONMENT_IMAGE", "image UUID is not visible to this Project")
	default:
		slog.Error("environment API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "ENVIRONMENT_FAILED", "environment operation failed")
	}
}
