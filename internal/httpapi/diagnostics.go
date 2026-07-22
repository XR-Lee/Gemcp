package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/diagnostic"
	"github.com/gin-gonic/gin"
)

type DiagnosticHandlers struct {
	service *diagnostic.Service
}

func NewDiagnosticHandlers(service *diagnostic.Service) *DiagnosticHandlers {
	return &DiagnosticHandlers{service: service}
}

func (h *DiagnosticHandlers) Options(c *gin.Context) {
	principal, ok := h.owner(c)
	if !ok {
		return
	}
	result, err := h.service.Options(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *DiagnosticHandlers) Preflight(c *gin.Context) {
	principal, ok := h.owner(c)
	if !ok {
		return
	}
	var input diagnostic.PreflightInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_DIAGNOSTIC", "valid diagnostic preflight JSON is required")
		return
	}
	result, err := h.service.Preflight(c.Request.Context(), principal.TenantID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *DiagnosticHandlers) Submit(c *gin.Context) {
	principal, ok := h.owner(c)
	if !ok {
		return
	}
	var input diagnostic.SubmitInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_DIAGNOSTIC", "valid diagnostic submission JSON is required")
		return
	}
	result, err := h.service.Submit(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	status := http.StatusCreated
	if result.Idempotent {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"data": result})
}

func (h *DiagnosticHandlers) List(c *gin.Context) {
	principal, ok := h.owner(c)
	if !ok {
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
	result, err := h.service.List(c.Request.Context(), principal.TenantID, c.Param("id"), limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *DiagnosticHandlers) Get(c *gin.Context) {
	principal, ok := h.owner(c)
	if !ok {
		return
	}
	result, err := h.service.Get(c.Request.Context(), principal.TenantID, c.Param("id"), c.Param("runID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *DiagnosticHandlers) Cancel(c *gin.Context) {
	principal, ok := h.owner(c)
	if !ok {
		return
	}
	result, err := h.service.Cancel(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("runID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *DiagnosticHandlers) owner(c *gin.Context) (diagnosticPrincipal, bool) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return diagnosticPrincipal{}, false
	}
	if principal.Role != "owner" {
		writeError(c, http.StatusForbidden, "OWNER_REQUIRED", "Owner access is required")
		return diagnosticPrincipal{}, false
	}
	return diagnosticPrincipal{TenantID: principal.TenantID, UserPublicID: principal.UserPublicID}, true
}

type diagnosticPrincipal struct {
	TenantID     int
	UserPublicID string
}

func (h *DiagnosticHandlers) writeError(c *gin.Context, err error) {
	var validation *diagnostic.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_DIAGNOSTIC", validation.Error())
	case errors.Is(err, diagnostic.ErrNotFound):
		writeError(c, http.StatusNotFound, "DIAGNOSTIC_NOT_FOUND", "diagnostic project or run not found")
	case errors.Is(err, diagnostic.ErrConfirmationRequired):
		writeError(c, http.StatusConflict, "DIAGNOSTIC_CONFIRMATION_REQUIRED", "review and explicitly confirm the immutable diagnostic proposal")
	case errors.Is(err, diagnostic.ErrProposalChanged):
		writeError(c, http.StatusConflict, "DIAGNOSTIC_PROPOSAL_CHANGED", "diagnostic configuration changed; run preflight again and review the new proposal")
	case errors.Is(err, diagnostic.ErrPreflightFailed):
		writeError(c, http.StatusConflict, "DIAGNOSTIC_PREFLIGHT_FAILED", "diagnostic preflight must pass before submission")
	case errors.Is(err, diagnostic.ErrIdempotencyConflict):
		writeError(c, http.StatusConflict, "DIAGNOSTIC_IDEMPOTENCY_CONFLICT", "idempotency key was reused with different diagnostic parameters")
	case errors.Is(err, diagnostic.ErrBudgetExceeded), errors.Is(err, diagnostic.ErrExperimentCap):
		writeError(c, http.StatusConflict, "DIAGNOSTIC_BUDGET_REJECTED", err.Error())
	default:
		slog.Error("diagnostic API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "DIAGNOSTIC_FAILED", "diagnostic operation failed")
	}
}
