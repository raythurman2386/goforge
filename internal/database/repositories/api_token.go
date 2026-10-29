package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/models"
)

// APITokenRepository handles API token data access.
type APITokenRepository struct {
	db *sql.DB
}

// NewAPITokenRepository creates a new APITokenRepository.
func NewAPITokenRepository(db *sql.DB) *APITokenRepository {
	return &APITokenRepository{db: db}
}

// Create creates a new API token.
func (r *APITokenRepository) Create(ctx context.Context, token *models.APIToken) error {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	if len(token.Scopes) == 0 {
		token.Scopes = []string{models.ScopeFull}
	}

	query := `
		INSERT INTO api_tokens (id, user_id, name, token_hash, expires_at, created_at, scopes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.ExecContext(ctx, query,
		token.ID,
		token.UserID,
		token.Name,
		token.TokenHash,
		token.ExpiresAt,
		token.CreatedAt,
		pq.Array(token.Scopes),
	)
	return err
}

// GetByID retrieves an API token by ID.
func (r *APITokenRepository) GetByID(ctx context.Context, id string) (*models.APIToken, error) {
	query := `
		SELECT id, user_id, name, token_hash, last_used_at, expires_at, created_at, scopes
		FROM api_tokens
		WHERE id = $1
	`

	token := &models.APIToken{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&token.ID,
		&token.UserID,
		&token.Name,
		&token.TokenHash,
		&token.LastUsedAt,
		&token.ExpiresAt,
		&token.CreatedAt,
		pq.Array(&token.Scopes),
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return token, nil
}

// GetByTokenHash retrieves an API token by its hash (for authentication).
func (r *APITokenRepository) GetByTokenHash(ctx context.Context, hash string) (*models.APIToken, error) {
	query := `
		SELECT id, user_id, name, token_hash, last_used_at, expires_at, created_at, scopes
		FROM api_tokens
		WHERE token_hash = $1
	`

	token := &models.APIToken{}
	err := r.db.QueryRowContext(ctx, query, hash).Scan(
		&token.ID,
		&token.UserID,
		&token.Name,
		&token.TokenHash,
		&token.LastUsedAt,
		&token.ExpiresAt,
		&token.CreatedAt,
		pq.Array(&token.Scopes),
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return token, nil
}

// ListByUserID retrieves all API tokens for a user.
func (r *APITokenRepository) ListByUserID(ctx context.Context, userID string) ([]*models.APIToken, error) {
	query := `
		SELECT id, user_id, name, token_hash, last_used_at, expires_at, created_at, scopes
		FROM api_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1000`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*models.APIToken
	for rows.Next() {
		token := &models.APIToken{}
		err := rows.Scan(
			&token.ID,
			&token.UserID,
			&token.Name,
			&token.TokenHash,
			&token.LastUsedAt,
			&token.ExpiresAt,
			&token.CreatedAt,
			pq.Array(&token.Scopes),
		)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// UpdateLastUsed updates the last_used_at timestamp.
func (r *APITokenRepository) UpdateLastUsed(ctx context.Context, id string) error {
	query := `UPDATE api_tokens SET last_used_at = $1 WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

// Delete deletes an API token.
func (r *APITokenRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM api_tokens WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}
