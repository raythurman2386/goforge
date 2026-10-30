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

// ErrBackupSettingsUnreadable means the backup_settings keys did not decrypt
// with the current ENCRYPTION_KEY.
var ErrBackupSettingsUnreadable = errors.New("backup settings keys do not decrypt with the current ENCRYPTION_KEY")

// pgUndefinedTable is the Postgres error code for a missing table.
const pgUndefinedTable = "42P01"

// BackupSettingsRepository stores the one backup_settings row. The access
// key ID and secret are sealed with ENCRYPTION_KEY and never stored in
// plaintext.
type BackupSettingsRepository struct {
	db  *sql.DB
	enc *secrets.Encryptor
}

// NewBackupSettingsRepository creates a BackupSettingsRepository. enc is
// required; without it every write fails rather than storing plaintext.
func NewBackupSettingsRepository(db *sql.DB, enc *secrets.Encryptor) *BackupSettingsRepository {
	return &BackupSettingsRepository{db: db, enc: enc}
}

// Get returns the row, or ErrNotFound when no admin has saved settings yet.
// A database that has not run migration 020 also reads as ErrNotFound, so
// `goforge backup run` against an older schema keeps using the environment.
//
// When the keys do not decrypt with the current ENCRYPTION_KEY, Get returns
// ErrBackupSettingsUnreadable together with the row: every non-secret column
// is filled in and both keys are blank, so an admin can see the settings and
// enter the keys again. Callers that run backups must treat the error as
// fatal for the row.
func (r *BackupSettingsRepository) Get(ctx context.Context) (*models.BackupSettings, error) {
	var s models.BackupSettings
	var akSealed, skSealed string
	var timeoutSec int64
	err := r.db.QueryRowContext(ctx, `
		SELECT enabled, endpoint, region, bucket, prefix, access_key_id_sealed, secret_access_key_sealed,
			force_path_style, schedule, retention_daily, timeout_seconds, updated_by, created_at, updated_at
		FROM backup_settings WHERE id = 1`).Scan(
		&s.Enabled, &s.Endpoint, &s.Region, &s.Bucket, &s.Prefix, &akSealed, &skSealed,
		&s.ForcePathStyle, &s.Schedule, &s.RetentionDaily, &timeoutSec, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	var pqErr *pq.Error
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case errors.As(err, &pqErr) && string(pqErr.Code) == pgUndefinedTable:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("failed to read backup settings: %w", err)
	}
	s.Timeout = time.Duration(timeoutSec) * time.Second
	ak, akErr := r.open(akSealed)
	sk, skErr := r.open(skSealed)
	if akErr != nil || skErr != nil {
		return &s, ErrBackupSettingsUnreadable
	}
	s.AccessKeyID, s.SecretAccessKey = ak, sk
	return &s, nil
}

const backupSettingsInsert = `
		INSERT INTO backup_settings (id, enabled, endpoint, region, bucket, prefix,
			access_key_id_sealed, secret_access_key_sealed, force_path_style, schedule,
			retention_daily, timeout_seconds, updated_by, created_at, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)`

const backupSettingsUpsert = backupSettingsInsert + `
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled, endpoint = EXCLUDED.endpoint, region = EXCLUDED.region,
			bucket = EXCLUDED.bucket, prefix = EXCLUDED.prefix,
			access_key_id_sealed = EXCLUDED.access_key_id_sealed,
			secret_access_key_sealed = EXCLUDED.secret_access_key_sealed,
			force_path_style = EXCLUDED.force_path_style, schedule = EXCLUDED.schedule,
			retention_daily = EXCLUDED.retention_daily, timeout_seconds = EXCLUDED.timeout_seconds,
			updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`

const backupSettingsInsertIfAbsent = backupSettingsInsert + ` ON CONFLICT (id) DO NOTHING`

// Upsert writes the row, replacing every column.
func (r *BackupSettingsRepository) Upsert(ctx context.Context, s *models.BackupSettings) error {
	_, err := r.write(ctx, s, backupSettingsUpsert)
	return err
}

// InsertIfAbsent writes the row only when none exists. It reports whether it
// wrote. Two processes starting at once cannot both import.
func (r *BackupSettingsRepository) InsertIfAbsent(ctx context.Context, s *models.BackupSettings) (bool, error) {
	return r.write(ctx, s, backupSettingsInsertIfAbsent)
}

func (r *BackupSettingsRepository) write(ctx context.Context, s *models.BackupSettings, query string) (bool, error) {
	ak, err := r.seal(s.AccessKeyID)
	if err != nil {
		return false, err
	}
	sk, err := r.seal(s.SecretAccessKey)
	if err != nil {
		return false, err
	}
	now := time.Now()
	res, err := r.db.ExecContext(ctx, query,
		s.Enabled, s.Endpoint, s.Region, s.Bucket, s.Prefix, ak, sk, s.ForcePathStyle, s.Schedule,
		s.RetentionDaily, int64(s.Timeout/time.Second), s.UpdatedBy, now)
	if err != nil {
		return false, fmt.Errorf("failed to save backup settings: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	s.UpdatedAt = now
	return n > 0, nil
}

func (r *BackupSettingsRepository) seal(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if r.enc == nil {
		return "", errors.New("backup settings need ENCRYPTION_KEY to store keys")
	}
	return r.enc.Encrypt(v)
}

func (r *BackupSettingsRepository) open(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if r.enc == nil {
		return "", ErrBackupSettingsUnreadable
	}
	p, err := r.enc.Decrypt(v)
	if err != nil {
		return "", ErrBackupSettingsUnreadable
	}
	return p, nil
}
