package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

// backupLockKey is the pg advisory lock key that keeps two backup runs (the
// in-app scheduler and `goforge backup run`) from overlapping. Any constant
// works as long as nothing else in GoForge uses it.
const backupLockKey int64 = 0x60f02eb4c0

// BackupRunRepository stores backup_runs rows.
type BackupRunRepository struct {
	db *sql.DB
}

// NewBackupRunRepository creates a BackupRunRepository.
func NewBackupRunRepository(db *sql.DB) *BackupRunRepository {
	return &BackupRunRepository{db: db}
}

const backupRunColumns = `id, trigger, status, run_key, started_at, finished_at,
	objects, bytes, skipped, pruned, error_summary`

func scanBackupRun(row interface{ Scan(...any) error }) (*models.BackupRun, error) {
	var r models.BackupRun
	if err := row.Scan(&r.ID, &r.Trigger, &r.Status, &r.RunKey, &r.StartedAt, &r.FinishedAt,
		&r.Objects, &r.Bytes, &r.Skipped, &r.Pruned, &r.ErrorSummary); err != nil {
		return nil, err
	}
	return &r, nil
}

// Start inserts a running row.
func (r *BackupRunRepository) Start(ctx context.Context, run *models.BackupRun) error {
	if run.ID == "" {
		run.ID = uuid.New().String()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	run.Status = models.BackupRunRunning
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO backup_runs (id, trigger, status, run_key, started_at) VALUES ($1, $2, $3, $4, $5)`,
		run.ID, run.Trigger, run.Status, run.RunKey, run.StartedAt)
	if err != nil {
		return fmt.Errorf("failed to insert backup run: %w", err)
	}
	return nil
}

// Finish records the outcome of a run.
func (r *BackupRunRepository) Finish(ctx context.Context, run *models.BackupRun) error {
	if !run.FinishedAt.Valid {
		run.FinishedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE backup_runs SET status = $2, finished_at = $3, objects = $4, bytes = $5,
			skipped = $6, pruned = $7, error_summary = $8
		WHERE id = $1`,
		run.ID, run.Status, run.FinishedAt, run.Objects, run.Bytes, run.Skipped, run.Pruned, run.ErrorSummary)
	if err != nil {
		return fmt.Errorf("failed to update backup run: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read updated backup run count: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns the newest runs first.
func (r *BackupRunRepository) List(ctx context.Context, limit int) ([]*models.BackupRun, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+backupRunColumns+` FROM backup_runs ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup runs: %w", err)
	}
	defer rows.Close()
	var out []*models.BackupRun
	for rows.Next() {
		run, scanErr := scanBackupRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// LatestSucceeded returns the newest succeeded run, or ErrNotFound.
func (r *BackupRunRepository) LatestSucceeded(ctx context.Context) (*models.BackupRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+backupRunColumns+` FROM backup_runs WHERE status = 'succeeded' ORDER BY started_at DESC LIMIT 1`)
	run, err := scanBackupRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

// MarkInterrupted fails every row still running. Call it once while holding
// the backup lock: no other run can be in progress then, so a running row is
// left over from a process that stopped mid-run.
func (r *BackupRunRepository) MarkInterrupted(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE backup_runs SET status = 'failed', finished_at = NOW(),
			error_summary = 'interrupted: the GoForge process stopped before the run finished'
		WHERE status = 'running'`)
	if err != nil {
		return 0, fmt.Errorf("failed to mark interrupted backup runs: %w", err)
	}
	return res.RowsAffected()
}

// ErrBackupLocked is returned when another backup run holds the lock.
var ErrBackupLocked = errors.New("another backup run is in progress")

// Lock takes the session-level advisory lock on a dedicated connection. The
// returned unlock releases it. ErrBackupLocked means another run holds it.
func (r *BackupRunRepository) Lock(ctx context.Context) (unlock func(), err error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get a connection for the backup lock: %w", err)
	}
	var ok bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, backupLockKey).Scan(&ok); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to take the backup lock: %w", err)
	}
	if !ok {
		conn.Close()
		return nil, ErrBackupLocked
	}
	// The unlock must run even after ctx is canceled.
	base := context.WithoutCancel(ctx)
	return func() {
		ctx, cancel := context.WithTimeout(base, 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, backupLockKey) //nolint:errcheck // closing the connection releases it anyway
		conn.Close()
	}, nil
}
