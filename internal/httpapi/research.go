package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/gin-gonic/gin"
)

type ResearchHandlers struct {
	service *research.Service
}

func NewResearchHandlers(service *research.Service) *ResearchHandlers {
	return &ResearchHandlers{service: service}
}

func (h *ResearchHandlers) Get(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	result, err := h.service.OwnerWorkspace(c.Request.Context(), principal.TenantID, c.Param("id"), c.Query("study_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ResearchHandlers) Catalog(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	result, err := h.service.OwnerCatalog(c.Request.Context(), principal.TenantID, c.Param("id"), c.Query("repository_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ResearchHandlers) Update(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if principal.Role != "owner" {
		writeError(c, http.StatusForbidden, "OWNER_REQUIRED", "Owner access is required")
		return
	}
	var input research.UpdateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_RESEARCH", "valid research workspace JSON is required")
		return
	}
	result, err := h.service.OwnerUpdate(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ResearchHandlers) CloseRun(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input research.CloseRunInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_CLOSE_RUN", "valid close_run JSON is required")
		return
	}
	result, err := h.service.OwnerCloseRun(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ResearchHandlers) PlanSync(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input research.PlanSyncInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_PLAN_SYNC", "valid plan-sync JSON is required")
		return
	}
	result, err := h.service.OwnerExportPlanSync(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ResearchHandlers) writeError(c *gin.Context, err error) {
	var validation *research.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_RESEARCH", validation.Error())
	case errors.Is(err, research.ErrNotFound):
		writeError(c, http.StatusNotFound, "RESEARCH_NOT_FOUND", "study or project not found")
	case errors.Is(err, research.ErrChoice):
		writeError(c, http.StatusConflict, "STUDY_CHOICE_REQUIRED", "study selector is required")
	case errors.Is(err, research.ErrStudyLimit):
		writeError(c, http.StatusConflict, "STUDY_LIMIT", "study limit reached")
	case errors.Is(err, research.ErrNodeLimit):
		writeError(c, http.StatusConflict, "RESEARCH_NODE_LIMIT", "research graph node limit reached")
	case errors.Is(err, research.ErrEdgeLimit):
		writeError(c, http.StatusConflict, "RESEARCH_EDGE_LIMIT", "research graph edge limit reached")
	case errors.Is(err, research.ErrStudyConflict):
		writeError(c, http.StatusConflict, "STUDY_CONFLICT", "study name is already used in this Project")
	case errors.Is(err, research.ErrCatalogLimit):
		writeError(c, http.StatusConflict, "EXPERIMENT_CATALOG_LIMIT", "experiment catalog row limit reached")
	case errors.Is(err, research.ErrForbidden):
		writeError(c, http.StatusForbidden, "FORBIDDEN", "operation is not allowed")
	default:
		slog.Error("research workspace failed", "error", err)
		writeError(c, http.StatusInternalServerError, "RESEARCH_FAILED", "research workspace failed")
	}
}
