package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/user/gocool/internal/models"
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
		FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`

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
	query := `UPDATE sessions SET last_active_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

// Delete deletes a session.
func (r *SessionRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// DeleteByUserID deletes all sessions for a user.
func (r *SessionRepository) DeleteByUserID(ctx context.Context, userID string) error {
	query := `DELETE FROM sessions WHERE user_id = $1`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
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
