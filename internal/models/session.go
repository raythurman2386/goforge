package models

import (
	"time"
)

// Session represents a user session.
type Session struct {
	ID           string
	UserID       string
	TokenHash    string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	LastActiveAt time.Time
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}
