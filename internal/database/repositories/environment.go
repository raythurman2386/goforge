package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/user/gocool/internal/models"
)

// EnvironmentRepository handles environment data access.
type EnvironmentRepository struct {
	db *sql.DB
}

// NewEnvironmentRepository creates a new EnvironmentRepository.
func NewEnvironmentRepository(db *sql.DB) *EnvironmentRepository {
	return &EnvironmentRepository{db: db}
}

// Create creates a new environment.
func (r *EnvironmentRepository) Create(ctx context.Context, env *models.Environment) error {
	if env.ID == "" {
		env.ID = uuid.New().String()
	}
	now := time.Now()
	env.CreatedAt = now
	env.UpdatedAt = now

	query := `
		INSERT INTO environments (id, project_id, name, domain, port, branch, replicas,
			cpu_limit, memory_limit, auto_deploy, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := r.db.ExecContext(ctx, query,
		env.ID,
		env.ProjectID,
		env.Name,
		env.Domain,
		env.Port,
		env.Branch,
		env.Replicas,
		env.CPULimit,
		env.MemoryLimit,
		env.AutoDeploy,
		env.CreatedAt,
		env.UpdatedAt,
	)
	return err
}

// GetByID retrieves an environment by ID.
func (r *EnvironmentRepository) GetByID(ctx context.Context, id string) (*models.Environment, error) {
	query := `
		SELECT id, project_id, name, domain, port, branch, replicas, cpu_limit,
			memory_limit, auto_deploy, created_at, updated_at
		FROM environments WHERE id = $1`

	env := &models.Environment{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&env.ID,
		&env.ProjectID,
		&env.Name,
		&env.Domain,
		&env.Port,
		&env.Branch,
		&env.Replicas,
		&env.CPULimit,
		&env.MemoryLimit,
		&env.AutoDeploy,
		&env.CreatedAt,
		&env.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return env, err
}

// GetByProjectAndName retrieves an environment by project ID and name.
func (r *EnvironmentRepository) GetByProjectAndName(ctx context.Context, projectID, name string) (*models.Environment, error) {
	query := `
		SELECT id, project_id, name, domain, port, branch, replicas, cpu_limit,
			memory_limit, auto_deploy, created_at, updated_at
		FROM environments WHERE project_id = $1 AND name = $2`

	env := &models.Environment{}
	err := r.db.QueryRowContext(ctx, query, projectID, name).Scan(
		&env.ID,
		&env.ProjectID,
		&env.Name,
		&env.Domain,
		&env.Port,
		&env.Branch,
		&env.Replicas,
		&env.CPULimit,
		&env.MemoryLimit,
		&env.AutoDeploy,
		&env.CreatedAt,
		&env.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return env, err
}

// ListByProjectID retrieves all environments for a project.
func (r *EnvironmentRepository) ListByProjectID(ctx context.Context, projectID string) ([]*models.Environment, error) {
	query := `
		SELECT id, project_id, name, domain, port, branch, replicas, cpu_limit,
			memory_limit, auto_deploy, created_at, updated_at
		FROM environments WHERE project_id = $1 ORDER BY name`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var envs []*models.Environment
	for rows.Next() {
		env := &models.Environment{}
		if err := rows.Scan(
			&env.ID,
			&env.ProjectID,
			&env.Name,
			&env.Domain,
			&env.Port,
			&env.Branch,
			&env.Replicas,
			&env.CPULimit,
			&env.MemoryLimit,
			&env.AutoDeploy,
			&env.CreatedAt,
			&env.UpdatedAt,
		); err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	return envs, rows.Err()
}

// Update updates an environment.
func (r *EnvironmentRepository) Update(ctx context.Context, env *models.Environment) error {
	env.UpdatedAt = time.Now()

	query := `
		UPDATE environments SET
			name = $2, domain = $3, port = $4, branch = $5, replicas = $6,
			cpu_limit = $7, memory_limit = $8, auto_deploy = $9, updated_at = $10
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query,
		env.ID,
		env.Name,
		env.Domain,
		env.Port,
		env.Branch,
		env.Replicas,
		env.CPULimit,
		env.MemoryLimit,
		env.AutoDeploy,
		env.UpdatedAt,
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

// Delete deletes an environment.
func (r *EnvironmentRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM environments WHERE id = $1`
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
