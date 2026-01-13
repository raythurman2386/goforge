package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/user/gocool/internal/database/repositories"
	"github.com/user/gocool/internal/models"
)

var (
	// ErrInvalidToken is returned when a session token is invalid or expired.
	ErrInvalidToken = errors.New("invalid or expired session token")
)

// SessionManager handles session lifecycle.
type SessionManager struct {
	sessionRepo *repositories.SessionRepository
	userRepo    *repositories.UserRepository
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(sessionRepo *repositories.SessionRepository, userRepo *repositories.UserRepository) *SessionManager {
	return &SessionManager{
		sessionRepo: sessionRepo,
		userRepo:    userRepo,
	}
}

// CreateSession creates a new session for a user and returns the token.
func (s *SessionManager) CreateSession(ctx context.Context, userID string, rememberMe bool) (*models.Session, string, error) {
	token, err := generateRandomToken(32)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate token: %w", err)
	}

	tokenHash := hashToken(token)

	duration := 24 * time.Hour
	if rememberMe {
		duration = 30 * 24 * time.Hour
	}

	session := &models.Session{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(duration),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, "", fmt.Errorf("failed to save session: %w", err)
	}

	return session, token, nil
}

// GetSession retrieves a session and the associated user by token.
func (s *SessionManager) GetSession(ctx context.Context, token string) (*models.Session, *models.User, error) {
	tokenHash := hashToken(token)

	session, err := s.sessionRepo.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, nil, ErrInvalidToken
		}
		return nil, nil, err
	}

	if time.Now().After(session.ExpiresAt) {
		if dbErr := s.sessionRepo.Delete(ctx, session.ID); dbErr != nil {
			return nil, nil, dbErr
		}
		return nil, nil, ErrInvalidToken
	}

	user, err := s.userRepo.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, nil, err
	}

	// Update last active time periodically (e.g., if last updated > 1 minute ago)
	if time.Since(session.LastActiveAt) > time.Minute {
		if updateErr := s.sessionRepo.UpdateLastActive(ctx, session.ID); updateErr != nil {
			// Log error but don't fail the request for last active update
			return session, user, updateErr
		}
	}

	return session, user, nil
}

// DestroySession removes a session by token.
func (s *SessionManager) DestroySession(ctx context.Context, token string) error {
	tokenHash := hashToken(token)
	session, err := s.sessionRepo.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil
		}
		return err
	}

	return s.sessionRepo.Delete(ctx, session.ID)
}

// generateRandomToken generates a random base64-encoded token.
func generateRandomToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// hashToken returns a SHA-256 hash of a token.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", hash)
}
