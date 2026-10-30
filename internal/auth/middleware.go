package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	// APITokenContextKey is the key for the authenticating API token, when the
	// request was authenticated with a bearer token instead of a session.
	APITokenContextKey contextKey = "api_token"
)

// Errors returned by AuthenticateBearer.
var (
	// ErrNoBearerToken means the request has no usable Authorization: Bearer header.
	ErrNoBearerToken = errors.New("no bearer token")
	// ErrInvalidAPIToken means the bearer token is unknown, expired, or its user is gone.
	ErrInvalidAPIToken = errors.New("invalid or expired API token")
)

// GetAPITokenFromContext returns the API token that authenticated the request,
// or nil for session-authenticated and anonymous requests.
func GetAPITokenFromContext(ctx context.Context) *models.APIToken {
	if token, ok := ctx.Value(APITokenContextKey).(*models.APIToken); ok {
		return token
	}
	return nil
}

// BearerToken returns the raw token from an Authorization: Bearer header.
func BearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
}

// HashAPIToken returns the hex SHA-256 digest stored in api_tokens.token_hash.
func HashAPIToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// APITokenPrefix starts every API token created since migration 021, so
// secret scanners can recognize a leaked GoForge token. Older tokens have no
// prefix and keep working.
const APITokenPrefix = "gf_"

// GenerateAPIToken returns a new raw token (APITokenPrefix plus 32 random
// bytes in hex), its storage hash, and a non-secret hint such as "gf_…a1b2".
// The raw value is shown to the user once and never stored.
func GenerateAPIToken() (raw, hash, hint string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("generate api token: %w", err)
	}
	raw = APITokenPrefix + hex.EncodeToString(buf)
	return raw, HashAPIToken(raw), APITokenHint(raw), nil
}

// APITokenHint is the label stored next to a token: the gf_ prefix when
// present, an ellipsis, and the last four characters. Four hex characters of
// a 256-bit token reveal nothing useful.
func APITokenHint(raw string) string {
	if len(raw) < 12 {
		return ""
	}
	prefix := ""
	if strings.HasPrefix(raw, APITokenPrefix) {
		prefix = APITokenPrefix
	}
	return prefix + "\u2026" + raw[len(raw)-4:]
}

// AuthenticateBearer resolves the request's bearer token to its active token
// row and user. Last-used is updated best-effort.
func AuthenticateBearer(r *http.Request, tokens TokenLookup, users UserLookup) (*models.User, *models.APIToken, error) {
	raw := BearerToken(r)
	if raw == "" {
		return nil, nil, ErrNoBearerToken
	}

	apiToken, err := tokens.GetByTokenHash(r.Context(), HashAPIToken(raw))
	if err != nil || apiToken == nil || !apiToken.IsActive() {
		return nil, nil, ErrInvalidAPIToken
	}

	user, err := users.GetByID(r.Context(), apiToken.UserID)
	if err != nil || user == nil {
		return nil, nil, ErrInvalidAPIToken
	}

	_ = tokens.UpdateLastUsed(r.Context(), apiToken.ID) //nolint:errcheck // best-effort update
	return user, apiToken, nil
}

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
//
// Only tokens with the legacy full scope are accepted here. Tokens limited to
// read, write, or protected are for /api/v1 only, so their scopes cannot be
// bypassed through the HTML and HTMX routes.
func APITokenMiddleware(tokens TokenLookup, users UserLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if already authenticated via session cookie
			if r.Context().Value(UserContextKey) != nil {
				next.ServeHTTP(w, r)
				return
			}

			user, apiToken, err := AuthenticateBearer(r, tokens, users)
			if errors.Is(err, ErrNoBearerToken) {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				http.Error(w, "invalid or expired API token", http.StatusUnauthorized)
				return
			}
			if !apiToken.IsFull() {
				http.Error(w, "scoped API tokens are only valid on /api/v1", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, user)
			ctx = context.WithValue(ctx, APITokenContextKey, apiToken)
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
