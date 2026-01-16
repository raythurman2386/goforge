package auth

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

func setupSessionTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_time_format=sqlite")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create users table
	_, err = db.Exec(`
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT,
			github_id INTEGER,
			github_username TEXT,
			github_access_token TEXT,
			avatar_url TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}

	// Create sessions table
	_, err = db.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT UNIQUE NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create sessions table: %v", err)
	}

	return db
}

func TestSessionManager(t *testing.T) {
	db := setupSessionTestDB(t)
	defer db.Close()

	userRepo := repositories.NewUserRepository(db)
	sessionRepo := repositories.NewSessionRepository(db)
	manager := NewSessionManager(sessionRepo, userRepo)
	ctx := context.Background()

	// Create a user
	user := &models.User{Email: "session@example.com"}
	_ = userRepo.Create(ctx, user)

	// Create a session
	session, token, err := manager.CreateSession(ctx, user.ID, false)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if token == "" {
		t.Fatal("CreateSession() returned empty token")
	}

	// Get the session
	gotSession, gotUser, err := manager.GetSession(ctx, token)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	if gotUser.ID != user.ID {
		t.Errorf("GetSession() User ID = %q, want %q", gotUser.ID, user.ID)
	}

	if gotSession.ID != session.ID {
		t.Errorf("GetSession() Session ID = %q, want %q", gotSession.ID, session.ID)
	}

	// Destroy the session
	err = manager.DestroySession(ctx, token)
	if err != nil {
		t.Fatalf("DestroySession() error = %v", err)
	}

	// Try to get it again
	_, _, err = manager.GetSession(ctx, token)
	if err != ErrInvalidToken {
		t.Errorf("GetSession() after destroy error = %v, want %v", err, ErrInvalidToken)
	}
}
