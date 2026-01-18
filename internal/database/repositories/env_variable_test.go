package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestEnvVariableRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Create user, project, environment
	userRepo := NewUserRepository(db)
	user := &models.User{Email: uniqueEmail("envvar-create")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "DATABASE_URL_" + uniqueID(),
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
	t.Parallel()
	db := getTestDB(t)

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Create user, project, environment
	userRepo := NewUserRepository(db)
	user := &models.User{Email: uniqueEmail("envvar-list")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	vars := []*models.EnvVariable{
		{EnvironmentID: env.ID, Key: "K1_" + uniqueID(), Value: "V1"},
		{EnvironmentID: env.ID, Key: "K2_" + uniqueID(), Value: "V2"},
	}

	for _, v := range vars {
		if err := repo.Create(ctx, v); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
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
	t.Parallel()
	db := getTestDB(t)

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Create user, project, environment
	userRepo := NewUserRepository(db)
	user := &models.User{Email: uniqueEmail("envvar-delete")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "DELETE_ME_" + uniqueID(),
		Value:         "VALUE",
	}
	if err := repo.Create(ctx, v); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err := repo.Delete(ctx, v.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify by trying to get the variable by ID - list would include other vars
	list, _ := repo.ListByEnvironmentID(ctx, env.ID)
	for _, item := range list {
		if item.ID == v.ID {
			t.Error("Delete() did not remove the variable")
		}
	}
}
