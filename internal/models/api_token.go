package models

import (
	"database/sql"
	"time"
)

// APIToken represents a personal access token.
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  string
	LastUsedAt sql.NullTime
	ExpiresAt  sql.NullTime
	CreatedAt  time.Time
}

// IsExpired returns true if the token has expired.
func (t *APIToken) IsExpired() bool {
	if !t.ExpiresAt.Valid {
		return false
	}
	return time.Now().After(t.ExpiresAt.Time)
}

// IsActive returns true if the token is active (not expired).
func (t *APIToken) IsActive() bool {
	return !t.IsExpired()
}
