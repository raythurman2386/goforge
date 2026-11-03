package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/models"
)

type sealedRef struct {
	seq  int64
	id   string
	hash []byte
}

// PurgeOnce hard-deletes audit rows older than days, and their seals.
// days 0 deletes nothing. Other values must be between MinRetentionDays and
// MaxRetentionDays. A prefix of the chain is removed in seq order. A suffix
// of rows that are old but were sealed after a newer row is removed from
// the tail. Rows sandwiched between newer seals are left in place so the
// chain stays contiguous. One audit.retention.purge row is written when
// anything was deleted.
func (c *Chain) PurgeOnce(ctx context.Context, days int) (int64, error) {
	if days == 0 {
		return 0, nil
	}
	if days < MinRetentionDays || days > MaxRetentionDays {
		return 0, fmt.Errorf("audit retention days %d is outside %d-%d", days, MinRetentionDays, MaxRetentionDays)
	}
	var (
		total    int64
		cutoff   time.Time
		moved    bool
		boundary int64
		hash     []byte
	)
	for {
		step, err := c.purgeBatch(ctx, days, PurgeBatch)
		if err != nil {
			return total, err
		}
		if cutoff.IsZero() && !step.cutoff.IsZero() {
			cutoff = step.cutoff
		}
		total += step.n
		if step.moved {
			moved = true
			boundary = step.boundary
			hash = step.hash
		}
		if step.n < int64(PurgeBatch) {
			break
		}
	}
	if total == 0 {
		return 0, nil
	}
	if !moved {
		var err error
		boundary, hash, err = c.boundary(ctx, c.db)
		if err != nil {
			return total, err
		}
	}
	if err := c.insertPurgeRow(ctx, total, cutoff, boundary, hash); err != nil {
		return total, err
	}
	return total, nil
}

// purgeStep is one batched delete. moved is set when a chain prefix was removed.
type purgeStep struct {
	n        int64
	cutoff   time.Time
	boundary int64
	hash     []byte
	moved    bool
}

// purgeBatch deletes up to limit eligible rows inside one transaction and
// advances the stored boundary when it removes a chain prefix.
func (c *Chain) purgeBatch(ctx context.Context, days, limit int) (purgeStep, error) {
	var step purgeStep
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return step, err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit

	if lockErr := lockAudit(ctx, tx); lockErr != nil {
		return step, lockErr
	}
	var flag string
	if scanErr := tx.QueryRowContext(ctx, setAuditPurgeSQL).Scan(&flag); scanErr != nil {
		return step, scanErr
	}
	if scanErr := tx.QueryRowContext(ctx, `SELECT NOW() - ($1::int * INTERVAL '1 day')`, days).Scan(&step.cutoff); scanErr != nil {
		return step, scanErr
	}

	prefix, selErr := c.selectPrefix(ctx, tx, days, limit)
	if selErr != nil {
		return step, selErr
	}
	if delErr := deleteSealed(ctx, tx, prefix); delErr != nil {
		return step, delErr
	}
	step.n = int64(len(prefix))
	if len(prefix) > 0 {
		last := prefix[len(prefix)-1]
		step.boundary, step.hash, step.moved = last.seq, last.hash, true
		if storeErr := c.storeBoundary(ctx, tx, step.boundary, step.hash); storeErr != nil {
			return step, storeErr
		}
	}

	room := limit - len(prefix)
	if room > 0 {
		suffix, serr := c.selectSuffix(ctx, tx, days, room)
		if serr != nil {
			return step, serr
		}
		if delErr := deleteSealed(ctx, tx, suffix); delErr != nil {
			return step, delErr
		}
		step.n += int64(len(suffix))
		room -= len(suffix)
	}
	if room > 0 {
		unsealed, uerr := deleteUnsealed(ctx, tx, days, room)
		if uerr != nil {
			return step, uerr
		}
		step.n += unsealed
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return step, commitErr
	}
	return step, nil
}

func (c *Chain) selectPrefix(ctx context.Context, q queryer, days, limit int) ([]sealedRef, error) {
	return c.selectEligible(ctx, q, days, limit, true)
}

func (c *Chain) selectSuffix(ctx context.Context, q queryer, days, limit int) ([]sealedRef, error) {
	return c.selectEligible(ctx, q, days, limit, false)
}

func (c *Chain) selectEligible(ctx context.Context, q queryer, days, limit int, prefix bool) ([]sealedRef, error) {
	order := "s.seq DESC"
	where := "b.last_keep >= 0 AND s.seq > b.last_keep"
	if prefix {
		order = "s.seq"
		where = "s.seq < b.first_keep"
	}
	// The bounds come from seals whose rows are still inside the retention
	// window. Everything before the first of those is a deletable prefix.
	// Everything after the last is a deletable suffix. The middle stays.
	query := `
		WITH cutoff AS (
			SELECT NOW() - ($1::int * INTERVAL '1 day') AS t
		),
		keepers AS (
			SELECT s.seq
			FROM audit_log_seals s
			JOIN audit_logs a ON a.id = s.audit_id
			CROSS JOIN cutoff c
			WHERE a.created_at >= c.t
		),
		bounds AS (
			SELECT
				COALESCE((SELECT MIN(seq) FROM keepers), 9223372036854775807) AS first_keep,
				COALESCE((SELECT MAX(seq) FROM keepers), -1) AS last_keep
		)
		SELECT s.seq, s.audit_id::text, s.row_hash
		FROM audit_log_seals s
		CROSS JOIN bounds b
		JOIN audit_logs a ON a.id = s.audit_id
		CROSS JOIN cutoff c
		WHERE a.created_at < c.t
		  AND ` + where + `
		ORDER BY ` + order + `
		LIMIT $2`
	rows, err := q.QueryContext(ctx, query, days, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sealedRef
	for rows.Next() {
		var s sealedRef
		if err := rows.Scan(&s.seq, &s.id, &s.hash); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if prefix {
		return out, nil
	}
	// Suffix was selected newest-first. Deleting any tail is safe; the
	// order of the DELETE does not matter.
	return out, nil
}

func deleteSealed(ctx context.Context, q queryer, refs []sealedRef) error {
	if len(refs) == 0 {
		return nil
	}
	ids := make([]string, len(refs))
	for i := range refs {
		ids[i] = refs[i].id
	}
	// Seals first, while the audit row still exists so the trigger can
	// read its created_at. The purge GUC is set on this transaction.
	if _, err := q.ExecContext(ctx, `DELETE FROM audit_log_seals WHERE audit_id = ANY($1::uuid[])`, pq.Array(ids)); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `DELETE FROM audit_logs WHERE id = ANY($1::uuid[])`, pq.Array(ids))
	return err
}

func deleteUnsealed(ctx context.Context, q queryer, days, limit int) (int64, error) {
	res, err := q.ExecContext(ctx, `
		DELETE FROM audit_logs
		WHERE id IN (
			SELECT a.id
			FROM audit_logs a
			WHERE a.created_at < NOW() - ($1::int * INTERVAL '1 day')
			  AND NOT EXISTS (SELECT 1 FROM audit_log_seals s WHERE s.audit_id = a.id)
			ORDER BY a.created_at, a.id
			LIMIT $2
		)`, days, limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (c *Chain) storeBoundary(ctx context.Context, q queryer, seq int64, hash []byte) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO audit_settings (id, boundary_seq, boundary_hash)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET
			boundary_seq = EXCLUDED.boundary_seq,
			boundary_hash = EXCLUDED.boundary_hash
		WHERE EXCLUDED.boundary_seq >= audit_settings.boundary_seq`,
		seq, hash)
	return err
}

type purgeDetails struct {
	Deleted     int64  `json:"deleted"`
	Cutoff      string `json:"cutoff"`
	ThroughSeq  int64  `json:"through_seq"`
	ThroughHash string `json:"through_hash"`
}

func (c *Chain) insertPurgeRow(ctx context.Context, deleted int64, cutoff time.Time, seq int64, hash []byte) error {
	raw, err := json.Marshal(purgeDetails{
		Deleted:     deleted,
		Cutoff:      cutoff.UTC().Format(time.RFC3339),
		ThroughSeq:  seq,
		ThroughHash: hexEncode(hash),
	})
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, action, resource_type, details, created_at)
		VALUES ($1, $2, $3, $4, NOW())`,
		uuid.New().String(), models.AuditActionAuditRetentionPurge, models.ResourceTypeAuditSettings, raw)
	if err != nil {
		return fmt.Errorf("audit retention purge row: %w", err)
	}
	return nil
}
