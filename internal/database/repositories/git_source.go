package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

// GitSourceRepository handles git source data access.
type GitSourceRepository struct {
	db *sql.DB
	// encryption service
	secrets *secrets.Encryptor
}

// NewGitSourceRepository creates a new GitSourceRepository.
func NewGitSourceRepository(db *sql.DB, secrets *secrets.Encryptor) *GitSourceRepository {
	return &GitSourceRepository{db: db, secrets: secrets}
}

// Create creates a new git source.
func (r *GitSourceRepository) Create(ctx context.Context, source *models.GitSource) error {
	if err := r.encrypt(source); err != nil {
		return err
	}

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
	if err != nil {
		return nil, err
	}

	r.decrypt(source)
	return source, nil
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
		r.decrypt(source)
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

// Update updates a git source.
func (r *GitSourceRepository) Update(ctx context.Context, source *models.GitSource) error {
	if err := r.encrypt(source); err != nil {
		return err
	}
	source.UpdatedAt = time.Now()

	query := `
		UPDATE git_sources SET 
			name = $1, type = $2, api_url = $3, access_token = $4,
			ssh_private_key = $5, is_default = $6, updated_at = $7
		WHERE id = $8`

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

func (r *GitSourceRepository) encrypt(source *models.GitSource) error {
	if source.AccessToken.Valid && source.AccessToken.String != "" {
		encrypted, err := r.secrets.Encrypt(source.AccessToken.String)
		if err != nil {
			return err
		}
		source.AccessToken.String = encrypted
	}

	if source.SSHPrivateKey.Valid && source.SSHPrivateKey.String != "" {
		encrypted, err := r.secrets.Encrypt(source.SSHPrivateKey.String)
		if err != nil {
			return err
		}
		source.SSHPrivateKey.String = encrypted
	}
	return nil
}

func (r *GitSourceRepository) decrypt(source *models.GitSource) {
	if source.AccessToken.Valid && source.AccessToken.String != "" {
		decrypted, err := r.secrets.Decrypt(source.AccessToken.String)
		if err == nil {
			source.AccessToken.String = decrypted
		}
		// If decryption fails, assume it's a plaintext token from a previous version.
		// The token will be encrypted on the next update.
	}

	if source.SSHPrivateKey.Valid && source.SSHPrivateKey.String != "" {
		decrypted, err := r.secrets.Decrypt(source.SSHPrivateKey.String)
		if err == nil {
			source.SSHPrivateKey.String = decrypted
		}
		// If decryption fails, assume it's a plaintext key from a previous version.
		// The key will be encrypted on the next update.
	}
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
