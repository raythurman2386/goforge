package repositories

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/raythurman2386/gocool/internal/models"
)

func setupContainerTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_time_format=sqlite")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create containers table
	_, err = db.Exec(`
		CREATE TABLE containers (
			id TEXT PRIMARY KEY,
			deployment_id TEXT,
			service_instance_id TEXT,
			docker_id TEXT NOT NULL,
			name TEXT NOT NULL,
			image TEXT NOT NULL,
			status TEXT NOT NULL,
			ports TEXT DEFAULT '[]',
			labels TEXT DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create containers table: %v", err)
	}

	return db
}

func TestContainerRepository_Create(t *testing.T) {
	db := setupContainerTestDB(t)
	defer db.Close()

	repo := NewContainerRepository(db)
	ctx := context.Background()

	c := &models.Container{
		DockerID: "docker-123",
		Name:     "web-app",
		Image:    "nginx:latest",
		Status:   models.ContainerStatusRunning,
	}

	err := repo.Create(ctx, c)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if c.ID == "" {
		t.Error("Create() did not set ID")
	}
}

func TestContainerRepository_GetByDockerID(t *testing.T) {
	db := setupContainerTestDB(t)
	defer db.Close()

	repo := NewContainerRepository(db)
	ctx := context.Background()

	c := &models.Container{
		DockerID: "docker-123",
		Name:     "web-app",
		Image:    "nginx:latest",
		Status:   models.ContainerStatusRunning,
	}
	_ = repo.Create(ctx, c)

	found, err := repo.GetByDockerID(ctx, "docker-123")
	if err != nil {
		t.Fatalf("GetByDockerID() error = %v", err)
	}

	if found.Name != c.Name {
		t.Errorf("GetByDockerID() Name = %q, want %q", found.Name, c.Name)
	}
}

func TestContainerRepository_ListByDeploymentID(t *testing.T) {
	db := setupContainerTestDB(t)
	defer db.Close()

	repo := NewContainerRepository(db)
	ctx := context.Background()

	deploymentID := "deploy-1"
	c := &models.Container{
		DeploymentID: sql.NullString{String: deploymentID, Valid: true},
		DockerID:     "docker-123",
		Name:         "web-app",
		Image:        "nginx:latest",
		Status:       models.ContainerStatusRunning,
	}
	_ = repo.Create(ctx, c)

	list, err := repo.ListByDeploymentID(ctx, deploymentID)
	if err != nil {
		t.Fatalf("ListByDeploymentID() error = %v", err)
	}

	if len(list) != 1 {
		t.Errorf("ListByDeploymentID() len = %d, want 1", len(list))
	}
}
