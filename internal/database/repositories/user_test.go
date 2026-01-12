package repositories

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/user/gocool/internal/models"
)

// setupTestDB creates an in-memory SQLite database for testing.
func setupTestDB(t *testing.T) *sql.DB {
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

	return db
}

func TestUserRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{
		Email:     "test@example.com",
		AvatarURL: sql.NullString{String: "https://example.com/avatar.png", Valid: true},
	}

	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if user.ID == "" {
		t.Error("Create() did not set ID")
	}
	if user.CreatedAt.IsZero() {
		t.Error("Create() did not set CreatedAt")
	}
	if user.UpdatedAt.IsZero() {
		t.Error("Create() did not set UpdatedAt")
	}
}

func TestUserRepository_Create_WithID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	expectedID := "custom-user-id-123"
	user := &models.User{
		ID:    expectedID,
		Email: "custom@example.com",
	}

	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if user.ID != expectedID {
		t.Errorf("Create() ID = %q, want %q", user.ID, expectedID)
	}
}

func TestUserRepository_Create_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	user1 := &models.User{Email: "duplicate@example.com"}
	err := repo.Create(ctx, user1)
	if err != nil {
		t.Fatalf("Create() first user error = %v", err)
	}

	user2 := &models.User{Email: "duplicate@example.com"}
	err = repo.Create(ctx, user2)
	if err == nil {
		t.Error("Create() expected error for duplicate email, got nil")
	}
}

func TestUserRepository_GetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent-id")
	if err != ErrNotFound {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_GetByEmail_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByEmail(ctx, "nonexistent@example.com")
	if err != ErrNotFound {
		t.Errorf("GetByEmail() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_GetByGitHubID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByGitHubID(ctx, 99999)
	if err != ErrNotFound {
		t.Errorf("GetByGitHubID() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Update_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{
		ID:    "nonexistent-id",
		Email: "update@example.com",
	}

	err := repo.Update(ctx, user)
	if err != ErrNotFound {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{Email: "delete@example.com"}
	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err = repo.Delete(ctx, user.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify deletion
	_, err = repo.GetByID(ctx, user.ID)
	if err != ErrNotFound {
		t.Errorf("GetByID() after delete error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Delete_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent-id")
	if err != ErrNotFound {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	user := &models.User{Email: "cancelled@example.com"}
	err := repo.Create(ctx, user)
	if err == nil {
		t.Error("Create() with cancelled context expected error, got nil")
	}
}

func TestUserRepository_ContextTimeout(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Give time for the timeout to expire
	time.Sleep(10 * time.Millisecond)

	user := &models.User{Email: "timeout@example.com"}
	err := repo.Create(ctx, user)
	if err == nil {
		t.Error("Create() with expired context expected error, got nil")
	}
}

func TestNewUserRepository(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewUserRepository(db)
	if repo == nil {
		t.Fatal("NewUserRepository() returned nil")
	}
}
