package models

import (
	"database/sql"
	"testing"
)

func TestUser_HasPassword(t *testing.T) {
	tests := []struct {
		name         string
		passwordHash sql.NullString
		want         bool
	}{
		{
			name:         "valid password hash",
			passwordHash: sql.NullString{String: "$2a$10$hash...", Valid: true},
			want:         true,
		},
		{
			name:         "null password hash",
			passwordHash: sql.NullString{Valid: false},
			want:         false,
		},
		{
			name:         "empty string password (valid but empty)",
			passwordHash: sql.NullString{String: "", Valid: true},
			want:         false,
		},
		{
			name:         "empty string password (invalid)",
			passwordHash: sql.NullString{String: "", Valid: false},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{PasswordHash: tt.passwordHash}
			if got := u.HasPassword(); got != tt.want {
				t.Errorf("User.HasPassword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_IsGitHubUser(t *testing.T) {
	tests := []struct {
		name     string
		githubID sql.NullInt64
		want     bool
	}{
		{
			name:     "valid github ID",
			githubID: sql.NullInt64{Int64: 12345678, Valid: true},
			want:     true,
		},
		{
			name:     "null github ID",
			githubID: sql.NullInt64{Valid: false},
			want:     false,
		},
		{
			name:     "zero github ID (valid but zero)",
			githubID: sql.NullInt64{Int64: 0, Valid: true},
			want:     false,
		},
		{
			name:     "negative github ID (edge case)",
			githubID: sql.NullInt64{Int64: -1, Valid: true},
			want:     false,
		},
		{
			name:     "large github ID",
			githubID: sql.NullInt64{Int64: 9999999999, Valid: true},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{GitHubID: tt.githubID}
			if got := u.IsGitHubUser(); got != tt.want {
				t.Errorf("User.IsGitHubUser() = %v, want %v", got, tt.want)
			}
		})
	}
}
