package models

import (
	"database/sql"
	"testing"
)

func TestBootstrapRole(t *testing.T) {
	if got := BootstrapRole(0); got != RoleAdmin {
		t.Fatalf("BootstrapRole(0) = %q, want admin", got)
	}
	if got := BootstrapRole(1); got != RoleUser {
		t.Fatalf("BootstrapRole(1) = %q, want user", got)
	}
	if got := BootstrapRole(4); got != RoleUser {
		t.Fatalf("BootstrapRole(4) = %q, want user", got)
	}
}

func TestUser_IsAdmin(t *testing.T) {
	if (&User{Role: RoleAdmin}).IsAdmin() != true {
		t.Fatal("admin role should be admin")
	}
	if (&User{Role: RoleUser}).IsAdmin() {
		t.Fatal("user role should not be admin")
	}
	var none *User
	if none.IsAdmin() {
		t.Fatal("nil user should not be admin")
	}
}

func TestRole_ScanRejectsUnknown(t *testing.T) {
	var role Role
	if err := role.Scan("owner"); err == nil {
		t.Fatal("Scan accepted an unknown role")
	}
	if err := role.Scan("admin"); err != nil {
		t.Fatal(err)
	}
	if role != RoleAdmin {
		t.Fatalf("role = %q", role)
	}
}

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
