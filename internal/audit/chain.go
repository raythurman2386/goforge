package audit

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"
)

const (
	// SealInterval is how often the sealer looks for unsealed rows.
	SealInterval = 15 * time.Second
	// SealMinAge is how long a row sits before it can be sealed. Inserts
	// that arrive out of order within this window still chain in
	// created_at, id order. A row deleted inside the window leaves no seal.
	SealMinAge = 5 * time.Second
	// VerifyUnsealedAge is how old an unsealed row may be before Verify
	// reports it. The sealer's interval is shorter than this.
	VerifyUnsealedAge = 5 * time.Minute
	// SealBatch is how many rows one sealer pass locks and seals.
	SealBatch = 500
	// PurgeBatch is how many audit rows one DELETE statement removes.
	PurgeBatch = 5000
	// PurgeStartupDelay is how long after serve starts before the first purge.
	PurgeStartupDelay = 5 * time.Minute
	// VerifyStartupDelay lets the sealer backfill existing rows before the
	// first daily check logs the chain head.
	VerifyStartupDelay = 30 * time.Second
	// DailyInterval is the period of the purge and the verify check.
	DailyInterval = 24 * time.Hour

	// MinRetentionDays is the shortest retention an admin can turn on.
	// 0 keeps rows forever. The floor matches the purge trigger.
	MinRetentionDays = 30
	// MaxRetentionDays is the longest retention an admin can set (10 years).
	MaxRetentionDays = 3650

	auditSealLockSQL = `SELECT pg_advisory_xact_lock(hashtext('goforge-audit-seal')::bigint)`
	setAuditPurgeSQL = `SELECT set_config('goforge.audit_purge', 'on', true)`
)

// Chain seals audit rows, verifies the chain, and purges old ones.
type Chain struct {
	db     *sql.DB
	key    []byte
	log    *slog.Logger
	status *integrityState
}

// NewChain creates a Chain. key comes from DeriveChainKey. A nil logger
// uses slog.Default.
func NewChain(db *sql.DB, key []byte, log *slog.Logger) *Chain {
	if log == nil {
		log = slog.Default()
	}
	return &Chain{db: db, key: key, log: log, status: &integrityState{}}
}

var errEmptyChainKey = errors.New("audit chain key is empty")

func (c *Chain) logger() *slog.Logger {
	if c.log == nil {
		return slog.Default()
	}
	return c.log
}

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SealOnce seals audit rows older than SealMinAge that have no seal yet,
// in created_at, id order. It returns how many rows it sealed. Concurrent
// sealers and the purge take the same advisory lock.
func (c *Chain) SealOnce(ctx context.Context) (int, error) {
	if len(c.key) == 0 {
		return 0, errEmptyChainKey
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit

	if lockErr := lockAudit(ctx, tx); lockErr != nil {
		return 0, lockErr
	}
	nextSeq, prev, err := c.tip(ctx, tx)
	if err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT a.id::text, `+canonicalExpr+`
		FROM audit_logs a
		WHERE a.created_at < NOW() - INTERVAL '5 seconds'
		  AND NOT EXISTS (SELECT 1 FROM audit_log_seals s WHERE s.audit_id = a.id)
		ORDER BY a.created_at, a.id
		LIMIT $1`, SealBatch)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type pending struct {
		id        string
		canonical string
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.canonical); err != nil {
			return 0, err
		}
		batch = append(batch, p)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	for _, p := range batch {
		sum, herr := rowHash(c.key, prev, []byte(p.canonical))
		if herr != nil {
			return 0, herr
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log_seals (seq, audit_id, prev_hash, row_hash, key_epoch)
			VALUES ($1, $2, $3, $4, $5)`,
			nextSeq, p.id, prev, sum, KeyEpoch); err != nil {
			return 0, err
		}
		prev = sum
		nextSeq++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(batch), nil
}

// tip is the seq to assign next and the prev_hash it must use.
func (c *Chain) tip(ctx context.Context, q queryer) (seq int64, prev []byte, err error) {
	err = q.QueryRowContext(ctx, `
		SELECT seq, row_hash FROM audit_log_seals ORDER BY seq DESC LIMIT 1`).Scan(&seq, &prev)
	if errors.Is(err, sql.ErrNoRows) {
		_, prev, err = c.boundary(ctx, q)
		if err != nil {
			return 0, nil, err
		}
		return 1, prev, nil
	}
	if err != nil {
		return 0, nil, err
	}
	return seq + 1, prev, nil
}

// boundary is the retention prefix removed so far. With no settings row it
// is seq 0 and Genesis.
func (c *Chain) boundary(ctx context.Context, q queryer) (seq int64, hash []byte, err error) {
	err = q.QueryRowContext(ctx, `
		SELECT boundary_seq, boundary_hash FROM audit_settings WHERE id = 1`).Scan(&seq, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, Genesis(), nil
	}
	if err != nil {
		return 0, nil, err
	}
	if len(hash) != ChainKeyLen {
		return 0, nil, errors.New("audit chain boundary hash has the wrong length")
	}
	return seq, hash, nil
}

func lockAudit(ctx context.Context, q queryer) error {
	_, err := q.ExecContext(ctx, auditSealLockSQL)
	return err
}

// Head returns the newest seal, or the retention boundary when nothing is
// sealed. hash is hex. Backup manifests store this as an anchor.
func (c *Chain) Head(ctx context.Context) (seq int64, hash string, err error) {
	var raw []byte
	seq, raw, err = c.head(ctx)
	if err != nil {
		return 0, "", err
	}
	return seq, hexEncode(raw), nil
}

func (c *Chain) head(ctx context.Context) (seq int64, hash []byte, err error) {
	err = c.db.QueryRowContext(ctx, `
		SELECT seq, row_hash FROM audit_log_seals ORDER BY seq DESC LIMIT 1`).Scan(&seq, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return c.boundary(ctx, c.db)
	}
	if err != nil {
		return 0, nil, err
	}
	return seq, hash, nil
}

func hexEncode(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
