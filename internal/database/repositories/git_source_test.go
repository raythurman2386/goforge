package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestGitSourceRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	secretService := getTestSecretService(t)
	repo := NewGitSourceRepository(db, secretService)
	userRepo := NewUserRepository(db)
	ctx := context.Background()

	// Create unique user for this test
	user := &models.User{Email: uniqueEmail("gitsource-create")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub-" + uniqueID(),
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
	t.Parallel()
	db := getTestDB(t)

	secretService := getTestSecretService(t)
	repo := NewGitSourceRepository(db, secretService)
	userRepo := NewUserRepository(db)
	ctx := context.Background()

	// Create unique user for this test
	user := &models.User{Email: uniqueEmail("gitsource-getbyid")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub-" + uniqueID(),
		Type:   models.GitSourceTypeGitHub,
	}
	if err := repo.Create(ctx, source); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, err := repo.GetByID(ctx, source.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if found.Name != source.Name {
		t.Errorf("GetByID() Name = %q, want %q", found.Name, source.Name)
	}
}

func TestGitSourceRepository_GetByID_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	secretService := getTestSecretService(t)
	repo := NewGitSourceRepository(db, secretService)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestGitSourceRepository_ListByUserID(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	secretService := getTestSecretService(t)
	repo := NewGitSourceRepository(db, secretService)
	userRepo := NewUserRepository(db)
	ctx := context.Background()

	// Create unique user for this test
	user := &models.User{Email: uniqueEmail("gitsource-list")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	sources := []*models.GitSource{
		{UserID: user.ID, Name: "GitHub-" + uniqueID(), Type: models.GitSourceTypeGitHub},
		{UserID: user.ID, Name: "GitLab-" + uniqueID(), Type: models.GitSourceTypeGitLab},
	}

	for _, s := range sources {
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
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
	t.Parallel()
	db := getTestDB(t)

	secretService := getTestSecretService(t)
	repo := NewGitSourceRepository(db, secretService)
	userRepo := NewUserRepository(db)
	ctx := context.Background()

	// Create unique user for this test
	user := &models.User{Email: uniqueEmail("gitsource-update")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub-" + uniqueID(),
		Type:   models.GitSourceTypeGitHub,
	}
	if err := repo.Create(ctx, source); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updatedName := "Updated-GitHub-" + uniqueID()
	source.Name = updatedName
	err := repo.Update(ctx, source)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	found, _ := repo.GetByID(ctx, source.ID)
	if found.Name != updatedName {
		t.Errorf("Update() Name = %q, want %q", found.Name, updatedName)
	}
}

func TestGitSourceRepository_Delete(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	secretService := getTestSecretService(t)
	repo := NewGitSourceRepository(db, secretService)
	userRepo := NewUserRepository(db)
	ctx := context.Background()

	// Create unique user for this test
	user := &models.User{Email: uniqueEmail("gitsource-delete")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	source := &models.GitSource{
		UserID: user.ID,
		Name:   "GitHub-" + uniqueID(),
		Type:   models.GitSourceTypeGitHub,
	}
	if err := repo.Create(ctx, source); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err := repo.Delete(ctx, source.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.GetByID(ctx, source.ID)
	if err != ErrNotFound {
		t.Errorf("GetByID() after delete error = %v, want ErrNotFound", err)
	}
}
