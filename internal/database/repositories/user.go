// Package repositories provides data access layer for GoForge.
package repositories

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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
	secrets *secrets.Encryptor
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *sql.DB, secrets *secrets.Encryptor) *UserRepository {
	return &UserRepository{db: db, secrets: secrets}
}

const userSelectColumns = `id, email, password_hash, github_id, github_username,
			github_access_token, avatar_url, role, created_at, updated_at`

// Create inserts a user. An empty users table yields one admin. Every later
// account is a user. The table lock makes two simultaneous first signups
// produce a single admin.
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

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if lockErr := lockUsers(ctx, tx); lockErr != nil {
		return lockErr
	}

	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&existing); err != nil {
		return err
	}
	user.Role = models.BootstrapRole(existing)

	query := `
		INSERT INTO users (id, email, password_hash, github_id, github_username,
			github_access_token, avatar_url, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	if _, err := tx.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.PasswordHash,
		user.GitHubID,
		user.GitHubUsername,
		user.GitHubAccessToken,
		user.AvatarURL,
		user.Role,
		user.CreatedAt,
		user.UpdatedAt,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// lockUsers serializes account creation on Postgres. SQLite has no LOCK TABLE
// and already serializes writers on a single connection, so a syntax error there
// is ignored.
func lockUsers(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`)
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "syntax") {
		return nil
	}
	return err
}

func scanUser(row interface{ Scan(dest ...any) error }) (*models.User, error) {
	user := &models.User{}
	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.GitHubID,
		&user.GitHubUsername,
		&user.GitHubAccessToken,
		&user.AvatarURL,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// GetByID retrieves a user by ID.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	query := `SELECT ` + userSelectColumns + ` FROM users WHERE id = $1`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, id))
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
	query := `SELECT ` + userSelectColumns + ` FROM users WHERE email = $1`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, email))
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
	query := `SELECT ` + userSelectColumns + ` FROM users WHERE github_id = $1`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, githubID))
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

// Count returns the number of users.
func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
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

// UpdateEmail updates the user's email address.
func (r *UserRepository) UpdateEmail(ctx context.Context, id, email string) error {
	query := `UPDATE users SET email = $1, updated_at = NOW() WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, email, id)
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

// UpdatePassword updates the user's password hash.
func (r *UserRepository) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	query := `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, passwordHash, id)
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
