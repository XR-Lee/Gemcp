package httpapi

import (
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/gin-gonic/gin"
)

const (
	principalContextKey = "gemcp.principal"
	sessionContextKey   = "gemcp.session"
)

type authenticatedContext struct {
	Principal auth.Principal
	Session   *ent.Session
}

func currentPrincipal(c *gin.Context) (auth.Principal, bool) {
	value, ok := c.Get(principalContextKey)
	if !ok {
		return auth.Principal{}, false
	}
	contextValue, ok := value.(authenticatedContext)
	if !ok {
		return auth.Principal{}, false
	}
	return contextValue.Principal, true
}
