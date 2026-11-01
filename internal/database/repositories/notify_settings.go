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

// ErrNotifySettingsUnreadable means the notify_settings URL or token did not
// decrypt with the current ENCRYPTION_KEY.
var ErrNotifySettingsUnreadable = errors.New("notification settings do not decrypt with the current ENCRYPTION_KEY")

// NotifySettingsRepository stores the one notify_settings row. The URL and
// token are sealed with ENCRYPTION_KEY and never stored in plaintext.
type NotifySettingsRepository struct {
	db  *sql.DB
	enc *secrets.Encryptor
}

// NewNotifySettingsRepository creates a NotifySettingsRepository. enc is
// required; without it every write fails rather than storing plaintext.
func NewNotifySettingsRepository(db *sql.DB, enc *secrets.Encryptor) *NotifySettingsRepository {
	return &NotifySettingsRepository{db: db, enc: enc}
}

// Get returns the row, or ErrNotFound when no admin has saved settings yet
// (or migration 025 has not run). When the URL or token do not decrypt, Get
// returns ErrNotifySettingsUnreadable with the row: the non-secret columns
// are filled in and URL and Token are blank.
func (r *NotifySettingsRepository) Get(ctx context.Context) (*models.NotifySettings, error) {
	var s models.NotifySettings
	var urlSealed, tokenSealed string
	err := r.db.QueryRowContext(ctx, `
		SELECT enabled, kind, url_sealed, token_sealed, updated_by, created_at, updated_at
		FROM notify_settings WHERE id = 1`).Scan(
		&s.Enabled, &s.Kind, &urlSealed, &tokenSealed, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	var pqErr *pq.Error
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case errors.As(err, &pqErr) && string(pqErr.Code) == pgUndefinedTable:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("failed to read notification settings: %w", err)
	}
	u, uErr := r.open(urlSealed)
	t, tErr := r.open(tokenSealed)
	if uErr != nil || tErr != nil {
		return &s, ErrNotifySettingsUnreadable
	}
	s.URL, s.Token = u, t
	return &s, nil
}

// Upsert writes the row, replacing every column.
func (r *NotifySettingsRepository) Upsert(ctx context.Context, s *models.NotifySettings) error {
	u, err := r.seal(s.URL)
	if err != nil {
		return err
	}
	t, err := r.seal(s.Token)
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO notify_settings (id, enabled, kind, url_sealed, token_sealed, updated_by, created_at, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled, kind = EXCLUDED.kind,
			url_sealed = EXCLUDED.url_sealed, token_sealed = EXCLUDED.token_sealed,
			updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		s.Enabled, s.Kind, u, t, s.UpdatedBy, now)
	if err != nil {
		return fmt.Errorf("failed to save notification settings: %w", err)
	}
	s.UpdatedAt = now
	return nil
}

func (r *NotifySettingsRepository) seal(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if r.enc == nil {
		return "", errors.New("notification settings need ENCRYPTION_KEY to store the URL")
	}
	return r.enc.Encrypt(v)
}

func (r *NotifySettingsRepository) open(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if r.enc == nil {
		return "", ErrNotifySettingsUnreadable
	}
	p, err := r.enc.Decrypt(v)
	if err != nil {
		return "", ErrNotifySettingsUnreadable
	}
	return p, nil
}
