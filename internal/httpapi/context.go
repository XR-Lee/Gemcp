package httpapi

import (
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/auth"
)

const (
	principalContextKey = "gemcp.principal"
	sessionContextKey   = "gemcp.session"
)

type authenticatedContext struct {
	Principal auth.Principal
	Session   *ent.Session
}
