package apiv1

import (
	"context"
	"errors"
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

// requireBearer authenticates every /api/v1 request with an API token.
// Session cookies are ignored on purpose: this surface is not CSRF-protected,
// so it must never act on ambient browser credentials.
func (h *Handler) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.deps.Tokens == nil || h.deps.Users == nil {
			unavailable(w, "token authentication")
			return
		}
		user, token, err := auth.AuthenticateBearer(r, h.deps.Tokens, h.deps.Users)
		if err != nil {
			msg := "missing bearer token"
			if !errors.Is(err, auth.ErrNoBearerToken) {
				msg = "invalid or expired API token"
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="goforge-api-v1"`)
			writeError(w, http.StatusUnauthorized, codeUnauthorized, msg)
			return
		}
		ctx := context.WithValue(r.Context(), auth.UserContextKey, user)
		ctx = context.WithValue(ctx, auth.APITokenContextKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireScope answers 403 insufficient_scope unless the token grants scope.
func requireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := auth.GetAPITokenFromContext(r.Context())
			if token == nil || !token.HasScope(scope) {
				writeError(w, http.StatusForbidden, codeInsufficientScope, "token lacks the "+scope+" scope")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// caller returns the authenticated user and token. requireBearer guarantees both.
func caller(r *http.Request) (*models.User, *models.APIToken) {
	return auth.GetUserFromContext(r.Context()), auth.GetAPITokenFromContext(r.Context())
}
