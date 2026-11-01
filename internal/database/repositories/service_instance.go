package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

// ServiceInstanceRepository handles service instance data access.
type ServiceInstanceRepository struct {
	db        *sql.DB
	encryptor *secrets.Encryptor
}

// NewServiceInstanceRepository creates a new ServiceInstanceRepository.
func NewServiceInstanceRepository(db *sql.DB) *ServiceInstanceRepository {
	return &ServiceInstanceRepository{db: db}
}

// WithEncryptor seals config and connection_info at rest. The in-memory
// instance keeps plaintext so callers can keep using the returned struct.
func (r *ServiceInstanceRepository) WithEncryptor(enc *secrets.Encryptor) *ServiceInstanceRepository {
	r.encryptor = enc
	return r
}

func (r *ServiceInstanceRepository) sealInstance(s *models.ServiceInstance) (config, connection json.RawMessage, err error) {
	config, err = sealJSON(r.encryptor, s.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt service config: %w", err)
	}
	connection, err = sealJSON(r.encryptor, s.ConnectionInfo)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt connection info: %w", err)
	}
	return config, connection, nil
}

func (r *ServiceInstanceRepository) openInstance(s *models.ServiceInstance) {
	if s == nil {
		return
	}
	s.Config = openJSON(r.encryptor, s.Config)
	s.ConnectionInfo = openJSON(r.encryptor, s.ConnectionInfo)
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

	config, connection, err := r.sealInstance(s)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO service_instances (id, user_id, template_name, name, version, 
			config, domain, status, connection_info, created_at, updated_at, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err = r.db.ExecContext(ctx, query,
		s.ID,
		s.UserID,
		s.TemplateName,
		s.Name,
		s.Version,
		config,
		s.Domain,
		s.Status,
		connection,
		s.CreatedAt,
		s.UpdatedAt,
		s.LastError,
	)
	return err
}

// GetByID retrieves a service instance by ID.
func (r *ServiceInstanceRepository) GetByID(ctx context.Context, id string) (*models.ServiceInstance, error) {
	query := `
		SELECT id, user_id, template_name, name, version, config, 
			domain, status, connection_info, created_at, updated_at, last_error
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
		&s.LastError,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.openInstance(s)
	return s, nil
}

// ListByUserID retrieves all service instances for a user.
func (r *ServiceInstanceRepository) ListByUserID(ctx context.Context, userID string) ([]*models.ServiceInstance, error) {
	query := `
		SELECT id, user_id, template_name, name, version, config, 
			domain, status, connection_info, created_at, updated_at, last_error
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
			&s.LastError,
		)
		if err != nil {
			return nil, err
		}
		r.openInstance(s)
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

	config, connection, err := r.sealInstance(s)
	if err != nil {
		return err
	}

	query := `
		UPDATE service_instances SET 
			name = $1, version = $2, config = $3, domain = $4, 
			status = $5, connection_info = $6, updated_at = $7, last_error = $9
		WHERE id = $8`

	result, err := r.db.ExecContext(ctx, query,
		s.Name,
		s.Version,
		config,
		s.Domain,
		s.Status,
		connection,
		s.UpdatedAt,
		s.ID,
		s.LastError,
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

// UpdateIfStatus writes s like Update, but only while the stored status is
// still expected. The check and the write are one UPDATE, so of two callers
// that both read the same status, exactly one swaps it; the other gets
// false and nothing is written. It also returns false when the row is gone.
func (r *ServiceInstanceRepository) UpdateIfStatus(ctx context.Context, s *models.ServiceInstance, expected models.ServiceInstanceStatus) (bool, error) {
	config, connection, err := r.sealInstance(s)
	if err != nil {
		return false, err
	}
	updatedAt := time.Now()

	query := `
		UPDATE service_instances SET
			name = $1, version = $2, config = $3, domain = $4,
			status = $5, connection_info = $6, updated_at = $7, last_error = $10
		WHERE id = $8 AND status = $9`

	result, err := r.db.ExecContext(ctx, query,
		s.Name,
		s.Version,
		config,
		s.Domain,
		s.Status,
		connection,
		updatedAt,
		s.ID,
		expected,
		s.LastError,
	)
	if err != nil {
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, nil
	}
	s.UpdatedAt = updatedAt
	return true, nil
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

// ListAll returns every service instance across all users with only ID,
// UserID, TemplateName, Name, and Status set. Config and connection info are
// not read, so nothing is decrypted. Used by backups.
func (r *ServiceInstanceRepository) ListAll(ctx context.Context) ([]*models.ServiceInstance, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, template_name, name, status
		FROM service_instances
		ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*models.ServiceInstance
	for rows.Next() {
		s := &models.ServiceInstance{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.TemplateName, &s.Name, &s.Status); err != nil {
			return nil, err
		}
		instances = append(instances, s)
	}
	return instances, rows.Err()
}
