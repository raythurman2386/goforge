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

// ErrNotifySettingsUnreadable means a sealed notify_settings column (URL,
// token, or an SMTP field) did not decrypt with the current ENCRYPTION_KEY.
var ErrNotifySettingsUnreadable = errors.New("notification settings do not decrypt with the current ENCRYPTION_KEY")

// NotifySettingsRepository stores the one notify_settings row. The URL,
// token, SMTP host, username, password, From, and To are sealed with ENCRYPTION_KEY and never stored in plaintext.
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
// (or migration 025 has not run). When a sealed column does not decrypt,
// Get returns ErrNotifySettingsUnreadable with the row: the non-secret
// columns are filled in and every sealed field is blank.
func (r *NotifySettingsRepository) Get(ctx context.Context) (*models.NotifySettings, error) {
	var s models.NotifySettings
	var urlSealed, tokenSealed string
	var hostSealed, userSealed, passSealed, fromSealed, toSealed string
	err := r.db.QueryRowContext(ctx, `
		SELECT enabled, kind, url_sealed, token_sealed,
			smtp_host_sealed, smtp_port, smtp_tls, smtp_username_sealed, smtp_password_sealed, smtp_from_sealed, smtp_to_sealed,
			updated_by, created_at, updated_at
		FROM notify_settings WHERE id = 1`).Scan(
		&s.Enabled, &s.Kind, &urlSealed, &tokenSealed,
		&hostSealed, &s.SMTP.Port, &s.SMTP.TLS, &userSealed, &passSealed, &fromSealed, &toSealed,
		&s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	var pqErr *pq.Error
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case errors.As(err, &pqErr) && string(pqErr.Code) == pgUndefinedTable:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("failed to read notification settings: %w", err)
	}
	plain := make([]string, 7)
	for i, v := range []string{urlSealed, tokenSealed, hostSealed, userSealed, passSealed, fromSealed, toSealed} {
		p, oErr := r.open(v)
		if oErr != nil {
			return &s, ErrNotifySettingsUnreadable
		}
		plain[i] = p
	}
	s.URL, s.Token = plain[0], plain[1]
	s.SMTP.Host, s.SMTP.Username, s.SMTP.Password, s.SMTP.From, s.SMTP.To = plain[2], plain[3], plain[4], plain[5], plain[6]
	return &s, nil
}

// Upsert writes the row, replacing every column.
func (r *NotifySettingsRepository) Upsert(ctx context.Context, s *models.NotifySettings) error {
	sealed := make([]string, 7)
	for i, v := range []string{s.URL, s.Token, s.SMTP.Host, s.SMTP.Username, s.SMTP.Password, s.SMTP.From, s.SMTP.To} {
		c, err := r.seal(v)
		if err != nil {
			return err
		}
		sealed[i] = c
	}
	tlsMode := s.SMTP.TLS
	if tlsMode == "" {
		tlsMode = models.SMTPTLSStartTLS
	}
	now := time.Now()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notify_settings (id, enabled, kind, url_sealed, token_sealed,
			smtp_host_sealed, smtp_port, smtp_tls, smtp_username_sealed, smtp_password_sealed, smtp_from_sealed, smtp_to_sealed,
			updated_by, created_at, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled, kind = EXCLUDED.kind,
			url_sealed = EXCLUDED.url_sealed, token_sealed = EXCLUDED.token_sealed,
			smtp_host_sealed = EXCLUDED.smtp_host_sealed, smtp_port = EXCLUDED.smtp_port, smtp_tls = EXCLUDED.smtp_tls,
			smtp_username_sealed = EXCLUDED.smtp_username_sealed, smtp_password_sealed = EXCLUDED.smtp_password_sealed,
			smtp_from_sealed = EXCLUDED.smtp_from_sealed, smtp_to_sealed = EXCLUDED.smtp_to_sealed,
			updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		s.Enabled, s.Kind, sealed[0], sealed[1],
		sealed[2], s.SMTP.Port, tlsMode, sealed[3], sealed[4], sealed[5], sealed[6],
		s.UpdatedBy, now)
	if err != nil {
		return fmt.Errorf("failed to save notification settings: %w", err)
	}
	s.SMTP.TLS = tlsMode
	s.UpdatedAt = now
	return nil
}

func (r *NotifySettingsRepository) seal(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if r.enc == nil {
		return "", errors.New("notification settings need ENCRYPTION_KEY to store notifier secrets")
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
