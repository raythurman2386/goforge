// Package handlers contains HTTP handlers for the web application.
package handlers

import (
	"net"
	"net/http"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/errors"
)

// trustedProxies holds the parsed trusted proxy CIDRs for IP extraction.
// Initialized via SetTrustedProxies during application startup.
var trustedProxies []*net.IPNet

// SetTrustedProxies configures which proxy CIDRs are trusted for
// X-Forwarded-For / X-Real-IP header extraction. If proxies is nil,
// DefaultTrustedProxies (private/loopback networks) are used.
func SetTrustedProxies(proxies []*net.IPNet) {
	if proxies != nil {
		trustedProxies = proxies
	} else {
		trustedProxies = httputil.DefaultTrustedProxies()
	}
}

// getClientIP extracts the real client IP address from a request,
// only trusting proxy headers when the direct peer is a trusted proxy.
func getClientIP(r *http.Request) string {
	return httputil.ClientIP(
		r.RemoteAddr,
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		trustedProxies,
	)
}

// RenderError renders an error page.
func RenderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	csrfToken := auth.GetCSRFToken(r)
	var component templ.Component
	switch status {
	case http.StatusNotFound:
		component = errors.NotFound(csrfToken)
	case http.StatusForbidden:
		component = errors.Forbidden(csrfToken)
	case http.StatusInternalServerError:
		component = errors.InternalError(csrfToken)
	default:
		component = errors.ErrorPage(status, http.StatusText(status), message, csrfToken)
	}

	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		// Fallback if rendering fails
		http.Error(w, message, status)
	}
}

// NotFoundHandler handles 404 errors.
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	RenderError(w, r, http.StatusNotFound, "Page not found")
}

// MethodNotAllowedHandler handles 405 errors.
func MethodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	RenderError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
}

// Index handles the root path.
func Index(w http.ResponseWriter, r *http.Request) {
	if user := r.Context().Value(auth.UserContextKey); user != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
