package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "gemcp_session"
	csrfCookieName    = "gemcp_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

type AuthHandlers struct {
	service       *auth.Service
	secureCookies bool
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password"`
}

func NewAuthHandlers(service *auth.Service, secureCookies bool) *AuthHandlers {
	return &AuthHandlers{service: service, secureCookies: secureCookies}
}

func (h *AuthHandlers) Login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "email is required")
		return
	}
	if !h.service.SkipPassword() && strings.TrimSpace(request.Password) == "" {
		writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "email and password are required")
		return
	}
	credentials, err := h.service.Login(c.Request.Context(), request.Email, request.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
			return
		}
		writeError(c, http.StatusInternalServerError, "LOGIN_FAILED", "login failed")
		return
	}
	h.setCookies(c, credentials)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"user":       credentials.User,
		"csrf_token": credentials.CSRFToken,
		"expires_at": credentials.ExpiresAt,
	}})
}

func (h *AuthHandlers) Me(c *gin.Context) {
	value, ok := c.Get(principalContextKey)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	contextValue := value.(authenticatedContext)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"user": contextValue.Principal}})
}

func (h *AuthHandlers) Logout(c *gin.Context) {
	value, ok := c.Get(principalContextKey)
	if ok {
		contextValue := value.(authenticatedContext)
		if err := h.service.Logout(c.Request.Context(), contextValue.Session); err != nil {
			writeError(c, http.StatusInternalServerError, "LOGOUT_FAILED", "logout failed")
			return
		}
	}
	h.clearCookies(c)
	c.Status(http.StatusNoContent)
}

func (h *AuthHandlers) RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(token) == "" {
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		principal, record, err := h.service.Authenticate(c.Request.Context(), token)
		if err != nil {
			h.clearCookies(c)
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		if requiresCSRF(c.Request.Method) {
			if err := h.service.ValidateCSRF(record, c.GetHeader(csrfHeaderName)); err != nil {
				writeError(c, http.StatusForbidden, "INVALID_CSRF", "CSRF validation failed")
				return
			}
		}
		c.Set(principalContextKey, authenticatedContext{Principal: principal, Session: record})
		c.Next()
	}
}

func (h *AuthHandlers) setCookies(c *gin.Context, credentials auth.SessionCredentials) {
	maxAge := int(time.Until(credentials.ExpiresAt).Seconds())
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookieName, credentials.SessionToken, maxAge, "/", "", h.secureCookies, true)
	c.SetCookie(csrfCookieName, credentials.CSRFToken, maxAge, "/", "", h.secureCookies, false)
}

func (h *AuthHandlers) clearCookies(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", h.secureCookies, true)
	c.SetCookie(csrfCookieName, "", -1, "/", "", h.secureCookies, false)
}

func requiresCSRF(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}
