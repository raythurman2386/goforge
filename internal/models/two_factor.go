package models

import (
	"database/sql"
	"time"
)

// UserTOTP is an account's TOTP enrollment (user_totp). Secret is the
// base32 secret after the repository opened the sealed column; it is never
// logged or sent back after enrollment.
type UserTOTP struct {
	UserID       string
	Secret       string
	EnabledAt    sql.NullTime
	LastUsedStep int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Enabled reports whether enrollment was confirmed with a first code.
func (t *UserTOTP) Enabled() bool {
	return t != nil && t.EnabledAt.Valid
}

// LoginChallenge is the pending step between a correct password and the
// second factor (login_challenges). It is not a session: it only lets the
// browser holding its token submit a code until ExpiresAt.
type LoginChallenge struct {
	ID         string
	TokenHash  string
	UserID     string
	RememberMe bool
	Attempts   int
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// Expired reports whether the challenge is past ExpiresAt at now.
func (c *LoginChallenge) Expired(now time.Time) bool {
	return !now.Before(c.ExpiresAt)
}
