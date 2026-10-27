// Package models contains the domain models for GoForge.
package models

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// Role is the operator privilege of an account.
// Admin is the bootstrap operator. Every later account is a user.
type Role string

const (
	// RoleAdmin can run host operations such as pruning Docker state.
	RoleAdmin Role = "admin"
	// RoleUser is an ordinary account.
	RoleUser Role = "user"
)

// BootstrapRole is the role assigned when an account is created.
// The empty table produces one admin. Every later account is a user.
func BootstrapRole(existingUsers int) Role {
	if existingUsers == 0 {
		return RoleAdmin
	}
	return RoleUser
}

// IsAdmin reports whether the role can perform operator actions.
func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

// Scan reads a role from a database value.
func (r *Role) Scan(value any) error {
	if value == nil {
		*r = ""
		return nil
	}
	switch v := value.(type) {
	case string:
		*r = Role(v)
	case []byte:
		*r = Role(v)
	default:
		return fmt.Errorf("cannot scan role from %T", value)
	}
	switch *r {
	case RoleAdmin, RoleUser, "":
		return nil
	default:
		return fmt.Errorf("invalid role %q", string(*r))
	}
}

// Value writes the role to the database.
func (r Role) Value() (driver.Value, error) {
	switch r {
	case RoleAdmin, RoleUser:
		return string(r), nil
	default:
		return nil, fmt.Errorf("invalid role %q", string(r))
	}
}

// User represents a user account.
type User struct {
	ID                string
	Email             string
	PasswordHash      sql.NullString
	GitHubID          sql.NullInt64
	GitHubUsername    sql.NullString
	GitHubAccessToken sql.NullString // encrypted
	AvatarURL         sql.NullString
	Role              Role
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

// IsAdmin reports whether this account can perform operator actions.
func (u *User) IsAdmin() bool {
	return u != nil && u.Role.IsAdmin()
}
