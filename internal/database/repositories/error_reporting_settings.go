package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

// ErrErrorReportingSettingsUnreadable means the sealed DSN did not decrypt
// with the current ENCRYPTION_KEY.
var ErrErrorReportingSettingsUnreadable = errors.New("error reporting settings do not decrypt with the current ENCRYPTION_KEY")

// ErrorReportingSettingsRepository stores the one error_reporting_settings
// row. The DSN is sealed with ENCRYPTION_KEY and never stored in plaintext.
type ErrorReportingSettingsRepository struct {
	db  *sql.DB
	enc *secrets.Encryptor
}

// NewErrorReportingSettingsRepository creates an
// ErrorReportingSettingsRepository. enc is required; without it a write
// with a DSN fails rather than storing plaintext.
func NewErrorReportingSettingsRepository(db *sql.DB, enc *secrets.Encryptor) *ErrorReportingSettingsRepository {
	return &ErrorReportingSettingsRepository{db: db, enc: enc}
}

// Get returns the row, or ErrNotFound when no admin has saved settings yet
// (or migration 029 has not run). When the DSN does not decrypt, Get
// returns ErrErrorReportingSettingsUnreadable with the row: Environment and
// the timestamps are filled in and DSN is blank.
func (r *ErrorReportingSettingsRepository) Get(ctx context.Context) (*models.ErrorReportingSettings, error) {
	var s models.ErrorReportingSettings
	var dsnSealed string
	err := r.db.QueryRowContext(ctx, `
		SELECT dsn_sealed, environment, updated_by, created_at, updated_at
		FROM error_reporting_settings WHERE id = 1`).Scan(
		&dsnSealed, &s.Environment, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	var pqErr *pq.Error
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case errors.As(err, &pqErr) && string(pqErr.Code) == pgUndefinedTable:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("failed to read error reporting settings: %w", err)
	}
	if dsnSealed == "" {
		return &s, nil
	}
	if r.enc == nil {
		return &s, ErrErrorReportingSettingsUnreadable
	}
	dsn, err := r.enc.Decrypt(dsnSealed)
	if err != nil {
		return &s, ErrErrorReportingSettingsUnreadable
	}
	s.DSN = dsn
	return &s, nil
}

// Upsert writes the row, replacing every column.
func (r *ErrorReportingSettingsRepository) Upsert(ctx context.Context, s *models.ErrorReportingSettings) error {
	sealed := ""
	if s.DSN != "" {
		if r.enc == nil {
			return errors.New("error reporting settings need ENCRYPTION_KEY to store the DSN")
		}
		c, err := r.enc.Encrypt(s.DSN)
		if err != nil {
			return err
		}
		sealed = c
	}
	now := time.Now()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO error_reporting_settings (id, dsn_sealed, environment, updated_by, created_at, updated_at)
		VALUES (1, $1, $2, $3, $4, $4)
		ON CONFLICT (id) DO UPDATE SET
			dsn_sealed = EXCLUDED.dsn_sealed, environment = EXCLUDED.environment,
			updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		sealed, s.Environment, s.UpdatedBy, now)
	if err != nil {
		return fmt.Errorf("failed to save error reporting settings: %w", err)
	}
	s.UpdatedAt = now
	return nil
}
