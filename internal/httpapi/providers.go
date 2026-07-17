package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	providerservice "github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/gin-gonic/gin"
)

type providerOperations interface {
	Summary(context.Context, int) (providerservice.Summary, error)
	QueryResources(context.Context, int) (providerservice.ResourceSnapshot, error)
	Configure(context.Context, int, string, providerservice.ConfigureInput) (providerservice.ConfigureResult, error)
	Deployment(context.Context, int, string) (providerservice.DeploymentDetails, error)
}

type ProviderHandlers struct {
	service providerOperations
}

func NewProviderHandlers(service providerOperations) *ProviderHandlers {
	return &ProviderHandlers{service: service}
}

func (h *ProviderHandlers) Summary(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	view, err := h.service.Summary(c.Request.Context(), principal.TenantID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *ProviderHandlers) Query(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	view, err := h.service.QueryResources(ctx, principal.TenantID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *ProviderHandlers) Configure(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input providerservice.ConfigureInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	view, err := h.service.Configure(ctx, principal.TenantID, principal.UserPublicID, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *ProviderHandlers) Deployment(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	view, err := h.service.Deployment(ctx, principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func ownerPrincipal(c *gin.Context) (principalView, bool) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return principalView{}, false
	}
	if principal.Role != "owner" {
		writeError(c, http.StatusForbidden, "OWNER_REQUIRED", "Owner access is required")
		return principalView{}, false
	}
	return principalView{TenantID: principal.TenantID, UserPublicID: principal.UserPublicID}, true
}

type principalView struct {
	TenantID     int
	UserPublicID string
}

func (h *ProviderHandlers) writeError(c *gin.Context, err error) {
	var validation *providerservice.ValidationError
	var operation *providerservice.OperationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_PROVIDER_CONFIGURATION", validation.Error())
	case errors.Is(err, providerservice.ErrNotFound):
		writeError(c, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider account not found")
	case errors.Is(err, providerservice.ErrDeploymentNotFound):
		writeError(c, http.StatusNotFound, "PROVIDER_DEPLOYMENT_NOT_FOUND", "Provider deployment not found")
	case errors.Is(err, providerservice.ErrUnsupportedBackend):
		writeError(c, http.StatusConflict, "PROVIDER_BACKEND_UNSUPPORTED", "Configure the Provider for AutoDL Private Cloud")
	case errors.As(err, &operation):
		slog.Warn("Provider query failed", "operation", operation.Operation, "error", operation.Cause)
		writeError(c, http.StatusBadGateway, "PROVIDER_QUERY_FAILED", operation.Error())
	default:
		slog.Error("Provider operation failed", "error", err)
		writeError(c, http.StatusInternalServerError, "PROVIDER_OPERATION_FAILED", "Provider operation failed")
	}
}
