package auth

import (
	"net/http"
	"time"
)

const (
	// SessionCookieName is the name of the session cookie.
	SessionCookieName = "goforge_session"
)

// IsSecureRequest returns true if the request was made over HTTPS.
// It checks the X-Forwarded-Proto header (set by reverse proxies like Traefik)
// and falls back to r.TLS. r.URL.Scheme is almost always empty server-side.
func IsSecureRequest(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return r.TLS != nil
}

// SetSessionCookie sets the session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// GetSessionToken retrieves the session token from the request.
func GetSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
