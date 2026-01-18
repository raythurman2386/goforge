package repositories

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestUserRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{
		Email:     uniqueEmail("create"),
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
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	expectedID := "12345678-1234-5678-1234-567812345678"
	user := &models.User{
		ID:    expectedID,
		Email: uniqueEmail("custom-id"),
	}

	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if user.ID != expectedID {
		t.Errorf("Create() ID = %q, want %q", user.ID, expectedID)
	}

	// Cleanup: delete the user to avoid conflict on re-run
	_ = repo.Delete(ctx, user.ID)
}

func TestUserRepository_Create_DuplicateEmail(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	email := uniqueEmail("duplicate")
	user1 := &models.User{Email: email}
	err := repo.Create(ctx, user1)
	if err != nil {
		t.Fatalf("Create() first user error = %v", err)
	}

	user2 := &models.User{Email: email}
	err = repo.Create(ctx, user2)
	if err == nil {
		t.Error("Create() expected error for duplicate email, got nil")
	}
}

func TestUserRepository_GetByID_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_GetByEmail_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByEmail(ctx, "nonexistent-"+uniqueID()+"@example.com")
	if err != ErrNotFound {
		t.Errorf("GetByEmail() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_GetByGitHubID_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByGitHubID(ctx, 99999999)
	if err != ErrNotFound {
		t.Errorf("GetByGitHubID() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Update_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{
		ID:    "00000000-0000-0000-0000-000000000001",
		Email: uniqueEmail("update-notfound"),
	}

	err := repo.Update(ctx, user)
	if err != ErrNotFound {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Delete(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("delete")}
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
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx := context.Background()

	err := repo.Delete(ctx, "00000000-0000-0000-0000-000000000002")
	if err != ErrNotFound {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_ContextCancellation(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	user := &models.User{Email: uniqueEmail("cancelled")}
	err := repo.Create(ctx, user)
	if err == nil {
		t.Error("Create() with cancelled context expected error, got nil")
	}
}

func TestUserRepository_ContextTimeout(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Give time for the timeout to expire
	time.Sleep(10 * time.Millisecond)

	user := &models.User{Email: uniqueEmail("timeout")}
	err := repo.Create(ctx, user)
	if err == nil {
		t.Error("Create() with expired context expected error, got nil")
	}
}

func TestNewUserRepository(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db)
	if repo == nil {
		t.Fatal("NewUserRepository() returned nil")
	}
}
