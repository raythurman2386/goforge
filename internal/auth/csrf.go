package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
)

const (
	// CSRFCookieName is the name of the CSRF cookie.
	CSRFCookieName = "goforge_csrf"
	// CSRFHeaderName is the name of the CSRF header.
	CSRFHeaderName = "X-CSRF-Token"
	// CSRFFieldName is the name of the CSRF form field.
	CSRFFieldName = "csrf_token"

	// csrfContextKey is the key for the CSRF token in the request context.
	csrfContextKey contextKey = "csrf_token"
)

// CSRFMiddleware handles CSRF protection.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get existing CSRF token from cookie
		cookie, err := r.Cookie(CSRFCookieName)
		var token string

		if err != nil || cookie.Value == "" {
			// Generate new token if not present
			var genErr error
			token, genErr = generateRandomToken(32)
			if genErr != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     CSRFCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: false, // Must be accessible by JS for HTMX
				SameSite: http.SameSiteLaxMode,
			})
		} else {
			token = cookie.Value
		}

		// Validate CSRF token for state-changing requests
		if r.Method == http.MethodPost || r.Method == http.MethodPut ||
			r.Method == http.MethodDelete || r.Method == http.MethodPatch {
			requestToken := r.Header.Get(CSRFHeaderName)
			if requestToken == "" {
				requestToken = r.FormValue(CSRFFieldName)
			}

			if requestToken == "" || subtle.ConstantTimeCompare([]byte(requestToken), []byte(token)) != 1 {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		// Add token to context
		ctx := context.WithValue(r.Context(), csrfContextKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetCSRFToken retrieves the CSRF token from the request context or cookie.
func GetCSRFToken(r *http.Request) string {
	// Check context first
	if val, ok := r.Context().Value(csrfContextKey).(string); ok {
		return val
	}

	cookie, err := r.Cookie(CSRFCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
