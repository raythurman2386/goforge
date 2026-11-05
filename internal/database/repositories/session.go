package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

// SessionRepository handles session data access.
type SessionRepository struct {
	db *sql.DB
}

// NewSessionRepository creates a new SessionRepository.
func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Create creates a new session.
func (r *SessionRepository) Create(ctx context.Context, session *models.Session) error {
	if session.ID == "" {
		session.ID = uuid.New().String()
	}
	now := time.Now()
	session.CreatedAt = now
	session.LastActiveAt = now

	query := `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, last_active_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := r.db.ExecContext(ctx, query,
		session.ID,
		session.UserID,
		session.TokenHash,
		session.ExpiresAt,
		session.CreatedAt,
		session.LastActiveAt,
	)
	return err
}

// GetByTokenHash retrieves a session by token hash.
func (r *SessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*models.Session, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, created_at, last_active_at
		FROM sessions WHERE token_hash = $1`

	session := &models.Session{}
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&session.ID,
		&session.UserID,
		&session.TokenHash,
		&session.ExpiresAt,
		&session.CreatedAt,
		&session.LastActiveAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return session, err
}

// GetByUserID retrieves all sessions for a user.
func (r *SessionRepository) GetByUserID(ctx context.Context, userID string) ([]*models.Session, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, created_at, last_active_at
		FROM sessions WHERE user_id = $1 ORDER BY created_at DESC
		LIMIT 1000`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*models.Session
	for rows.Next() {
		session := &models.Session{}
		if err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.TokenHash,
			&session.ExpiresAt,
			&session.CreatedAt,
			&session.LastActiveAt,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// UpdateLastActive updates the last active time for a session.
func (r *SessionRepository) UpdateLastActive(ctx context.Context, id string) error {
	query := `UPDATE sessions SET last_active_at = $1 WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

// Delete deletes a session.
func (r *SessionRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// DeleteByIDAndUserID deletes one session only when it belongs to userID.
// The ownership predicate is part of the mutation, so a session ID from
// another account can never be revoked by the signed-in user.
func (r *SessionRepository) DeleteByIDAndUserID(ctx context.Context, id, userID string) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteByUserID deletes all sessions for a user.
func (r *SessionRepository) DeleteByUserID(ctx context.Context, userID string) error {
	query := `DELETE FROM sessions WHERE user_id = $1`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

// DeleteByUserIDExcept deletes every session of userID except keepSessionID
// and returns how many rows it removed. An empty keepSessionID keeps none,
// for a request that was not made with a session (for example an API token).
func (r *SessionRepository) DeleteByUserIDExcept(ctx context.Context, userID, keepSessionID string) (int64, error) {
	var (
		result sql.Result
		err    error
	)
	if keepSessionID == "" {
		result, err = r.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	} else {
		result, err = r.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, userID, keepSessionID)
	}
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteExpired deletes all expired sessions.
func (r *SessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM sessions WHERE expires_at < $1`
	result, err := r.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
