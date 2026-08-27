package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
	"github.com/gin-gonic/gin"
)

type DatasetBindingHandlers struct {
	service *datasetcatalog.Service
}

func NewDatasetBindingHandlers(service *datasetcatalog.Service) *DatasetBindingHandlers {
	return &DatasetBindingHandlers{service: service}
}

func (h *DatasetBindingHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.OwnerList(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result.Bindings})
}

func (h *DatasetBindingHandlers) Create(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input datasetcatalog.RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_DATASET_BINDING", "name and canonical_root are required")
		return
	}
	view, err := h.service.OwnerRegister(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": view})
}

func (h *DatasetBindingHandlers) Sources(c *gin.Context) {
	if _, ok := ownerPrincipal(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": datasetcatalog.Catalog()})
}

func (h *DatasetBindingHandlers) Remove(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	view, err := h.service.OwnerRemove(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("bindingID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *DatasetBindingHandlers) writeError(c *gin.Context, err error) {
	var validation *datasetcatalog.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_DATASET_BINDING", validation.Message)
	case errors.Is(err, datasetcatalog.ErrNotFound):
		writeError(c, http.StatusNotFound, "DATASET_BINDING_NOT_FOUND", "dataset binding not found")
	case errors.Is(err, datasetcatalog.ErrProject):
		writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "active Project not found")
	case errors.Is(err, datasetcatalog.ErrConflict):
		writeError(c, http.StatusConflict, "DATASET_BINDING_CONFLICT", "dataset binding name or environment variable is already registered")
	case errors.Is(err, datasetcatalog.ErrLimit):
		writeError(c, http.StatusConflict, "DATASET_BINDING_LIMIT", "dataset binding limit reached")
	default:
		slog.Error("dataset binding API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "DATASET_BINDING_FAILED", "dataset binding operation failed")
	}
}
