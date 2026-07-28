package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/experiment"
	"github.com/gin-gonic/gin"
)

type ExperimentHandlers struct {
	service *experiment.Service
}

func NewExperimentHandlers(service *experiment.Service) *ExperimentHandlers {
	return &ExperimentHandlers{service: service}
}

func (h *ExperimentHandlers) List(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	limit := 25
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_LIMIT", "limit must be an integer")
			return
		}
		limit = parsed
	}
	result, err := h.service.OwnerList(c.Request.Context(), principal.TenantID, c.Query("project_id"), experiment.ListInput{
		Limit: limit, States: c.QueryArray("state"),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result.Experiments})
}

func (h *ExperimentHandlers) Get(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	result, err := h.service.OwnerGet(c.Request.Context(), principal.TenantID, c.Query("project_id"), c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ExperimentHandlers) Attempts(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	result, err := h.service.OwnerAttempts(c.Request.Context(), principal.TenantID, c.Query("project_id"), c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ExperimentHandlers) Cost(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	result, err := h.service.OwnerCost(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ExperimentHandlers) Operations(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	limit := 25
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_LIMIT", "limit must be an integer")
			return
		}
		limit = parsed
	}
	result, err := h.service.OwnerOperations(c.Request.Context(), principal.TenantID, c.Param("id"), limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ExperimentHandlers) writeError(c *gin.Context, err error) {
	var validation *experiment.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_EXPERIMENT_QUERY", validation.Error())
	case errors.Is(err, experiment.ErrNotFound):
		writeError(c, http.StatusNotFound, "EXPERIMENT_NOT_FOUND", "experiment or project not found")
	case errors.Is(err, experiment.ErrForbidden):
		writeError(c, http.StatusForbidden, "FORBIDDEN", "operation is not allowed")
	default:
		slog.Error("experiment API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "EXPERIMENT_API_FAILED", "experiment operation failed")
	}
}
