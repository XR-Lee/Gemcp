package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/gin-gonic/gin"
)

type RepositoryHandlers struct {
	service *gitrepository.Service
}

func NewRepositoryHandlers(service *gitrepository.Service) *RepositoryHandlers {
	return &RepositoryHandlers{service: service}
}

func (h *RepositoryHandlers) Create(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	var input gitrepository.Input
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	view, err := h.service.Create(c.Request.Context(), principal.TenantID, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": view})
}

func (h *RepositoryHandlers) List(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	views, err := h.service.List(c.Request.Context(), principal.TenantID, strings.TrimSpace(c.Query("project_id")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

func (h *RepositoryHandlers) Verify(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	var input struct {
		HostKeyFingerprint string `json:"host_key_fingerprint"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	view, err := h.service.Verify(ctx, principal.TenantID, c.Param("id"), input.HostKeyFingerprint)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *RepositoryHandlers) writeError(c *gin.Context, err error) {
	var validation *gitrepository.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_REPOSITORY", validation.Error())
	case errors.Is(err, gitrepository.ErrNotFound):
		writeError(c, http.StatusNotFound, "REPOSITORY_NOT_FOUND", "repository not found")
	case errors.Is(err, gitrepository.ErrNotActive):
		writeError(c, http.StatusConflict, "REPOSITORY_NOT_ACTIVE", "repository is not active")
	case errors.Is(err, gitrepository.ErrVerificationFailed):
		writeError(c, http.StatusUnprocessableEntity, gitrepository.VerifyFailureCode(err), err.Error())
	case ent.IsConstraintError(err):
		writeError(c, http.StatusConflict, "REPOSITORY_CONFLICT", "repository name already exists in this project")
	default:
		slog.Error("repository operation failed", "error", err)
		writeError(c, http.StatusInternalServerError, "REPOSITORY_OPERATION_FAILED", "repository operation failed")
	}
}
