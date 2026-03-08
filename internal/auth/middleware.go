package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/raythurman2386/goforge/internal/models"
)

type contextKey string

const (
	// UserContextKey is the key for the user in the request context.
	UserContextKey contextKey = "user"
	// SessionContextKey is the key for the session in the request context.
	SessionContextKey contextKey = "session"
)

// GetUserFromContext retrieves the user from the context.
func GetUserFromContext(ctx context.Context) *models.User {
	if user, ok := ctx.Value(UserContextKey).(*models.User); ok {
		return user
	}
	return nil
}

// Middleware handles authentication by checking the session cookie.
func Middleware(manager *SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := GetSessionToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			session, user, err := manager.GetSession(r.Context(), token)
			if err != nil {
				// Invalid or expired token
				ClearSessionCookie(w)
				next.ServeHTTP(w, r)
				return
			}

			// Add user and session to context
			ctx := context.WithValue(r.Context(), UserContextKey, user)
			ctx = context.WithValue(ctx, SessionContextKey, session)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TokenLookup provides the ability to look up an API token by its hash.
type TokenLookup interface {
	GetByTokenHash(ctx context.Context, hash string) (*models.APIToken, error)
	UpdateLastUsed(ctx context.Context, id string) error
}

// UserLookup provides the ability to look up a user by ID.
type UserLookup interface {
	GetByID(ctx context.Context, id string) (*models.User, error)
}

// APITokenMiddleware authenticates requests that carry an Authorization: Bearer
// header with an API token. If the request already has a user in context (from
// session cookie auth) or has no Bearer token, it passes through unchanged.
func APITokenMiddleware(tokens TokenLookup, users UserLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if already authenticated via session cookie
			if r.Context().Value(UserContextKey) != nil {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			rawToken := strings.TrimPrefix(authHeader, "Bearer ")
			if rawToken == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Hash the token and look it up
			hash := sha256.Sum256([]byte(rawToken))
			tokenHash := hex.EncodeToString(hash[:])

			apiToken, err := tokens.GetByTokenHash(r.Context(), tokenHash)
			if err != nil || !apiToken.IsActive() {
				http.Error(w, "invalid or expired API token", http.StatusUnauthorized)
				return
			}

			user, err := users.GetByID(r.Context(), apiToken.UserID)
			if err != nil {
				http.Error(w, "invalid API token", http.StatusUnauthorized)
				return
			}

			// Update last used timestamp (best-effort, don't block on error)
			_ = tokens.UpdateLastUsed(r.Context(), apiToken.ID) //nolint:errcheck // best-effort update

			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth is a middleware that requires the user to be authenticated.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value(UserContextKey)
		if user == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
