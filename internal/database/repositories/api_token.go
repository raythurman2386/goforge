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
		INSERT INTO api_tokens (id, user_id, name, token_hash, expires_at, created_at, scopes, token_hint, project_ids, service_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.ExecContext(ctx, query,
		token.ID,
		token.UserID,
		token.Name,
		token.TokenHash,
		token.ExpiresAt,
		token.CreatedAt,
		pq.Array(token.Scopes),
		token.TokenHint,
		pq.Array(idList(token.ProjectIDs)),
		pq.Array(idList(token.ServiceIDs)),
	)
	return err
}

// GetByID retrieves an API token by ID.
func (r *APITokenRepository) GetByID(ctx context.Context, id string) (*models.APIToken, error) {
	query := `
		SELECT id, user_id, name, token_hash, last_used_at, expires_at, created_at, scopes, token_hint, project_ids, service_ids
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
		&token.TokenHint,
		pq.Array(&token.ProjectIDs),
		pq.Array(&token.ServiceIDs),
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
		SELECT id, user_id, name, token_hash, last_used_at, expires_at, created_at, scopes, token_hint, project_ids, service_ids
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
		&token.TokenHint,
		pq.Array(&token.ProjectIDs),
		pq.Array(&token.ServiceIDs),
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
		SELECT id, user_id, name, token_hash, last_used_at, expires_at, created_at, scopes, token_hint, project_ids, service_ids
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
			&token.TokenHint,
			pq.Array(&token.ProjectIDs),
			pq.Array(&token.ServiceIDs),
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

// Rotate replaces the secret of the user's token in place: the row keeps its
// id, name, scopes, and expiry, and the old secret stops matching at once.
// last_used_at is cleared because it described the old secret. It returns
// ErrNotFound when the token does not exist or belongs to another user.
func (r *APITokenRepository) Rotate(ctx context.Context, id, userID, tokenHash, tokenHint string) error {
	query := `UPDATE api_tokens SET token_hash = $1, token_hint = $2, last_used_at = NULL WHERE id = $3 AND user_id = $4`
	res, err := r.db.ExecContext(ctx, query, tokenHash, tokenHint, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete deletes an API token.
func (r *APITokenRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM api_tokens WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// idList returns ids, or an empty non-nil slice: pq.Array(nil) is NULL,
// which the NOT NULL project_ids and service_ids columns refuse.
func idList(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
