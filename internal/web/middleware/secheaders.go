package middleware

import (
	"net/http"
)

// Security header values set by SecurityHeaders.
const (
	// HSTSValue is sent only on HTTPS requests. includeSubDomains is left out
	// on purpose: GoForge routes user apps on sibling subdomains and must not
	// pin HTTPS for hosts it does not control.
	HSTSValue = "max-age=31536000"
	// CSPValue is a framing and base-URI policy only. Templates use inline
	// scripts and HTMX attributes, so script-src/style-src are not restricted
	// here; frame-ancestors carries the clickjacking protection.
	CSPValue            = "frame-ancestors 'none'; base-uri 'self'; object-src 'none'"
	FrameOptionsValue   = "DENY"
	ContentTypeOptions  = "nosniff"
	ReferrerPolicyValue = "strict-origin-when-cross-origin"
)

// SecurityHeaders sets browser hardening headers on every response:
// Content-Security-Policy (frame-ancestors), X-Frame-Options, X-Content-Type-Options,
// Referrer-Policy, and Strict-Transport-Security when the request arrived over
// HTTPS (directly or via X-Forwarded-Proto from the proxy).
//
// Headers are set before the handler runs, so they also land on error pages,
// redirects, static files, and streaming responses. A handler may override them.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", CSPValue)
		h.Set("X-Frame-Options", FrameOptionsValue)
		h.Set("X-Content-Type-Options", ContentTypeOptions)
		h.Set("Referrer-Policy", ReferrerPolicyValue)
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", HSTSValue)
		}
		next.ServeHTTP(w, r)
	})
}

// isHTTPS mirrors auth.IsSecureRequest without importing auth.
func isHTTPS(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return r.TLS != nil
}
