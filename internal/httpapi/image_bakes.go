package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/imagebake"
	"github.com/gin-gonic/gin"
)

type ImageBakeHandlers struct {
	service *imagebake.Service
}

func NewImageBakeHandlers(service *imagebake.Service) *ImageBakeHandlers {
	return &ImageBakeHandlers{service: service}
}

func (h *ImageBakeHandlers) Options(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	result, err := h.service.OwnerOptions(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ImageBakeHandlers) List(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		if _, err := strconv.Atoi(value); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_LIMIT", "limit must be an integer")
			return
		}
	}
	result, err := h.service.OwnerList(c.Request.Context(), principal.TenantID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ImageBakeHandlers) Create(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input imagebake.RequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IMAGE_BAKE", "valid image bake JSON is required")
		return
	}
	view, err := h.service.OwnerRequest(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": view})
}

func (h *ImageBakeHandlers) Get(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	view, err := h.service.OwnerGet(c.Request.Context(), principal.TenantID, c.Param("id"), c.Param("bakeID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *ImageBakeHandlers) Confirm(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input imagebake.ConfirmInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IMAGE_BAKE", "confirmation_digest is required")
		return
	}
	view, err := h.service.OwnerConfirm(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("bakeID"), input.ConfirmationDigest)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *ImageBakeHandlers) Cancel(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	view, err := h.service.OwnerCancel(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), c.Param("bakeID"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *ImageBakeHandlers) writeError(c *gin.Context, err error) {
	var validation *imagebake.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(c, http.StatusBadRequest, "INVALID_IMAGE_BAKE", validation.Error())
	case errors.Is(err, imagebake.ErrNotFound):
		writeError(c, http.StatusNotFound, "IMAGE_BAKE_NOT_FOUND", "image bake not found")
	case errors.Is(err, imagebake.ErrProject):
		writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "active Project not found")
	case errors.Is(err, imagebake.ErrBusy):
		writeError(c, http.StatusConflict, "IMAGE_BAKE_BUSY", "an image bake is already in progress for this Project")
	case errors.Is(err, imagebake.ErrDigestMismatch):
		writeError(c, http.StatusConflict, "IMAGE_BAKE_DIGEST_MISMATCH", "confirmation digest does not match the requested bake")
	case errors.Is(err, imagebake.ErrNotRequested):
		writeError(c, http.StatusConflict, "IMAGE_BAKE_NOT_REQUESTED", "only a requested image bake can be confirmed")
	case errors.Is(err, imagebake.ErrNotCancellable):
		writeError(c, http.StatusConflict, "IMAGE_BAKE_NOT_CANCELLABLE", "image bake cannot be cancelled in its current state")
	default:
		slog.Error("image bake API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "IMAGE_BAKE_FAILED", "image bake operation failed")
	}
}
