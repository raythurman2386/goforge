package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestEnvVariableRepository_Create(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Create user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "env-test-user@example.com"}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Create project
	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project",
		RepositoryURL: "https://github.com/test/repo",
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create environment
	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod"}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "DATABASE_URL",
		Value:         "postgres://localhost",
		IsSecret:      true,
	}

	err := repo.Create(ctx, v)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if v.ID == "" {
		t.Error("Create() did not set ID")
	}
}

func TestEnvVariableRepository_ListByEnvironmentID(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Create user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "env-list-test@example.com"}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Create project
	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project",
		RepositoryURL: "https://github.com/test/repo",
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create environment
	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod"}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	vars := []*models.EnvVariable{
		{EnvironmentID: env.ID, Key: "K1", Value: "V1"},
		{EnvironmentID: env.ID, Key: "K2", Value: "V2"},
	}

	for _, v := range vars {
		_ = repo.Create(ctx, v)
	}

	list, err := repo.ListByEnvironmentID(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListByEnvironmentID() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("ListByEnvironmentID() len = %d, want 2", len(list))
	}
}

func TestEnvVariableRepository_Delete(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Create user first
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "env-delete-test@example.com"}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Create project
	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project",
		RepositoryURL: "https://github.com/test/repo",
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create environment
	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod"}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "DELETE_ME",
		Value:         "VALUE",
	}
	_ = repo.Create(ctx, v)

	err := repo.Delete(ctx, v.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	list, _ := repo.ListByEnvironmentID(ctx, env.ID)
	if len(list) != 0 {
		t.Errorf("ListByEnvironmentID() len = %d, want 0", len(list))
	}
}
