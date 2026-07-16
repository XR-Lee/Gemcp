//go:build !webembed

package web

import "net/http"

func Enabled() bool { return false }

func Handler() http.Handler {
	return http.NotFoundHandler()
}
