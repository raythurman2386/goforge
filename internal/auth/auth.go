// Package auth provides authentication and session management.
package auth

import (
	"context"

	"github.com/user/gocool/internal/models"
)

// Authenticator defines the interface for authentication services.
type Authenticator interface {
	// Authenticate validates user credentials.
	Authenticate(ctx context.Context, email, password string) (*models.User, error)
	// GetSession retrieves a user session by token.
	GetSession(ctx context.Context, token string) (*models.Session, *models.User, error)
	// CreateSession creates a new session for a user.
	CreateSession(ctx context.Context, userID string, rememberMe bool) (*models.Session, string, error)
	// DestroySession removes a session.
	DestroySession(ctx context.Context, token string) error
}
