// Package models contains the domain models for GoForge.
package models

import (
	"database/sql"
	"time"
)

// User represents a user account.
type User struct {
	ID                string
	Email             string
	PasswordHash      sql.NullString
	GitHubID          sql.NullInt64
	GitHubUsername    sql.NullString
	GitHubAccessToken sql.NullString // encrypted
	AvatarURL         sql.NullString
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// HasPassword returns true if the user has a local password set.
func (u *User) HasPassword() bool {
	return u.PasswordHash.Valid && u.PasswordHash.String != ""
}

// IsGitHubUser returns true if the user signed up via GitHub OAuth.
func (u *User) IsGitHubUser() bool {
	return u.GitHubID.Valid && u.GitHubID.Int64 > 0
}
