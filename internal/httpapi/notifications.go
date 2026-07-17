package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/notification"
	"github.com/gin-gonic/gin"
)

type NotificationHandlers struct {
	service *notification.Service
}

func NewNotificationHandlers(service *notification.Service) *NotificationHandlers {
	return &NotificationHandlers{service: service}
}

func (h *NotificationHandlers) Setting(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.Setting(c.Request.Context(), principal.TenantID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NotificationHandlers) Configure(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input notification.ConfigureInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_NOTIFICATION_SETTINGS", "valid SMTP settings JSON is required")
		return
	}
	result, err := h.service.Configure(c.Request.Context(), principal.TenantID, principal.UserPublicID, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NotificationHandlers) Test(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.EnqueueTest(c.Request.Context(), principal.TenantID, principal.UserPublicID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"data": result})
}

func (h *NotificationHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_NOTIFICATION_LIMIT", "notification limit must be an integer")
			return
		}
		limit = parsed
	}
	result, err := h.service.List(c.Request.Context(), principal.TenantID, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *NotificationHandlers) writeError(c *gin.Context, err error) {
	var validation *notification.ValidationError
	switch {
	case errors.As(err, &validation), errors.Is(err, notification.ErrInvalidInput):
		writeError(c, http.StatusBadRequest, "INVALID_NOTIFICATION_SETTINGS", err.Error())
	case errors.Is(err, notification.ErrNotConfigured):
		writeError(c, http.StatusConflict, "SMTP_NOT_CONFIGURED", "SMTP notifications are not configured and enabled")
	default:
		slog.Error("notification API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "NOTIFICATION_API_FAILED", "notification operation failed")
	}
}
