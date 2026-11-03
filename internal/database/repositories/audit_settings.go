package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/models"
)

// AuditSettingsRepository stores the single audit_settings row.
type AuditSettingsRepository struct {
	db *sql.DB
}

// NewAuditSettingsRepository creates an AuditSettingsRepository.
func NewAuditSettingsRepository(db *sql.DB) *AuditSettingsRepository {
	return &AuditSettingsRepository{db: db}
}

const auditSettingsColumns = `retention_days, retention_saved, boundary_seq, boundary_hash, updated_by, created_at, updated_at`

// Get returns the row, or ErrNotFound when none exists yet (or migration
// 031 has not run).
func (r *AuditSettingsRepository) Get(ctx context.Context) (*models.AuditSettings, error) {
	var s models.AuditSettings
	err := r.db.QueryRowContext(ctx, `
		SELECT `+auditSettingsColumns+`
		FROM audit_settings WHERE id = 1`).Scan(
		&s.RetentionDays, &s.RetentionSaved, &s.BoundarySeq, &s.BoundaryHash,
		&s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	return scanAuditSettings(err, &s)
}

func scanAuditSettings(err error, s *models.AuditSettings) (*models.AuditSettings, error) {
	var pqErr *pq.Error
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case errors.As(err, &pqErr) && string(pqErr.Code) == pgUndefinedTable:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("failed to read audit settings: %w", err)
	}
	return s, nil
}

// SaveRetention upserts the admin retention choice. It does not change the
// chain boundary. retention_saved is set so the row wins over the environment.
func (r *AuditSettingsRepository) SaveRetention(ctx context.Context, days int, updatedBy string) error {
	var by any
	if updatedBy != "" {
		by = updatedBy
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO audit_settings (id, retention_days, retention_saved, updated_by, created_at, updated_at)
		VALUES (1, $1, TRUE, $2, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			retention_days = EXCLUDED.retention_days,
			retention_saved = TRUE,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()`,
		days, by)
	if err != nil {
		return fmt.Errorf("failed to save audit settings: %w", err)
	}
	return nil
}
