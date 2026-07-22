package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XR-Lee/Gemcp/internal/finance"
	"github.com/gin-gonic/gin"
)

type FinanceHandlers struct {
	service *finance.Service
}

func NewFinanceHandlers(service *finance.Service) *FinanceHandlers {
	return &FinanceHandlers{service: service}
}

func (h *FinanceHandlers) Dashboard(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.Dashboard(
		c.Request.Context(), principal.TenantID, c.Query("period"), c.Query("project_id"),
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *FinanceHandlers) Adjust(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input finance.AdjustmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_BUDGET_ADJUSTMENT", "direction, amount_milli, reason, and idempotency_key are required")
		return
	}
	result, err := h.service.Adjust(
		c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input,
	)
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

func (h *FinanceHandlers) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, finance.ErrProjectNotFound):
		writeError(c, http.StatusNotFound, "FINANCE_PROJECT_NOT_FOUND", "active Project not found")
	case errors.Is(err, finance.ErrInvalidPeriod):
		writeError(c, http.StatusBadRequest, "INVALID_FINANCE_PERIOD", "period must use YYYY-MM")
	case errors.Is(err, finance.ErrInvalidAdjustment):
		writeError(c, http.StatusBadRequest, "INVALID_BUDGET_ADJUSTMENT", "use credit or debit, a positive bounded milli-CNY amount, a reason, and an 8-128 character idempotency key")
	case errors.Is(err, finance.ErrIdempotencyConflict):
		writeError(c, http.StatusConflict, "BUDGET_ADJUSTMENT_IDEMPOTENCY_CONFLICT", "idempotency key was already used for a different budget adjustment")
	default:
		slog.Error("finance API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "FINANCE_OPERATION_FAILED", "finance operation failed")
	}
}
