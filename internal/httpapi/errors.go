package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func methodNotAllowed(c *gin.Context) {
	writeError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
}
