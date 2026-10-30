package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/raythurman2386/goforge/internal/models"
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

	csrfSessionPrefix = "s."
	csrfAnonPrefix    = "a."
)

// CSRF issues and checks CSRF tokens keyed by CSRF_SECRET (HMAC-SHA256).
//
//   - With a valid session, the token is HMAC(secret, SHA-256(session token)).
//     It is a pure function of the session, so it cannot be guessed without
//     the session cookie and the secret, a token from one session fails in
//     another, and a value planted in the goforge_csrf cookie (for example
//     from a sibling subdomain) is ignored.
//   - Without a session (login and register forms), the token is a random
//     nonce plus HMAC(secret, nonce), stored in the goforge_csrf cookie. A
//     planted cookie without a valid MAC is replaced and the request rejected.
//
// State-changing requests (POST, PUT, PATCH, DELETE) must send the token in
// the X-CSRF-Token header or the csrf_token form field.
type CSRF struct {
	key []byte
}

// NewCSRF returns CSRF protection keyed by secret. An empty secret (tests, or
// a caller that skipped config validation) gets a random per-process key, so
// tokens never verify against a known key; config.Validate requires
// CSRF_SECRET outside dev mode.
func NewCSRF(secret string) *CSRF {
	key := []byte(secret)
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic("csrf: crypto/rand failed: " + err.Error())
		}
	}
	return &CSRF{key: key}
}

// Middleware handles CSRF protection.
func (c *CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := c.tokenFor(r)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if cookie, cErr := r.Cookie(CSRFCookieName); cErr != nil || cookie.Value != token {
			http.SetCookie(w, &http.Cookie{
				Name:     CSRFCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: false, // Readable by page JS; HTMX gets it via hx-headers
				Secure:   IsSecureRequest(r),
				SameSite: http.SameSiteLaxMode,
			})
		}

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

		ctx := context.WithValue(r.Context(), csrfContextKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tokenFor returns the token this request must present: the session-bound
// token when auth.Middleware validated a session (it clears invalid session
// cookies, so a stale cookie falls back to the anonymous token), otherwise the anonymous token from
// a valid cookie, or a fresh anonymous token.
func (c *CSRF) tokenFor(r *http.Request) (string, error) {
	if sess, ok := r.Context().Value(SessionContextKey).(*models.Session); ok && sess != nil && sess.TokenHash != "" {
		return c.sessionToken(sess.TokenHash), nil
	}
	if cookie, err := r.Cookie(CSRFCookieName); err == nil && c.validAnon(cookie.Value) {
		return cookie.Value, nil
	}
	nonce, err := generateRandomToken(32)
	if err != nil {
		return "", err
	}
	return csrfAnonPrefix + nonce + "." + c.mac("anon", nonce), nil
}

func (c *CSRF) sessionToken(sessionTokenHash string) string {
	return csrfSessionPrefix + c.mac("session", sessionTokenHash)
}

func (c *CSRF) validAnon(v string) bool {
	rest, ok := strings.CutPrefix(v, csrfAnonPrefix)
	if !ok {
		return false
	}
	nonce, sig, ok := strings.Cut(rest, ".")
	if !ok || nonce == "" || sig == "" {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(c.mac("anon", nonce)))
}

func (c *CSRF) mac(purpose, msg string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte("goforge-csrf-v1\x00" + purpose + "\x00" + msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
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
