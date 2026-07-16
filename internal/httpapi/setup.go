package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/setup"
	"github.com/gin-gonic/gin"
)

const bootstrapTokenHeader = "X-Gemcp-Bootstrap-Token"

type SetupHandlers struct {
	service            *setup.Service
	bootstrapTokenHash [sha256.Size]byte
	bootstrapEnabled   bool
}

func NewSetupHandlers(service *setup.Service, bootstrapToken string) *SetupHandlers {
	trimmed := strings.TrimSpace(bootstrapToken)
	return &SetupHandlers{
		service:            service,
		bootstrapTokenHash: sha256.Sum256([]byte(trimmed)),
		bootstrapEnabled:   trimmed != "",
	}
}

func (h *SetupHandlers) Status(c *gin.Context) {
	initialized, err := h.service.Initialized(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "SETUP_STATUS_FAILED", "could not read setup state")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"initialized": initialized}})
}

func (h *SetupHandlers) Initialize(c *gin.Context) {
	providedHash := sha256.Sum256([]byte(strings.TrimSpace(c.GetHeader(bootstrapTokenHeader))))
	if !h.bootstrapEnabled || !hmac.Equal(providedHash[:], h.bootstrapTokenHash[:]) {
		writeError(c, http.StatusUnauthorized, "INVALID_BOOTSTRAP_TOKEN", "valid bootstrap token required")
		return
	}
	var input setup.Input
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid setup payload")
		return
	}
	result, err := h.service.Initialize(c.Request.Context(), input)
	if err != nil {
		if errors.Is(err, setup.ErrAlreadyInitialized) {
			writeError(c, http.StatusConflict, "ALREADY_INITIALIZED", "Gemcp is already initialized")
			return
		}
		var validationErr *setup.ValidationError
		if errors.As(err, &validationErr) {
			writeError(c, http.StatusBadRequest, "SETUP_FAILED", validationErr.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, "SETUP_FAILED", "initialization failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}
