package repositories

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/user/gocool/internal/models"
)

func setupEnvVariableTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_time_format=sqlite")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create environments table (needed for FK)
	_, err = db.Exec(`
		CREATE TABLE environments (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			name TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create environments table: %v", err)
	}

	// Create env_variables table
	_, err = db.Exec(`
		CREATE TABLE env_variables (
			id TEXT PRIMARY KEY,
			environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			is_secret BOOLEAN DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(environment_id, key)
		)
	`)
	if err != nil {
		t.Fatalf("failed to create env_variables table: %v", err)
	}

	return db
}

func TestEnvVariableRepository_Create(t *testing.T) {
	db := setupEnvVariableTestDB(t)
	defer db.Close()

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Need an environment first
	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: "project-1", Name: "prod"}
	_ = envRepo.Create(ctx, env)

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
	db := setupEnvVariableTestDB(t)
	defer db.Close()

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Need an environment first
	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: "project-1", Name: "prod"}
	_ = envRepo.Create(ctx, env)

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
	db := setupEnvVariableTestDB(t)
	defer db.Close()

	repo := NewEnvVariableRepository(db)
	ctx := context.Background()

	// Need an environment first
	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: "project-1", Name: "prod"}
	_ = envRepo.Create(ctx, env)

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
