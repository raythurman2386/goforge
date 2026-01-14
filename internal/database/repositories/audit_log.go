package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/gocool/internal/models"
)

// AuditLogRepository handles audit log data access.
type AuditLogRepository struct {
	db *sql.DB
}

// NewAuditLogRepository creates a new AuditLogRepository.
func NewAuditLogRepository(db *sql.DB) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

// Create creates a new audit log entry.
func (r *AuditLogRepository) Create(ctx context.Context, l *models.AuditLog) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}

	if l.Details == nil {
		l.Details = []byte("{}")
	}

	query := `
		INSERT INTO audit_logs (id, user_id, action, resource_type, resource_id, 
			details, ip_address, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := r.db.ExecContext(ctx, query,
		l.ID,
		l.UserID,
		l.Action,
		l.ResourceType,
		l.ResourceID,
		l.Details,
		l.IPAddress,
		l.UserAgent,
		l.CreatedAt,
	)
	return err
}

// List retrieves audit log entries with pagination.
func (r *AuditLogRepository) List(ctx context.Context, limit, offset int) ([]*models.AuditLog, error) {
	query := `
		SELECT id, user_id, action, resource_type, resource_id, 
			details, ip_address, user_agent, created_at
		FROM audit_logs
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.AuditLog
	for rows.Next() {
		l := &models.AuditLog{}
		err := rows.Scan(
			&l.ID,
			&l.UserID,
			&l.Action,
			&l.ResourceType,
			&l.ResourceID,
			&l.Details,
			&l.IPAddress,
			&l.UserAgent,
			&l.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

// ListByResource retrieves audit log entries for a specific resource.
func (r *AuditLogRepository) ListByResource(ctx context.Context, resourceType, resourceID string, limit, offset int) ([]*models.AuditLog, error) {
	query := `
		SELECT id, user_id, action, resource_type, resource_id, 
			details, ip_address, user_agent, created_at
		FROM audit_logs
		WHERE resource_type = $1 AND resource_id = $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4`

	rows, err := r.db.QueryContext(ctx, query, resourceType, resourceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.AuditLog
	for rows.Next() {
		l := &models.AuditLog{}
		err := rows.Scan(
			&l.ID,
			&l.UserID,
			&l.Action,
			&l.ResourceType,
			&l.ResourceID,
			&l.Details,
			&l.IPAddress,
			&l.UserAgent,
			&l.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}
