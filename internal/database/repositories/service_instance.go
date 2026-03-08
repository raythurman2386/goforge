package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

// ServiceInstanceRepository handles service instance data access.
type ServiceInstanceRepository struct {
	db *sql.DB
}

// NewServiceInstanceRepository creates a new ServiceInstanceRepository.
func NewServiceInstanceRepository(db *sql.DB) *ServiceInstanceRepository {
	return &ServiceInstanceRepository{db: db}
}

// Create creates a new service instance.
func (r *ServiceInstanceRepository) Create(ctx context.Context, s *models.ServiceInstance) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	now := time.Now()
	s.CreatedAt = now
	s.UpdatedAt = now

	if s.Config == nil {
		s.Config = []byte("{}")
	}
	if s.ConnectionInfo == nil {
		s.ConnectionInfo = []byte("{}")
	}

	query := `
		INSERT INTO service_instances (id, user_id, template_name, name, version, 
			config, domain, status, connection_info, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err := r.db.ExecContext(ctx, query,
		s.ID,
		s.UserID,
		s.TemplateName,
		s.Name,
		s.Version,
		s.Config,
		s.Domain,
		s.Status,
		s.ConnectionInfo,
		s.CreatedAt,
		s.UpdatedAt,
	)
	return err
}

// GetByID retrieves a service instance by ID.
func (r *ServiceInstanceRepository) GetByID(ctx context.Context, id string) (*models.ServiceInstance, error) {
	query := `
		SELECT id, user_id, template_name, name, version, config, 
			domain, status, connection_info, created_at, updated_at
		FROM service_instances WHERE id = $1`

	s := &models.ServiceInstance{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
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
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return s, err
}

// ListByUserID retrieves all service instances for a user.
func (r *ServiceInstanceRepository) ListByUserID(ctx context.Context, userID string) ([]*models.ServiceInstance, error) {
	query := `
		SELECT id, user_id, template_name, name, version, config, 
			domain, status, connection_info, created_at, updated_at
		FROM service_instances WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1000`

	rows, err := r.db.QueryContext(ctx, query, userID)
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

// Update updates a service instance.
func (r *ServiceInstanceRepository) Update(ctx context.Context, s *models.ServiceInstance) error {
	s.UpdatedAt = time.Now()

	query := `
		UPDATE service_instances SET 
			name = $1, version = $2, config = $3, domain = $4, 
			status = $5, connection_info = $6, updated_at = $7
		WHERE id = $8`

	result, err := r.db.ExecContext(ctx, query,
		s.Name,
		s.Version,
		s.Config,
		s.Domain,
		s.Status,
		s.ConnectionInfo,
		s.UpdatedAt,
		s.ID,
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

// Delete deletes a service instance.
func (r *ServiceInstanceRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM service_instances WHERE id = $1`
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

// CountByUserID counts the number of service instances for a user.
func (r *ServiceInstanceRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM service_instances WHERE user_id = $1`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	return count, err
}
