// Package repositories provides data access layer for GoForge.
package repositories

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

// Common errors
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// UserRepository handles user data access.
type UserRepository struct {
	db      *sql.DB
	secrets *secrets.EncryptionService
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *sql.DB, secrets *secrets.EncryptionService) *UserRepository {
	return &UserRepository{db: db, secrets: secrets}
}

// Create creates a new user.
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	if err := r.encrypt(user); err != nil {
		return err
	}

	if user.ID == "" {
		user.ID = uuid.New().String()
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	query := `
		INSERT INTO users (id, email, password_hash, github_id, github_username,
			github_access_token, avatar_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := r.db.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.PasswordHash,
		user.GitHubID,
		user.GitHubUsername,
		user.GitHubAccessToken,
		user.AvatarURL,
		user.CreatedAt,
		user.UpdatedAt,
	)
	return err
}

// GetByID retrieves a user by ID.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, github_id, github_username,
			github_access_token, avatar_url, created_at, updated_at
		FROM users WHERE id = $1`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.GitHubID,
		&user.GitHubUsername,
		&user.GitHubAccessToken,
		&user.AvatarURL,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	r.decrypt(user)
	return user, nil
}

// GetByEmail retrieves a user by email.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, github_id, github_username,
			github_access_token, avatar_url, created_at, updated_at
		FROM users WHERE email = $1`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.GitHubID,
		&user.GitHubUsername,
		&user.GitHubAccessToken,
		&user.AvatarURL,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	r.decrypt(user)
	return user, nil
}

// GetByGitHubID retrieves a user by GitHub ID.
func (r *UserRepository) GetByGitHubID(ctx context.Context, githubID int64) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, github_id, github_username,
			github_access_token, avatar_url, created_at, updated_at
		FROM users WHERE github_id = $1`

	user := &models.User{}
	err := r.db.QueryRowContext(ctx, query, githubID).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.GitHubID,
		&user.GitHubUsername,
		&user.GitHubAccessToken,
		&user.AvatarURL,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	r.decrypt(user)
	return user, nil
}

// Update updates a user.
func (r *UserRepository) Update(ctx context.Context, user *models.User) error {
	if err := r.encrypt(user); err != nil {
		return err
	}

	user.UpdatedAt = time.Now()

	query := `
		UPDATE users SET
			email = $1, password_hash = $2, github_id = $3, github_username = $4,
			github_access_token = $5, avatar_url = $6, updated_at = $7
		WHERE id = $8`

	result, err := r.db.ExecContext(ctx, query,
		user.Email,
		user.PasswordHash,
		user.GitHubID,
		user.GitHubUsername,
		user.GitHubAccessToken,
		user.AvatarURL,
		user.UpdatedAt,
		user.ID,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete deletes a user.
func (r *UserRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// encrypt encrypts sensitive fields before storing.
func (r *UserRepository) encrypt(user *models.User) error {
	if r.secrets == nil {
		return nil
	}

	if user.GitHubAccessToken.Valid && user.GitHubAccessToken.String != "" {
		encrypted, err := r.secrets.Encrypt(user.GitHubAccessToken.String)
		if err != nil {
			return err
		}
		user.GitHubAccessToken.String = encrypted
	}
	return nil
}

// decrypt decrypts sensitive fields after retrieving.
func (r *UserRepository) decrypt(user *models.User) {
	if r.secrets == nil {
		return
	}

	if user.GitHubAccessToken.Valid && user.GitHubAccessToken.String != "" {
		decrypted, err := r.secrets.Decrypt(user.GitHubAccessToken.String)
		if err == nil {
			user.GitHubAccessToken.String = decrypted
		}
		// If decryption fails, assume it's a plaintext token from a previous version.
		// The token will be encrypted on the next update.
	}
}
