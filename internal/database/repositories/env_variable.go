package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/user/gocool/internal/models"
)

// EnvVariableRepository handles environment variable data access.
type EnvVariableRepository struct {
	db *sql.DB
}

// NewEnvVariableRepository creates a new EnvVariableRepository.
func NewEnvVariableRepository(db *sql.DB) *EnvVariableRepository {
	return &EnvVariableRepository{db: db}
}

// Create creates a new environment variable.
func (r *EnvVariableRepository) Create(ctx context.Context, v *models.EnvVariable) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	now := time.Now()
	v.CreatedAt = now
	v.UpdatedAt = now

	query := `
		INSERT INTO env_variables (id, environment_id, key, value, is_secret, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		v.ID,
		v.EnvironmentID,
		v.Key,
		v.Value,
		v.IsSecret,
		v.CreatedAt,
		v.UpdatedAt,
	)
	return err
}

// ListByEnvironmentID retrieves all environment variables for an environment.
func (r *EnvVariableRepository) ListByEnvironmentID(ctx context.Context, envID string) ([]*models.EnvVariable, error) {
	query := `
		SELECT id, environment_id, key, value, is_secret, created_at, updated_at
		FROM env_variables WHERE environment_id = ?
		ORDER BY key ASC`

	rows, err := r.db.QueryContext(ctx, query, envID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var variables []*models.EnvVariable
	for rows.Next() {
		v := &models.EnvVariable{}
		err := rows.Scan(
			&v.ID,
			&v.EnvironmentID,
			&v.Key,
			&v.Value,
			&v.IsSecret,
			&v.CreatedAt,
			&v.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		variables = append(variables, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return variables, nil
}

// Update updates an environment variable.
func (r *EnvVariableRepository) Update(ctx context.Context, v *models.EnvVariable) error {
	v.UpdatedAt = time.Now()

	query := `
		UPDATE env_variables SET 
			key = ?, value = ?, is_secret = ?, updated_at = ?
		WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query,
		v.Key,
		v.Value,
		v.IsSecret,
		v.UpdatedAt,
		v.ID,
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

// Delete deletes an environment variable.
func (r *EnvVariableRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM env_variables WHERE id = ?`
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
