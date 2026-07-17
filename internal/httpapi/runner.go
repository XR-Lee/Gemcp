package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/runner"
	"github.com/gin-gonic/gin"
)

type RunnerHandlers struct {
	service *runner.Service
}

func NewRunnerHandlers(service *runner.Service) *RunnerHandlers {
	return &RunnerHandlers{service: service}
}

func (h *RunnerHandlers) Bootstrap(c *gin.Context) {
	token, ok := h.token(c)
	if !ok {
		return
	}
	if _, err := h.service.Spec(c.Request.Context(), token); err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Content-Type", "text/x-python; charset=utf-8")
	c.Header("X-Content-Type-Options", "nosniff")
	c.String(http.StatusOK, runner.BootstrapScript())
}

func (h *RunnerHandlers) Spec(c *gin.Context) {
	token, ok := h.token(c)
	if !ok {
		return
	}
	result, err := h.service.Spec(c.Request.Context(), token)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RunnerHandlers) Source(c *gin.Context) {
	token, ok := h.token(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	archive, err := h.service.Source(ctx, token)
	if err != nil {
		h.writeError(c, err)
		return
	}
	defer archive.Close()
	file, err := archive.Open()
	if err != nil {
		h.writeError(c, err)
		return
	}
	defer file.Close()
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Length", strconv.FormatInt(archive.SizeBytes(), 10))
	c.Header("Content-Disposition", `attachment; filename="source.tar.gz"`)
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, file); err != nil {
		slog.Warn("Runner source stream interrupted", "error", err)
	}
}

func (h *RunnerHandlers) Event(c *gin.Context) {
	token, ok := h.token(c)
	if !ok {
		return
	}
	var input runner.EventInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_RUNNER_EVENT", "valid Runner event JSON is required")
		return
	}
	result, err := h.service.Event(c.Request.Context(), token, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RunnerHandlers) token(c *gin.Context) (string, bool) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") || strings.Contains(header[7:], ",") {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED_RUNNER", "Runner authentication required")
		return "", false
	}
	token := strings.TrimSpace(header[7:])
	if token == "" {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED_RUNNER", "Runner authentication required")
		return "", false
	}
	return token, true
}

func (h *RunnerHandlers) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, runner.ErrUnauthenticated), errors.Is(err, runner.ErrExpired):
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED_RUNNER", "Runner authentication failed")
	case errors.Is(err, runner.ErrSourceLimit):
		writeError(c, http.StatusTooManyRequests, "RUNNER_SOURCE_LIMIT", "Runner source download limit reached")
	case errors.Is(err, runner.ErrInvalidEvent):
		writeError(c, http.StatusBadRequest, "INVALID_RUNNER_EVENT", "Runner event is invalid")
	case errors.Is(err, runner.ErrTerminal):
		writeError(c, http.StatusGone, "RUNNER_SESSION_FINISHED", "Runner session is no longer active")
	default:
		slog.Error("Runner API failed", "error", err)
		writeError(c, http.StatusInternalServerError, "RUNNER_API_FAILED", "Runner operation failed")
	}
}
