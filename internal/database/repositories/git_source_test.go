package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

func TestGitSourceRepository_Create(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	key, _ := secrets.GenerateKeyString(32)
	cfg := &config.Config{EncryptionKey: key}
	secretService, _ := secrets.NewEncryptionService(cfg)
	repo := NewGitSourceRepository(db, secretService)
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
	db := setupSharedTestDB(t)
	defer db.Close()

	key, _ := secrets.GenerateKeyString(32)
	cfg := &config.Config{EncryptionKey: key}
	secretService, _ := secrets.NewEncryptionService(cfg)
	repo := NewGitSourceRepository(db, secretService)
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
	db := setupSharedTestDB(t)
	defer db.Close()

	key, _ := secrets.GenerateKeyString(32)
	cfg := &config.Config{EncryptionKey: key}
	secretService, _ := secrets.NewEncryptionService(cfg)
	repo := NewGitSourceRepository(db, secretService)
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
	db := setupSharedTestDB(t)
	defer db.Close()

	key, _ := secrets.GenerateKeyString(32)
	cfg := &config.Config{EncryptionKey: key}
	secretService, _ := secrets.NewEncryptionService(cfg)
	repo := NewGitSourceRepository(db, secretService)
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
	db := setupSharedTestDB(t)
	defer db.Close()

	key, _ := secrets.GenerateKeyString(32)
	cfg := &config.Config{EncryptionKey: key}
	secretService, _ := secrets.NewEncryptionService(cfg)
	repo := NewGitSourceRepository(db, secretService)
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
