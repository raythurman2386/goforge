package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/user/gocool/internal/models"
)

// ProjectRepository handles project data access.
type ProjectRepository struct {
	db *sql.DB
}

// NewProjectRepository creates a new ProjectRepository.
func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

// Create creates a new project.
func (r *ProjectRepository) Create(ctx context.Context, project *models.Project) error {
	if project.ID == "" {
		project.ID = uuid.New().String()
	}
	now := time.Now()
	project.CreatedAt = now
	project.UpdatedAt = now

	query := `
		INSERT INTO projects (id, user_id, name, description, git_source_id, repository_url,
			repository_name, default_branch, dockerfile_path, docker_compose_path, build_context,
			build_args, health_check_path, health_check_interval, auto_deploy, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`

	_, err := r.db.ExecContext(ctx, query,
		project.ID,
		project.UserID,
		project.Name,
		project.Description,
		project.GitSourceID,
		project.RepositoryURL,
		project.RepositoryName,
		project.DefaultBranch,
		project.DockerfilePath,
		project.DockerComposePath,
		project.BuildContext,
		project.BuildArgs,
		project.HealthCheckPath,
		project.HealthCheckInterval,
		project.AutoDeploy,
		project.CreatedAt,
		project.UpdatedAt,
	)
	return err
}

// GetByID retrieves a project by ID.
func (r *ProjectRepository) GetByID(ctx context.Context, id string) (*models.Project, error) {
	query := `
		SELECT id, user_id, name, description, git_source_id, repository_url, repository_name,
			default_branch, dockerfile_path, docker_compose_path, build_context, build_args,
			health_check_path, health_check_interval, auto_deploy, created_at, updated_at
		FROM projects WHERE id = $1`

	project := &models.Project{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&project.ID,
		&project.UserID,
		&project.Name,
		&project.Description,
		&project.GitSourceID,
		&project.RepositoryURL,
		&project.RepositoryName,
		&project.DefaultBranch,
		&project.DockerfilePath,
		&project.DockerComposePath,
		&project.BuildContext,
		&project.BuildArgs,
		&project.HealthCheckPath,
		&project.HealthCheckInterval,
		&project.AutoDeploy,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return project, err
}

// ListByUserID retrieves all projects for a user.
func (r *ProjectRepository) ListByUserID(ctx context.Context, userID string) ([]*models.Project, error) {
	query := `
		SELECT id, user_id, name, description, git_source_id, repository_url, repository_name,
			default_branch, dockerfile_path, docker_compose_path, build_context, build_args,
			health_check_path, health_check_interval, auto_deploy, created_at, updated_at
		FROM projects WHERE user_id = $1 ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*models.Project
	for rows.Next() {
		project := &models.Project{}
		if err := rows.Scan(
			&project.ID,
			&project.UserID,
			&project.Name,
			&project.Description,
			&project.GitSourceID,
			&project.RepositoryURL,
			&project.RepositoryName,
			&project.DefaultBranch,
			&project.DockerfilePath,
			&project.DockerComposePath,
			&project.BuildContext,
			&project.BuildArgs,
			&project.HealthCheckPath,
			&project.HealthCheckInterval,
			&project.AutoDeploy,
			&project.CreatedAt,
			&project.UpdatedAt,
		); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

// Update updates a project.
func (r *ProjectRepository) Update(ctx context.Context, project *models.Project) error {
	project.UpdatedAt = time.Now()

	query := `
		UPDATE projects SET
			name = ?, description = ?, git_source_id = ?, repository_url = ?,
			repository_name = ?, default_branch = ?, dockerfile_path = ?,
			docker_compose_path = ?, build_context = ?, build_args = ?,
			health_check_path = ?, health_check_interval = ?, auto_deploy = ?,
			updated_at = ?
		WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query,
		project.Name,
		project.Description,
		project.GitSourceID,
		project.RepositoryURL,
		project.RepositoryName,
		project.DefaultBranch,
		project.DockerfilePath,
		project.DockerComposePath,
		project.BuildContext,
		project.BuildArgs,
		project.HealthCheckPath,
		project.HealthCheckInterval,
		project.AutoDeploy,
		project.UpdatedAt,
		project.ID,
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

// Delete deletes a project.
func (r *ProjectRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM projects WHERE id = $1`
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
