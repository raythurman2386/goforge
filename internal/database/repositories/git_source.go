package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/user/gocool/internal/models"
)

// GitSourceRepository handles git source data access.
type GitSourceRepository struct {
	db *sql.DB
}

// NewGitSourceRepository creates a new GitSourceRepository.
func NewGitSourceRepository(db *sql.DB) *GitSourceRepository {
	return &GitSourceRepository{db: db}
}

// Create creates a new git source.
func (r *GitSourceRepository) Create(ctx context.Context, source *models.GitSource) error {
	if source.ID == "" {
		source.ID = uuid.New().String()
	}
	now := time.Now()
	source.CreatedAt = now
	source.UpdatedAt = now

	query := `
		INSERT INTO git_sources (id, user_id, name, type, api_url, 
			access_token, ssh_private_key, is_default, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := r.db.ExecContext(ctx, query,
		source.ID,
		source.UserID,
		source.Name,
		source.Type,
		source.APIURL,
		source.AccessToken,
		source.SSHPrivateKey,
		source.IsDefault,
		source.CreatedAt,
		source.UpdatedAt,
	)
	return err
}

// GetByID retrieves a git source by ID.
func (r *GitSourceRepository) GetByID(ctx context.Context, id string) (*models.GitSource, error) {
	query := `
		SELECT id, user_id, name, type, api_url, access_token, 
			ssh_private_key, is_default, created_at, updated_at
		FROM git_sources WHERE id = $1`

	source := &models.GitSource{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&source.ID,
		&source.UserID,
		&source.Name,
		&source.Type,
		&source.APIURL,
		&source.AccessToken,
		&source.SSHPrivateKey,
		&source.IsDefault,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return source, err
}

// ListByUserID retrieves all git sources for a user.
func (r *GitSourceRepository) ListByUserID(ctx context.Context, userID string) ([]*models.GitSource, error) {
	query := `
		SELECT id, user_id, name, type, api_url, access_token, 
			ssh_private_key, is_default, created_at, updated_at
		FROM git_sources WHERE user_id = $1
		ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []*models.GitSource
	for rows.Next() {
		source := &models.GitSource{}
		err := rows.Scan(
			&source.ID,
			&source.UserID,
			&source.Name,
			&source.Type,
			&source.APIURL,
			&source.AccessToken,
			&source.SSHPrivateKey,
			&source.IsDefault,
			&source.CreatedAt,
			&source.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

// Update updates a git source.
func (r *GitSourceRepository) Update(ctx context.Context, source *models.GitSource) error {
	source.UpdatedAt = time.Now()

	query := `
		UPDATE git_sources SET 
			name = ?, type = ?, api_url = ?, access_token = ?,
			ssh_private_key = ?, is_default = ?, updated_at = ?
		WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query,
		source.Name,
		source.Type,
		source.APIURL,
		source.AccessToken,
		source.SSHPrivateKey,
		source.IsDefault,
		source.UpdatedAt,
		source.ID,
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

// Delete deletes a git source.
func (r *GitSourceRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM git_sources WHERE id = $1`
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
