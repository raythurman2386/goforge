package auth

import (
	"context"
	"net/http"

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
