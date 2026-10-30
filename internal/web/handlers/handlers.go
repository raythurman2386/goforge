// Package handlers contains HTTP handlers for the web application.
package handlers

import (
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/errors"
)

// proxyTrust decides which proxy headers getClientIP reads.
// Initialized via SetProxyTrust during application startup.
var proxyTrust = httputil.ProxyTrust{Proxies: httputil.DefaultTrustedProxies()}

// htmxTrue is the value of the HX-Request header for HTMX requests.
const htmxTrue = "true"

// SetProxyTrust configures which proxies' X-Forwarded-For / X-Real-IP (and,
// when enabled, CF-Connecting-IP) headers are read. Nil Proxies means
// DefaultTrustedProxies (private/loopback networks).
func SetProxyTrust(trust httputil.ProxyTrust) {
	if trust.Proxies == nil {
		trust.Proxies = httputil.DefaultTrustedProxies()
	}
	proxyTrust = trust
}

// getClientIP extracts the real client IP address from a request,
// only trusting proxy headers when the direct peer is a trusted proxy.
func getClientIP(r *http.Request) string {
	return httputil.ClientIP(r, proxyTrust)
}

// RenderError renders an error page or an HTMX-compatible error fragment.
func RenderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	// For HTMX requests, return a small HTML fragment instead of a full page
	// to avoid injecting <!DOCTYPE html> into swap targets.
	if r.Header.Get("HX-Request") == htmxTrue {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		// templ escapes the message; app.js reads the fragment's text for the toast.
		if err := components.AlertMessage("error", message).Render(r.Context(), w); err != nil {
			fmt.Fprint(w, html.EscapeString(message))
		}
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	// Signed-in users get the app shell and a dashboard link; others get sign in.
	signedIn := auth.GetUserFromContext(r.Context()) != nil
	var component templ.Component
	switch status {
	case http.StatusNotFound:
		component = errors.NotFound(csrfToken, signedIn)
	case http.StatusForbidden:
		component = errors.Forbidden(csrfToken, signedIn)
	case http.StatusInternalServerError:
		component = errors.InternalError(csrfToken, signedIn)
	default:
		component = errors.ErrorPage(status, http.StatusText(status), message, csrfToken, signedIn)
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

// Input validation constants.
const (
	maxDomainLength = 253
	maxInputLength  = 255
	maxPasswordLen  = 256
	maxTokenNameLen = 100
	maxEnvVarKeyLen = 128
)

// domainPattern matches RFC 1123 hostnames (labels separated by dots).
var domainPattern = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// emailPattern is a basic email format check.
var emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// isValidDomain validates a domain string against RFC 1123 rules.
func isValidDomain(domain string) bool {
	domain = strings.TrimSpace(domain)
	if domain == "" || len(domain) > maxDomainLength {
		return false
	}
	return domainPattern.MatchString(domain)
}

// isValidEmail performs a basic email format validation.
func isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > maxInputLength {
		return false
	}
	return emailPattern.MatchString(email)
}
