package repositories

import (
	"context"
	"database/sql"

	"github.com/raythurman2386/goforge/internal/models"
)

// ProjectServiceInstanceRepository handles project-service instance linking.
type ProjectServiceInstanceRepository struct {
	db *sql.DB
}

// NewProjectServiceInstanceRepository creates a new ProjectServiceInstanceRepository.
func NewProjectServiceInstanceRepository(db *sql.DB) *ProjectServiceInstanceRepository {
	return &ProjectServiceInstanceRepository{db: db}
}

// Link associates a service instance with a project.
func (r *ProjectServiceInstanceRepository) Link(ctx context.Context, projectID, serviceInstanceID string) error {
	query := `
		INSERT INTO project_service_instances (project_id, service_instance_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`

	_, err := r.db.ExecContext(ctx, query, projectID, serviceInstanceID)
	return err
}

// Unlink removes the association between a service instance and a project.
func (r *ProjectServiceInstanceRepository) Unlink(ctx context.Context, projectID, serviceInstanceID string) error {
	query := `DELETE FROM project_service_instances WHERE project_id = $1 AND service_instance_id = $2`

	result, err := r.db.ExecContext(ctx, query, projectID, serviceInstanceID)
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

// ListByProjectID returns all service instances linked to a project.
func (r *ProjectServiceInstanceRepository) ListByProjectID(ctx context.Context, projectID string) ([]*models.ServiceInstance, error) {
	query := `
		SELECT si.id, si.user_id, si.template_name, si.name, si.version, si.config,
			si.domain, si.status, si.connection_info, si.created_at, si.updated_at
		FROM service_instances si
		INNER JOIN project_service_instances psi ON si.id = psi.service_instance_id
		WHERE psi.project_id = $1
		ORDER BY si.name`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*models.ServiceInstance
	for rows.Next() {
		s := &models.ServiceInstance{}
		err := rows.Scan(
			&s.ID,
			&s.UserID,
			&s.TemplateName,
			&s.Name,
			&s.Version,
			&s.Config,
			&s.Domain,
			&s.Status,
			&s.ConnectionInfo,
			&s.CreatedAt,
			&s.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		instances = append(instances, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return instances, nil
}

// ListByServiceInstanceID returns all project IDs linked to a service instance.
func (r *ProjectServiceInstanceRepository) ListByServiceInstanceID(ctx context.Context, serviceInstanceID string) ([]string, error) {
	query := `SELECT project_id FROM project_service_instances WHERE service_instance_id = $1`

	rows, err := r.db.QueryContext(ctx, query, serviceInstanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projectIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		projectIDs = append(projectIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projectIDs, nil
}
