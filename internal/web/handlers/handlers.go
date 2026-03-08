// Package handlers contains HTTP handlers for the web application.
package handlers

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/errors"
)

// trustedProxies holds the parsed trusted proxy CIDRs for IP extraction.
// Initialized via SetTrustedProxies during application startup.
var trustedProxies []*net.IPNet

// htmxTrue is the value of the HX-Request header for HTMX requests.
const htmxTrue = "true"

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

// RenderError renders an error page or an HTMX-compatible error fragment.
func RenderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	// For HTMX requests, return a small HTML fragment instead of a full page
	// to avoid injecting <!DOCTYPE html> into swap targets.
	if r.Header.Get("HX-Request") == htmxTrue {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		fmt.Fprintf(w, `<div class="rounded-md bg-red-50 p-4"><div class="flex"><div class="flex-shrink-0"><svg class="h-5 w-5 text-red-400" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/></svg></div><div class="ml-3"><p class="text-sm font-medium text-red-800">%s</p></div></div></div>`, message)
		return
	}

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
