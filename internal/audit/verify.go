package audit

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"
)

const verifyBatch = 1000

type sealRow struct {
	seq       int64
	auditID   string
	prev      []byte
	row       []byte
	epoch     int
	present   bool
	canonical string
}

// Verify walks the hash chain from fromSeq (0 means the oldest seal) and
// reports the first problem. A nil failure and a nil error mean the chain
// is intact. headSeq and headHash are the newest checked seal, or the
// retention boundary when there is nothing left to check.
func (c *Chain) Verify(ctx context.Context, fromSeq int64) (headSeq int64, headHash []byte, failure *ChainError, err error) {
	if len(c.key) == 0 {
		return 0, nil, nil, errEmptyChainKey
	}
	boundarySeq, boundaryHash, err := c.boundary(ctx, c.db)
	if err != nil {
		return 0, nil, nil, err
	}
	minSeq, err := c.minSeq(ctx)
	if err != nil {
		return 0, nil, nil, err
	}

	headSeq, headHash, failure, err = c.walkSeals(ctx, fromSeq, minSeq, boundarySeq, boundaryHash)
	if err != nil || failure != nil {
		return 0, nil, failure, err
	}
	if fail, uerr := c.checkUnsealed(ctx); uerr != nil || fail != nil {
		return 0, nil, fail, uerr
	}
	return headSeq, append([]byte(nil), headHash...), nil, nil
}

// walkSeals checks seals in seq order. headSeq and headHash stay at the
// boundary when there are no seals to check.
func (c *Chain) walkSeals(ctx context.Context, fromSeq, minSeq, boundarySeq int64, boundaryHash []byte) (headSeq int64, headHash []byte, failure *ChainError, err error) {
	headSeq, headHash = boundarySeq, boundaryHash
	var (
		prevSeq  int64
		prevHash []byte
		started  bool
	)
	after := fromSeq - 1
	if fromSeq <= 0 {
		after = 0
	}
	for {
		batch, qerr := c.loadSeals(ctx, after, verifyBatch)
		if qerr != nil {
			return 0, nil, nil, qerr
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			s := &batch[i]
			if !started {
				var serr error
				failure, serr = c.checkStart(ctx, s, fromSeq, minSeq, boundaryHash)
				if serr != nil {
					return 0, nil, nil, serr
				}
				if failure != nil {
					return 0, nil, failure, nil
				}
				prevSeq = s.seq - 1
				started = true
			}
			if s.seq != prevSeq+1 {
				return 0, nil, &ChainError{Seq: prevSeq + 1, Reason: ReasonMissingRow}, nil
			}
			if fail := c.checkSeal(s, prevHash); fail != nil {
				return 0, nil, fail, nil
			}
			prevSeq = s.seq
			prevHash = s.row
			headSeq = s.seq
			headHash = s.row
		}
		after = batch[len(batch)-1].seq
		if len(batch) < verifyBatch {
			break
		}
	}
	if fromSeq > 0 && !started {
		return 0, nil, &ChainError{Seq: fromSeq, Reason: ReasonMissingRow}, nil
	}
	return headSeq, append([]byte(nil), headHash...), nil, nil
}

func (c *Chain) minSeq(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	err := c.db.QueryRowContext(ctx, `SELECT MIN(seq) FROM audit_log_seals`).Scan(&seq)
	if err != nil || !seq.Valid {
		return 0, err
	}
	return seq.Int64, nil
}

func (c *Chain) loadSeals(ctx context.Context, after int64, limit int) ([]sealRow, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT s.seq, s.audit_id::text, s.prev_hash, s.row_hash, s.key_epoch,
			a.id IS NOT NULL,
			CASE WHEN a.id IS NULL THEN '' ELSE `+canonicalExpr+` END
		FROM audit_log_seals s
		LEFT JOIN audit_logs a ON a.id = s.audit_id
		WHERE s.seq > $1
		ORDER BY s.seq
		LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sealRow
	for rows.Next() {
		var s sealRow
		if err := rows.Scan(&s.seq, &s.auditID, &s.prev, &s.row, &s.epoch, &s.present, &s.canonical); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// checkStart checks the boundary when this seal is the oldest one, or the
// link to the seal just before a --from-seq start.
func (c *Chain) checkStart(ctx context.Context, s *sealRow, fromSeq, minSeq int64, boundary []byte) (*ChainError, error) {
	if fromSeq <= 0 || s.seq == minSeq {
		if !bytes.Equal(s.prev, boundary) {
			return &ChainError{Seq: s.seq, Reason: ReasonBoundary}, nil
		}
		return nil, nil
	}
	var prevSeq int64
	var prevHash []byte
	err := c.db.QueryRowContext(ctx, `
		SELECT seq, row_hash FROM audit_log_seals
		WHERE seq < $1 ORDER BY seq DESC LIMIT 1`, s.seq).Scan(&prevSeq, &prevHash)
	if errors.Is(err, sql.ErrNoRows) {
		return &ChainError{Seq: s.seq, Reason: ReasonMissingRow}, nil
	}
	if err != nil {
		return nil, err
	}
	if prevSeq+1 != s.seq {
		return &ChainError{Seq: prevSeq + 1, Reason: ReasonMissingRow}, nil
	}
	if !bytes.Equal(s.prev, prevHash) {
		return &ChainError{Seq: s.seq, Reason: ReasonHashMismatch}, nil
	}
	return nil, nil
}

func (c *Chain) checkSeal(s *sealRow, prevHash []byte) *ChainError {
	if !s.present {
		return &ChainError{Seq: s.seq, Reason: ReasonMissingRow}
	}
	if s.epoch != KeyEpoch {
		return &ChainError{Seq: s.seq, Reason: ReasonHashMismatch}
	}
	if prevHash != nil && !bytes.Equal(s.prev, prevHash) {
		return &ChainError{Seq: s.seq, Reason: ReasonHashMismatch}
	}
	sum, err := rowHash(c.key, s.prev, []byte(s.canonical))
	if err != nil || !bytes.Equal(sum, s.row) {
		return &ChainError{Seq: s.seq, Reason: ReasonHashMismatch}
	}
	return nil
}

func (c *Chain) checkUnsealed(ctx context.Context) (*ChainError, error) {
	var id string
	err := c.db.QueryRowContext(ctx, `
		SELECT a.id::text
		FROM audit_logs a
		WHERE NOT EXISTS (SELECT 1 FROM audit_log_seals s WHERE s.audit_id = a.id)
		  AND a.created_at < NOW() - make_interval(secs => $1)
		ORDER BY a.created_at, a.id
		LIMIT 1`, int(VerifyUnsealedAge/time.Second)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ChainError{Seq: 0, Reason: ReasonUnsealed}, nil
}
