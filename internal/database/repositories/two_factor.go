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
	"github.com/raythurman2386/goforge/internal/secrets"
)

// ErrTwoFactorUnreadable means a stored TOTP secret did not decrypt with the
// current ENCRYPTION_KEY. The account cannot pass the second step until an
// operator runs `goforge user reset-2fa`.
var ErrTwoFactorUnreadable = errors.New("two-factor secret does not decrypt with the current ENCRYPTION_KEY")

// TwoFactorRepository stores TOTP enrollments (user_totp), hashed recovery
// codes (user_recovery_codes), and pending sign-in challenges
// (login_challenges), all from migration 030. The TOTP secret is sealed with
// ENCRYPTION_KEY; without an encryptor every write that carries a secret
// fails rather than storing plaintext.
type TwoFactorRepository struct {
	db  *sql.DB
	enc *secrets.Encryptor
}

// NewTwoFactorRepository creates a TwoFactorRepository.
func NewTwoFactorRepository(db *sql.DB, enc *secrets.Encryptor) *TwoFactorRepository {
	return &TwoFactorRepository{db: db, enc: enc}
}

// GetTOTP returns the account's enrollment, or ErrNotFound when it has none
// (or migration 030 has not run). A secret that does not decrypt returns the
// row with a blank Secret and ErrTwoFactorUnreadable.
func (r *TwoFactorRepository) GetTOTP(ctx context.Context, userID string) (*models.UserTOTP, error) {
	t := &models.UserTOTP{}
	var sealed string
	err := r.db.QueryRowContext(ctx, `
		SELECT user_id, secret_sealed, enabled_at, last_used_step, created_at, updated_at
		FROM user_totp WHERE user_id = $1`, userID).Scan(
		&t.UserID, &sealed, &t.EnabledAt, &t.LastUsedStep, &t.CreatedAt, &t.UpdatedAt)
	var pqErr *pq.Error
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case errors.As(err, &pqErr) && string(pqErr.Code) == pgUndefinedTable:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("failed to read two-factor enrollment: %w", err)
	}
	if r.enc == nil {
		return t, ErrTwoFactorUnreadable
	}
	secret, err := r.enc.Decrypt(sealed)
	if err != nil {
		return t, ErrTwoFactorUnreadable
	}
	t.Secret = secret
	return t, nil
}

// SavePendingTOTP stores a new secret for an enrollment that is not
// confirmed yet, replacing any earlier pending secret. It returns
// ErrAlreadyExists when the account already has 2FA on.
func (r *TwoFactorRepository) SavePendingTOTP(ctx context.Context, userID, secret string) error {
	if r.enc == nil {
		return errors.New("two-factor enrollment needs ENCRYPTION_KEY to store the secret")
	}
	sealed, err := r.enc.Encrypt(secret)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO user_totp (user_id, secret_sealed, enabled_at, last_used_step, created_at, updated_at)
		VALUES ($1, $2, NULL, 0, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE
			SET secret_sealed = EXCLUDED.secret_sealed, last_used_step = 0, updated_at = NOW()
			WHERE user_totp.enabled_at IS NULL`, userID, sealed)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAlreadyExists
	}
	return nil
}

// EnableTOTP confirms a pending enrollment: it sets enabled_at, records step
// as the last used TOTP step, and replaces the account's recovery codes with
// codeHashes, in one transaction. It returns ErrNotFound when there is no
// pending enrollment (none started, or already enabled).
func (r *TwoFactorRepository) EnableTOTP(ctx context.Context, userID string, step int64, codeHashes []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit

	res, err := tx.ExecContext(ctx, `
		UPDATE user_totp SET enabled_at = NOW(), last_used_step = $2, updated_at = NOW()
		WHERE user_id = $1 AND enabled_at IS NULL`, userID, step)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, h := range codeHashes {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO user_recovery_codes (id, user_id, code_hash, created_at)
			VALUES ($1, $2, $3, NOW())`, uuid.New().String(), userID, h); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UseTOTPStep records step as used for an enabled enrollment. It returns
// false when step is not newer than the last accepted one (a replayed or
// older code) or when 2FA is not on, so each code works once.
func (r *TwoFactorRepository) UseTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE user_totp SET last_used_step = $2, updated_at = NOW()
		WHERE user_id = $1 AND enabled_at IS NOT NULL AND last_used_step < $2`, userID, step)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// UseRecoveryCode marks the unused recovery code with this hash as used. It
// returns false when no unused code matches, so each code works once.
func (r *TwoFactorRepository) UseRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE user_recovery_codes SET used_at = NOW()
		WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`, userID, codeHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// CountUnusedRecoveryCodes returns how many recovery codes the account has left.
func (r *TwoFactorRepository) CountUnusedRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

// DeleteTOTP removes the account's enrollment, recovery codes, and pending
// sign-in challenges in one transaction. It reports whether an enrollment
// (enabled or pending) existed.
func (r *TwoFactorRepository) DeleteTOTP(ctx context.Context, userID string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit
	res, err := tx.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id = $1`, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_challenges WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	return n > 0, tx.Commit()
}

// CreateChallenge stores a pending sign-in challenge. Expired challenges of
// any account are deleted first, so the table stays small without a timer.
func (r *TwoFactorRepository) CreateChallenge(ctx context.Context, c *models.LoginChallenge) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM login_challenges WHERE expires_at <= $1`, c.CreatedAt); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO login_challenges (id, token_hash, user_id, remember_me, attempts, expires_at, created_at)
		VALUES ($1, $2, $3, $4, 0, $5, $6)`,
		c.ID, c.TokenHash, c.UserID, c.RememberMe, c.ExpiresAt, c.CreatedAt)
	return err
}

// GetChallengeByTokenHash returns the challenge for a token hash, or
// ErrNotFound.
func (r *TwoFactorRepository) GetChallengeByTokenHash(ctx context.Context, tokenHash string) (*models.LoginChallenge, error) {
	c := &models.LoginChallenge{}
	err := r.db.QueryRowContext(ctx, `
		SELECT id, token_hash, user_id, remember_me, attempts, expires_at, created_at
		FROM login_challenges WHERE token_hash = $1`, tokenHash).Scan(
		&c.ID, &c.TokenHash, &c.UserID, &c.RememberMe, &c.Attempts, &c.ExpiresAt, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// IncrementChallengeAttempts adds one wrong code to the challenge and
// returns the new count. ErrNotFound means the challenge is gone.
func (r *TwoFactorRepository) IncrementChallengeAttempts(ctx context.Context, id string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		UPDATE login_challenges SET attempts = attempts + 1 WHERE id = $1 RETURNING attempts`, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return n, err
}

// DeleteChallenge removes a challenge and reports whether it existed. The
// sign-in step deletes the challenge before it creates a session, so two
// requests racing with the same token cannot both succeed.
func (r *TwoFactorRepository) DeleteChallenge(ctx context.Context, id string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM login_challenges WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
