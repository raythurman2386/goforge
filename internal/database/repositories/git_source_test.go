package repositories

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/raythurman2386/gocool/internal/models"
)

func setupGitSourceTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_time_format=sqlite")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create users table (needed for FK)
	_, err = db.Exec(`
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}

	// Create git_sources table
	_, err = db.Exec(`
		CREATE TABLE git_sources (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			api_url TEXT,
			access_token TEXT,
			ssh_private_key TEXT,
			is_default BOOLEAN DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create git_sources table: %v", err)
	}

	return db
}

func TestGitSourceRepository_Create(t *testing.T) {
	db := setupGitSourceTestDB(t)
	defer db.Close()

	repo := NewGitSourceRepository(db)
	ctx := context.Background()

	// Need a user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "test@example.com"}
	_ = userRepo.Create(ctx, user)

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub",
		Type:   models.GitSourceTypeGitHub,
	}

	err := repo.Create(ctx, source)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if source.ID == "" {
		t.Error("Create() did not set ID")
	}
}

func TestGitSourceRepository_GetByID(t *testing.T) {
	db := setupGitSourceTestDB(t)
	defer db.Close()

	repo := NewGitSourceRepository(db)
	ctx := context.Background()

	// Need a user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "test@example.com"}
	_ = userRepo.Create(ctx, user)

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub",
		Type:   models.GitSourceTypeGitHub,
	}
	_ = repo.Create(ctx, source)

	found, err := repo.GetByID(ctx, source.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if found.Name != source.Name {
		t.Errorf("GetByID() Name = %q, want %q", found.Name, source.Name)
	}
}

func TestGitSourceRepository_ListByUserID(t *testing.T) {
	db := setupGitSourceTestDB(t)
	defer db.Close()

	repo := NewGitSourceRepository(db)
	ctx := context.Background()

	// Need a user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "test@example.com"}
	_ = userRepo.Create(ctx, user)

	sources := []*models.GitSource{
		{UserID: user.ID, Name: "GitHub", Type: models.GitSourceTypeGitHub},
		{UserID: user.ID, Name: "GitLab", Type: models.GitSourceTypeGitLab},
	}

	for _, s := range sources {
		_ = repo.Create(ctx, s)
	}

	list, err := repo.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("ListByUserID() len = %d, want 2", len(list))
	}
}

func TestGitSourceRepository_Update(t *testing.T) {
	db := setupGitSourceTestDB(t)
	defer db.Close()

	repo := NewGitSourceRepository(db)
	ctx := context.Background()

	// Need a user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "test@example.com"}
	_ = userRepo.Create(ctx, user)

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub",
		Type:   models.GitSourceTypeGitHub,
	}
	err := repo.Create(ctx, source)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	source.Name = "Updated GitHub"
	err = repo.Update(ctx, source)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	found, _ := repo.GetByID(ctx, source.ID)
	if found.Name != "Updated GitHub" {
		t.Errorf("Update() Name = %q, want %q", found.Name, "Updated GitHub")
	}
}

func TestGitSourceRepository_Delete(t *testing.T) {
	db := setupGitSourceTestDB(t)
	defer db.Close()

	repo := NewGitSourceRepository(db)
	ctx := context.Background()

	// Need a user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "test@example.com"}
	_ = userRepo.Create(ctx, user)

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub",
		Type:   models.GitSourceTypeGitHub,
	}
	_ = repo.Create(ctx, source)

	err := repo.Delete(ctx, source.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.GetByID(ctx, source.ID)
	if err != ErrNotFound {
		t.Errorf("GetByID() after delete error = %v, want ErrNotFound", err)
	}
}
