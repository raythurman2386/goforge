package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

// ContainerRepository handles container data access.
type ContainerRepository struct {
	db *sql.DB
}

// NewContainerRepository creates a new ContainerRepository.
func NewContainerRepository(db *sql.DB) *ContainerRepository {
	return &ContainerRepository{db: db}
}

// Create creates a new container.
func (r *ContainerRepository) Create(ctx context.Context, c *models.Container) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now

	if c.Ports == nil {
		c.Ports = []byte("[]")
	}
	if c.Labels == nil {
		c.Labels = []byte("{}")
	}

	query := `
		INSERT INTO containers (id, deployment_id, service_instance_id, docker_id, 
			name, image, status, ports, labels, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err := r.db.ExecContext(ctx, query,
		c.ID,
		c.DeploymentID,
		c.ServiceInstanceID,
		c.DockerID,
		c.Name,
		c.Image,
		c.Status,
		c.Ports,
		c.Labels,
		c.CreatedAt,
		c.UpdatedAt,
	)
	return err
}

// GetByID retrieves a container by ID.
func (r *ContainerRepository) GetByID(ctx context.Context, id string) (*models.Container, error) {
	query := `
		SELECT id, deployment_id, service_instance_id, docker_id, name, image, 
			status, ports, labels, created_at, updated_at
		FROM containers WHERE id = $1`

	c := &models.Container{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID,
		&c.DeploymentID,
		&c.ServiceInstanceID,
		&c.DockerID,
		&c.Name,
		&c.Image,
		&c.Status,
		&c.Ports,
		&c.Labels,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return c, err
}

// GetByDockerID retrieves a container by Docker ID.
func (r *ContainerRepository) GetByDockerID(ctx context.Context, dockerID string) (*models.Container, error) {
	query := `
		SELECT id, deployment_id, service_instance_id, docker_id, name, image, 
			status, ports, labels, created_at, updated_at
		FROM containers WHERE docker_id = $1`

	c := &models.Container{}
	err := r.db.QueryRowContext(ctx, query, dockerID).Scan(
		&c.ID,
		&c.DeploymentID,
		&c.ServiceInstanceID,
		&c.DockerID,
		&c.Name,
		&c.Image,
		&c.Status,
		&c.Ports,
		&c.Labels,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return c, err
}

// GetByServiceInstanceID retrieves a container by Service Instance ID.
func (r *ContainerRepository) GetByServiceInstanceID(ctx context.Context, serviceInstanceID string) (*models.Container, error) {
	query := `
		SELECT id, deployment_id, service_instance_id, docker_id, name, image, 
			status, ports, labels, created_at, updated_at
		FROM containers WHERE service_instance_id = $1`

	c := &models.Container{}
	err := r.db.QueryRowContext(ctx, query, serviceInstanceID).Scan(
		&c.ID,
		&c.DeploymentID,
		&c.ServiceInstanceID,
		&c.DockerID,
		&c.Name,
		&c.Image,
		&c.Status,
		&c.Ports,
		&c.Labels,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return c, err
}

// ListByDeploymentID retrieves containers for a deployment.
func (r *ContainerRepository) ListByDeploymentID(ctx context.Context, deploymentID string) ([]*models.Container, error) {
	query := `
		SELECT id, deployment_id, service_instance_id, docker_id, name, image, 
			status, ports, labels, created_at, updated_at
		FROM containers WHERE deployment_id = $1
		ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, deploymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*models.Container
	for rows.Next() {
		c := &models.Container{}
		err := rows.Scan(
			&c.ID,
			&c.DeploymentID,
			&c.ServiceInstanceID,
			&c.DockerID,
			&c.Name,
			&c.Image,
			&c.Status,
			&c.Ports,
			&c.Labels,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return containers, nil
}

// UpdateStatus updates the status of a container.
func (r *ContainerRepository) UpdateStatus(ctx context.Context, id string, status models.ContainerStatus) error {
	query := `UPDATE containers SET status = $1, updated_at = $2 WHERE id = $3`
	result, err := r.db.ExecContext(ctx, query, status, time.Now(), id)
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

// Delete deletes a container.
func (r *ContainerRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM containers WHERE id = $1`
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

// ListAll retrieves all containers.
func (r *ContainerRepository) ListAll(ctx context.Context) ([]*models.Container, error) {
	query := `
		SELECT id, deployment_id, service_instance_id, docker_id, name, image,
			status, ports, labels, created_at, updated_at
		FROM containers
		ORDER BY created_at DESC
		LIMIT 1000`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*models.Container
	for rows.Next() {
		c := &models.Container{}
		err := rows.Scan(
			&c.ID,
			&c.DeploymentID,
			&c.ServiceInstanceID,
			&c.DockerID,
			&c.Name,
			&c.Image,
			&c.Status,
			&c.Ports,
			&c.Labels,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return containers, nil
}

// OwnedByUser reports whether the Docker container belongs to userID.
// Ownership is the service instance owner or the project owner reached
// through deployment → environment → project.
func (r *ContainerRepository) OwnedByUser(ctx context.Context, dockerID, userID string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM containers c
			LEFT JOIN service_instances si ON c.service_instance_id = si.id
			LEFT JOIN deployments d ON c.deployment_id = d.id
			LEFT JOIN environments e ON d.environment_id = e.id
			LEFT JOIN projects p ON e.project_id = p.id
			WHERE c.docker_id = $1 AND (si.user_id = $2 OR p.user_id = $2)
		)`

	var owned bool
	if err := r.db.QueryRowContext(ctx, query, dockerID, userID).Scan(&owned); err != nil {
		return false, err
	}
	return owned, nil
}

// ListByUserID returns containers belonging to a specific user.
// Ownership is determined via the service_instances table (service_instance.user_id)
// or via deployments→environments→projects (project.user_id).
func (r *ContainerRepository) ListByUserID(ctx context.Context, userID string) ([]*models.Container, error) {
	query := `
		SELECT c.id, c.deployment_id, c.service_instance_id, c.docker_id, c.name, c.image,
			c.status, c.ports, c.labels, c.created_at, c.updated_at
		FROM containers c
		LEFT JOIN service_instances si ON c.service_instance_id = si.id
		LEFT JOIN deployments d ON c.deployment_id = d.id
		LEFT JOIN environments e ON d.environment_id = e.id
		LEFT JOIN projects p ON e.project_id = p.id
		WHERE si.user_id = $1 OR p.user_id = $1
		ORDER BY c.created_at DESC
		LIMIT 1000`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*models.Container
	for rows.Next() {
		c := &models.Container{}
		err := rows.Scan(
			&c.ID,
			&c.DeploymentID,
			&c.ServiceInstanceID,
			&c.DockerID,
			&c.Name,
			&c.Image,
			&c.Status,
			&c.Ports,
			&c.Labels,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return containers, nil
}
