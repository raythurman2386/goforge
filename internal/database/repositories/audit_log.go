package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/models"
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
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == pgUniqueViolation {
		return fmt.Errorf("%w: %w", ErrAlreadyExists, err)
	}
	return err
}

// pgUniqueViolation is the Postgres error code for a unique index clash.
const pgUniqueViolation = "23505"

const auditColumns = `id, user_id, action, resource_type, resource_id,
			details, ip_address, user_agent, created_at`

func (r *AuditLogRepository) findOne(ctx context.Context, query string, args ...any) (*models.AuditLog, error) {
	l := &models.AuditLog{}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&l.ID, &l.UserID, &l.Action, &l.ResourceType, &l.ResourceID,
		&l.Details, &l.IPAddress, &l.UserAgent, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // nil, nil means no row.
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

// FindApproval returns the first row of userID with this action whose
// details hold approval_ref and idempotency_key, or nil when there is none.
// API v1 uses it so one approval_ref runs a change on a target at most once.
func (r *AuditLogRepository) FindApproval(ctx context.Context, userID, action, approvalRef, idempotencyKey string) (*models.AuditLog, error) {
	return r.findOne(ctx, `
		SELECT `+auditColumns+`
		FROM audit_logs
		WHERE user_id = $1 AND action = $2
			AND details->>'approval_ref' = $3
			AND details->>'idempotency_key' = $4
		ORDER BY created_at
		LIMIT 1`, userID, action, approvalRef, idempotencyKey)
}

// FindOutcome returns the <action>.completed or <action>.failed row whose
// details.audit_id is auditID, or nil when the change has not finished.
func (r *AuditLogRepository) FindOutcome(ctx context.Context, userID, action, auditID string) (*models.AuditLog, error) {
	return r.findOne(ctx, `
		SELECT `+auditColumns+`
		FROM audit_logs
		WHERE user_id = $1 AND action IN ($2, $3)
			AND details->>'audit_id' = $4
		ORDER BY created_at
		LIMIT 1`, userID, action+".completed", action+".failed", auditID)
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

// AuditFilter narrows ListFiltered. Empty fields match everything.
type AuditFilter struct {
	UserID       string
	ResourceType string
	ResourceID   string
	Action       string
	Limit        int
	Offset       int
}

// ListFiltered retrieves audit log entries matching every non-empty filter
// field, newest first.
func (r *AuditLogRepository) ListFiltered(ctx context.Context, f AuditFilter) ([]*models.AuditLog, error) {
	query := `
		SELECT id, user_id, action, resource_type, resource_id,
			details, ip_address, user_agent, created_at
		FROM audit_logs
		WHERE ($1 = '' OR user_id::text = $1)
			AND ($2 = '' OR resource_type = $2)
			AND ($3 = '' OR resource_id::text = $3)
			AND ($4 = '' OR action = $4)
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6`

	rows, err := r.db.QueryContext(ctx, query, f.UserID, f.ResourceType, f.ResourceID, f.Action, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.AuditLog
	for rows.Next() {
		l := &models.AuditLog{}
		if err := rows.Scan(
			&l.ID,
			&l.UserID,
			&l.Action,
			&l.ResourceType,
			&l.ResourceID,
			&l.Details,
			&l.IPAddress,
			&l.UserAgent,
			&l.CreatedAt,
		); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}
