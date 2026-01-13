// Package auth provides authentication and session management.
package auth

import (
	"context"
	"fmt"

	"github.com/user/gocool/internal/database/repositories"
	"github.com/user/gocool/internal/models"
)

// Authenticator defines the interface for authentication services.
type Authenticator interface {
	// Authenticate validates user credentials.
	Authenticate(ctx context.Context, email, password string) (*models.User, error)
}

// LocalAuthenticator implements Authenticator for local accounts.
type LocalAuthenticator struct {
	userRepo *repositories.UserRepository
}

// NewLocalAuthenticator creates a new LocalAuthenticator.
func NewLocalAuthenticator(userRepo *repositories.UserRepository) *LocalAuthenticator {
	return &LocalAuthenticator{userRepo: userRepo}
}

// Authenticate validates local credentials.
func (a *LocalAuthenticator) Authenticate(ctx context.Context, email, password string) (*models.User, error) {
	user, err := a.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if !user.PasswordHash.Valid {
		return nil, fmt.Errorf("user has no local password")
	}

	match, err := ComparePassword(password, user.PasswordHash.String)
	if err != nil || !match {
		return nil, fmt.Errorf("invalid credentials")
	}

	return user, nil
}
